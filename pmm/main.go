/*
	Copyright (c) 2016, Percona LLC and/or its affiliates. All rights reserved.

	This program is free software: you can redistribute it and/or modify
	it under the terms of the GNU Affero General Public License as published by
	the Free Software Foundation, either version 3 of the License, or
	(at your option) any later version.

	This program is distributed in the hope that it will be useful,
	but WITHOUT ANY WARRANTY; without even the implied warranty of
	MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
	GNU Affero General Public License for more details.

	You should have received a copy of the GNU Affero General Public License
	along with this program.  If not, see <http://www.gnu.org/licenses/>
*/

package pmm

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	consul "github.com/hashicorp/consul/api"
	"github.com/percona/kardianos-service"
	protocfg "github.com/percona/pmm/proto/config"
	"github.com/prometheus/client_golang/api/prometheus"
	//"golang.org/x/net/context"
	"gopkg.in/yaml.v2"
)

// PMM client config structure.
type Config struct {
	ServerAddress     string `yaml:"server_address"`
	ClientAddress     string `yaml:"client_address"`
	BindAddress       string `yaml:"bind_address"`
	ClientName        string `yaml:"client_name"`
	MySQLPassword     string `yaml:"mysql_password,omitempty"`
	ServerUser        string `yaml:"server_user,omitempty"`
	ServerPassword    string `yaml:"server_password,omitempty"`
	ServerSSL         bool   `yaml:"server_ssl,omitempty"`
	ServerInsecureSSL bool   `yaml:"server_insecure_ssl,omitempty"`
}

// Service status description.
type instanceStatus struct {
	Type    string
	Name    string
	Port    string
	Status  string
	DSN     string
	Options string
}

// Main class.
type Admin struct {
	ServiceName  string
	ServicePort  uint16
	Config       *Config
	filename     string
	serverURL    string
	apiTimeout   time.Duration
	qanAPI       *API
	consulAPI    *consul.Client
	promQueryAPI prometheus.QueryAPI
	//promSeriesAPI prometheus.SeriesAPI
}

// LoadConfig read PMM client config file.
func (a *Admin) LoadConfig(filename string) error {
	a.filename = filename
	a.Config = &Config{}
	if !FileExists(filename) {
		return nil
	}
	bytes, err := ioutil.ReadFile(filename)
	if err != nil {
		return err
	}
	if err := yaml.Unmarshal(bytes, a.Config); err != nil {
		return err
	}

	// If not set previously, assume it equals to client address.
	if a.Config.BindAddress == "" {
		a.Config.BindAddress = a.Config.ClientAddress
	}
	return nil
}

// SetConfig configure PMM client, check connectivity and write the config.
func (a *Admin) SetConfig(cf Config, flagForce bool) error {
	// Server options.
	if cf.ServerSSL && cf.ServerInsecureSSL {
		return errors.New("Flags --server-ssl and --server-insecure-ssl are mutually exclusive.")
	}

	if cf.ServerAddress != "" {
		a.Config.ServerAddress = cf.ServerAddress
		// Resetting server address clears up SSL and HTTP auth.
		a.Config.ServerSSL = false
		a.Config.ServerInsecureSSL = false
		a.Config.ServerUser = ""
		a.Config.ServerPassword = ""
	}
	if a.Config.ServerAddress == "" {
		return errors.New("Server address is not set. Use --server flag to set it.")
	}

	if cf.ServerPassword != "" {
		a.Config.ServerUser = cf.ServerUser
		a.Config.ServerPassword = cf.ServerPassword
	}
	if cf.ServerSSL {
		a.Config.ServerSSL = true
		a.Config.ServerInsecureSSL = false
	}
	if cf.ServerInsecureSSL {
		a.Config.ServerSSL = false
		a.Config.ServerInsecureSSL = true
	}

	// Set APIs and check if server is alive.
	if err := a.SetAPI(); err != nil {
		return err
	}

	// Client options.

	// Client name. Initial setup.
	if a.Config.ClientName == "" {
		if cf.ClientName != "" {
			a.Config.ClientName = cf.ClientName
		} else {
			hostname, _ := os.Hostname()
			a.Config.ClientName = hostname
		}

		node, _, err := a.consulAPI.Catalog().Node(a.Config.ClientName, nil)
		if err != nil {
			return fmt.Errorf("Unable to communicate with Consul: %s", err)
		}
		if node != nil && len(node.Services) > 0 {
			if !flagForce {
				return fmt.Errorf(`Another client with the same name '%s' detected, its address is %s.
It has the active services so this name is not available.

Specify the other one using --client-name flag.

In case this is the correct client node that was previously uninstalled with unreachable PMM server,
you can add --force flag to proceed further. Do not use this flag otherwise.
The orphaned remote services will be removed automatically.`,
					a.Config.ClientName, node.Node.Address)
			}
			// Allow to set client name and clean missing services.
			a.RepairInstallation()
		}
	} else if cf.ClientName != "" && cf.ClientName != a.Config.ClientName {
		// Attempt to change client name.
		// Checking source name.
		node, _, err := a.consulAPI.Catalog().Node(a.Config.ClientName, nil)
		if err != nil {
			return fmt.Errorf("Unable to communicate with Consul: %s", err)
		}
		if node != nil && len(node.Services) > 0 {
			return errors.New("Changing of client name is allowed only if there are no services under monitoring.")
		}

		// Checking target name.
		node, _, err = a.consulAPI.Catalog().Node(cf.ClientName, nil)
		if err != nil {
			return fmt.Errorf("Unable to communicate with Consul: %s", err)
		}
		if node != nil && len(node.Services) > 0 {
			return fmt.Errorf(`Another client with the same name '%s' detected, its address is address %s.
It has the active services so you cannot change client name as requested.`,
				cf.ClientName, node.Node.Address)
		}

		a.Config.ClientName = cf.ClientName
	}
	if match, _ := regexp.MatchString(NameRegex, a.Config.ClientName); !match {
		return errors.New(`Client name must be 2 to 60 characters long, contain only letters, numbers and symbols _ - . :
Use --client-name flag to set the correct one.`)
	}

	// Client address. Initial setup.
	isDetectedIP := false
	if a.Config.ClientAddress == "" {
		if cf.ClientAddress != "" {
			a.Config.ClientAddress = cf.ClientAddress
		} else {
			// Detect remote address from nginx response header.
			a.Config.ClientAddress = a.getNginxHeader("X-Remote-IP")
			isDetectedIP = true
		}

		if a.Config.ClientAddress == "" {
			return errors.New("Cannot detect client address. Use --client-address flag to set it.")
		}
	} else if cf.ClientAddress != "" && cf.ClientAddress != a.Config.ClientAddress {
		// Attempt to change client address.
		node, _, err := a.consulAPI.Catalog().Node(a.Config.ClientName, nil)
		if err != nil {
			return fmt.Errorf("Unable to communicate with Consul: %s", err)
		}
		if node != nil && len(node.Services) > 0 {
			return errors.New("Changing of client address is allowed only if there are no services under monitoring.")
		}

		a.Config.ClientAddress = cf.ClientAddress
	}

	// Bind address. Initial setup.
	if a.Config.BindAddress == "" {
		a.Config.BindAddress = a.Config.ClientAddress
		if cf.BindAddress != "" {
			a.Config.BindAddress = cf.BindAddress
			isDetectedIP = false
		}
	} else if cf.BindAddress != "" && cf.BindAddress != a.Config.BindAddress {
		// Attempt to change bind address.
		node, _, err := a.consulAPI.Catalog().Node(a.Config.ClientName, nil)
		if err != nil {
			return fmt.Errorf("Unable to communicate with Consul: %s", err)
		}
		if node != nil && len(node.Services) > 0 {
			return errors.New("Changing of bind address is allowed only if there are no services under monitoring.")
		}

		a.Config.BindAddress = cf.BindAddress
	}

	if !isAddressLocal(a.Config.BindAddress) {
		if isDetectedIP {
			return fmt.Errorf(`Detected address '%s' is not locally bound.
This usually happens when client and server are on the different networks.

Use --bind-address flag to set locally bound address, usually a private one, while client address is public.
The bind address should correspond to the detected client address via NAT and you would need to configure port forwarding.

PMM server should be able to connect to the client address '%s' which should translate to a local bind address.
What ports to map you can find from "pmm-admin check-network" output once you add instances to the monitoring.`,
				a.Config.BindAddress, a.Config.ClientAddress)
		}
		return fmt.Errorf(`Client Address: %s
Bind Address: %s

The bind address is not locally bound.

Use --bind-address flag to set locally bound address, usually a private one,
and --client-address flag to set the corresponding remote address, usually a public one.
The bind address should correspond to the client address via NAT and you would need to configure port forwarding.

PMM server should be able to connect to the client address which should translate to a local bind address.
What ports to map you can find from "pmm-admin check-network" output once you add instances to the monitoring.`,
			a.Config.ClientAddress, a.Config.BindAddress)
	}

	// If agent config exists, update the options like address, SSL, password etc.
	agentConfigFile := fmt.Sprintf("%s/config/agent.conf", agentBaseDir)
	if FileExists(agentConfigFile) {
		if err := a.syncAgentConfig(agentConfigFile); err != nil {
			return fmt.Errorf("Unable to update agent config %s: %s", agentConfigFile, err)
		}
		// Restart QAN agent.
		if err := a.StartStopMonitoring("restart", "mysql:queries"); err != nil && err != ErrNoService {
			return fmt.Errorf("Unable to restart queries service: %s", err)
		}
	}

	// Write the config.
	if err := a.writeConfig(); err != nil {
		return fmt.Errorf("Unable to write config file %s: %s", a.filename, err)
	}

	return nil
}

// SetAPI setup Consul, QAN, Prometheus APIs and verify connection.
func (a *Admin) SetAPI() error {
	// Set default API timeout if unset.
	if a.apiTimeout == 0 {
		a.apiTimeout = apiTimeout
	}

	scheme := "http"
	insecureTransport := &http.Transport{}
	if a.Config.ServerInsecureSSL {
		scheme = "https"
		insecureTransport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	if a.Config.ServerSSL {
		scheme = "https"
	}

	// Consul API.
	config := consul.Config{
		Address:    a.Config.ServerAddress,
		HttpClient: &http.Client{Timeout: a.apiTimeout},
		Scheme:     scheme,
	}
	if a.Config.ServerInsecureSSL {
		config.HttpClient.Transport = insecureTransport
	}
	var authStr string
	if a.Config.ServerUser != "" {
		config.HttpAuth = &consul.HttpBasicAuth{Username: a.Config.ServerUser, Password: a.Config.ServerPassword}
		authStr = fmt.Sprintf("%s:%s@", a.Config.ServerUser, a.Config.ServerPassword)
	}
	a.consulAPI, _ = consul.NewClient(&config)

	// Full URL.
	a.serverURL = fmt.Sprintf("%s://%s%s", scheme, authStr, a.Config.ServerAddress)

	// QAN API.
	a.qanAPI = NewAPI(a.Config.ServerInsecureSSL, a.apiTimeout)

	// Prometheus API.
	cfg := prometheus.Config{Address: fmt.Sprintf("%s/prometheus", a.serverURL)}
	if a.Config.ServerInsecureSSL {
		cfg.Transport = insecureTransport
	}
	client, _ := prometheus.New(cfg)
	a.promQueryAPI = prometheus.NewQueryAPI(client)
	//a.promSeriesAPI = prometheus.NewSeriesAPI(client)

	// Check if server is alive.
	url := a.qanAPI.URL(a.serverURL)
	resp, _, err := a.qanAPI.Get(url)
	if err != nil {
		if strings.Contains(err.Error(), "x509: cannot validate certificate") {
			return fmt.Errorf(`Unable to connect to PMM server by address: %s

Looks like PMM server running with self-signed SSL certificate.
Use 'pmm-admin config' with --server-insecure-ssl flag.`, a.Config.ServerAddress)
		}
		return fmt.Errorf(`Unable to connect to PMM server by address: %s

* Check if the configured address is correct.
* If server is running on non-default port, ensure it was specified along with the address.
* If server is enabled for SSL or self-signed SSL, enable the corresponding option.
* You may also check the firewall settings.`, a.Config.ServerAddress)
	}

	// Try to detect 400 (SSL) and 401 (HTTP auth).
	if err == nil && resp.StatusCode == http.StatusBadRequest {
		return fmt.Errorf(`Unable to connect to PMM server by address: %s

Looks like the server is enabled for SSL or self-signed SSL.
Use 'pmm-admin config' to enable the corresponding SSL option.`, a.Config.ServerAddress)
	}
	if err == nil && resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf(`Unable to connect to PMM server by address: %s

Looks like the server is password protected.
Use 'pmm-admin config' to define server user and password.`, a.Config.ServerAddress)
	}

	// Finally, check Consul status.
	if leader, err := a.consulAPI.Status().Leader(); err != nil || leader != "127.0.0.1:8300" {
		return fmt.Errorf(`Unable to connect to PMM server by address: %s

Even though the server is reachable it does not look to be PMM server.
Check if the configured address is correct.`, a.Config.ServerAddress)
	}

	return nil
}

// getNginxHeader get header value from Nginx response.
func (a *Admin) getNginxHeader(header string) string {
	url := a.qanAPI.URL(a.serverURL, "v1/status/leader")
	resp, _, err := a.qanAPI.Get(url)
	if err != nil {
		return ""
	}
	return resp.Header.Get(header)
}

// isAddressLocal check if IP address is locally bound on the system.
func isAddressLocal(myAddress string) bool {
	ips, _ := net.InterfaceAddrs()
	for _, ip := range ips {
		if strings.HasPrefix(ip.String(), myAddress+"/") {
			return true
		}
	}
	return false
}

// writeConfig write config to the file.
func (a *Admin) writeConfig() error {
	bytes, _ := yaml.Marshal(a.Config)
	return ioutil.WriteFile(a.filename, bytes, 0600)
}

// syncAgentConfig sync agent config.
func (a *Admin) syncAgentConfig(agentConfigFile string) error {
	jsonData, err := ioutil.ReadFile(agentConfigFile)
	if err != nil {
		return err
	}
	agentConf := &protocfg.Agent{}
	if err := json.Unmarshal(jsonData, &agentConf); err != nil {
		return err
	}
	agentConf.ApiHostname = a.Config.ServerAddress
	agentConf.ServerSSL = a.Config.ServerSSL
	agentConf.ServerInsecureSSL = a.Config.ServerInsecureSSL
	agentConf.ServerUser = a.Config.ServerUser
	agentConf.ServerPassword = a.Config.ServerPassword

	bytes, _ := json.Marshal(agentConf)
	return ioutil.WriteFile(agentConfigFile, bytes, 0600)
}

// List get all services from Consul.
func (a *Admin) List() error {
	node, _, err := a.consulAPI.Catalog().Node(a.Config.ClientName, nil)
	if err != nil || node == nil {
		fmt.Printf("%s '%s'.\n\n", noMonitoring, a.Config.ClientName)
		return nil
	}

	if len(node.Services) == 0 {
		fmt.Print("No services under monitoring.\n\n")
		return nil
	}

	// Parse all services except mysql:queries.
	var queryService *consul.AgentService
	var svcTable []instanceStatus
	for _, svc := range node.Services {
		svcType := svc.Service
		// When server hostname == client name, we have to exclude consul.
		if svcType == "consul" {
			continue
		}
		if svcType == "mysql:queries" {
			queryService = svc
			continue
		}

		status := "NO"
		if getServiceStatus(fmt.Sprintf("pmm-%s-%d", strings.Replace(svcType, ":", "-", 1), svc.Port)) {
			status = "YES"
		}
		opts := []string{}
		name := "-"
		dsn := "-"
		// Get values for service from Consul KV.
		prefix := fmt.Sprintf("%s/%s/", a.Config.ClientName, svc.ID)
		if data, _, err := a.consulAPI.KV().List(prefix, nil); err == nil {
			for _, kvp := range data {
				key := kvp.Key[len(prefix):]
				switch key {
				case "dsn":
					dsn = string(kvp.Value)
				default:
					opts = append(opts, fmt.Sprintf("%s=%s", key, kvp.Value))
				}
			}
		}
		// Parse Consul service tags.
		for _, tag := range svc.Tags {
			if strings.HasPrefix(tag, "alias_") {
				name = tag[6:]
				continue
			}
			tag := strings.Replace(tag, "_", "=", 1)
			opts = append(opts, tag)
		}
		row := instanceStatus{
			Type:    svcType,
			Name:    name,
			Port:    fmt.Sprintf("%d", svc.Port),
			Status:  status,
			DSN:     dsn,
			Options: strings.Join(opts, ", "),
		}
		svcTable = append(svcTable, row)
	}

	// Parse queries service.
	if queryService != nil {
		status := "NO"
		if getServiceStatus(fmt.Sprintf("pmm-mysql-queries-%d", queryService.Port)) {
			status = "YES"
		}

		// Get names from Consul tags.
		names := []string{}
		for _, tag := range queryService.Tags {
			if strings.HasPrefix(tag, "alias_") {
				names = append(names, tag[6:])
			}
		}

		for _, name := range names {
			dsn := "-"
			opts := []string{}
			// Get values for service from Consul KV.
			prefix := fmt.Sprintf("%s/%s/%s/", a.Config.ClientName, queryService.ID, name)
			if data, _, err := a.consulAPI.KV().List(prefix, nil); err == nil {
				for _, kvp := range data {
					key := kvp.Key[len(prefix):]
					switch key {
					case "dsn":
						dsn = string(kvp.Value)
					case "qan_mysql_uuid":
						f := fmt.Sprintf("%s/config/qan-%s.conf", agentBaseDir, kvp.Value)
						querySource, _ := getQuerySource(f)
						opts = append(opts, fmt.Sprintf("query_source=%s", querySource))
					}
				}
			}
			row := instanceStatus{
				Type:    queryService.Service,
				Name:    name,
				Port:    fmt.Sprintf("%d", queryService.Port),
				Status:  status,
				DSN:     dsn,
				Options: strings.Join(opts, ", "),
			}
			svcTable = append(svcTable, row)
		}
	}

	// Print table.
	// Info header.
	maxTypeLen := len("SERVICE TYPE")
	maxNameLen := len("NAME")
	maxDSNlen := len("DATA SOURCE")
	maxOptsLen := len("OPTIONS")
	for _, in := range svcTable {
		if len(in.Type) > maxTypeLen {
			maxTypeLen = len(in.Type)
		}
		if len(in.Name) > maxNameLen {
			maxNameLen = len(in.Name)
		}
		if len(in.DSN) > maxDSNlen {
			maxDSNlen = len(in.DSN)
		}
		if len(in.Options) > maxOptsLen {
			maxOptsLen = len(in.Options)
		}
	}
	maxTypeLen++
	maxNameLen++
	maxDSNlen++
	maxOptsLen++
	linefmt := "%-" + fmt.Sprintf("%d", maxTypeLen) + "s %-" + fmt.Sprintf("%d", maxNameLen) + "s %-12s %-8s %-" +
		fmt.Sprintf("%d", maxDSNlen) + "s %-" + fmt.Sprintf("%d", maxOptsLen) + "s\n"
	fmt.Printf(linefmt, strings.Repeat("-", maxTypeLen), strings.Repeat("-", maxNameLen), strings.Repeat("-", 12),
		strings.Repeat("-", 8), strings.Repeat("-", maxDSNlen), strings.Repeat("-", maxOptsLen))
	fmt.Printf(linefmt, "SERVICE TYPE", "NAME", "CLIENT PORT", "RUNNING", "DATA SOURCE", "OPTIONS")
	fmt.Printf(linefmt, strings.Repeat("-", maxTypeLen), strings.Repeat("-", maxNameLen), strings.Repeat("-", 12),
		strings.Repeat("-", 8), strings.Repeat("-", maxDSNlen), strings.Repeat("-", maxOptsLen))
	// Data table.
	sort.Sort(sortOutput(svcTable))
	for _, i := range svcTable {
		fmt.Printf(linefmt, i.Type, i.Name, i.Port, i.Status, i.DSN, i.Options)
	}
	fmt.Println()

	return nil
}

// GetInfo print PMM client info.
func (a *Admin) PrintInfo() {
	fmt.Printf("pmm-admin %s\n\n", Version)
	a.ServerInfo()
	fmt.Printf("%-15s | %s\n\n", "Service manager", service.Platform())
}

// ServerInfo print server info.
func (a *Admin) ServerInfo() {
	var labels []string
	if a.Config.ServerInsecureSSL {
		labels = append(labels, "insecure SSL")
	} else if a.Config.ServerSSL {
		labels = append(labels, "SSL")
	}
	if a.Config.ServerUser != "" {
		labels = append(labels, "password-protected")
	}
	securityInfo := ""
	if len(labels) > 0 {
		securityInfo = fmt.Sprintf("(%s)", strings.Join(labels, ", "))
	}

	bindAddress := ""
	if a.Config.ClientAddress != a.Config.BindAddress {
		bindAddress = fmt.Sprintf("(%s)", a.Config.BindAddress)
	}

	fmt.Printf("%-15s | %s %s\n", "PMM Server", a.Config.ServerAddress, securityInfo)
	fmt.Printf("%-15s | %s\n", "Client Name", a.Config.ClientName)
	fmt.Printf("%-15s | %s %s\n", "Client Address", a.Config.ClientAddress, bindAddress)
}

// StartStopMonitoring start/stop system service by its metric type and name.
func (a *Admin) StartStopMonitoring(action, svcType string) error {
	if svcType != "linux:metrics" && svcType != "mysql:metrics" && svcType != "mysql:queries" && svcType != "mongodb:metrics" && svcType != "proxysql:metrics" {
		return errors.New(`bad service type.

Service type takes the following values: linux:metrics, mysql:metrics, mysql:queries, mongodb:metrics, proxysql:metrics.`)
	}

	// Check if we have this service on Consul.
	consulSvc, err := a.getConsulService(svcType, a.ServiceName)
	if err != nil {
		return err
	}
	if consulSvc == nil {
		return ErrNoService
	}

	svcName := fmt.Sprintf("pmm-%s-%d", strings.Replace(svcType, ":", "-", 1), consulSvc.Port)
	switch action {
	case "start":
		if err := startService(svcName); err != nil {
			return err
		}
	case "stop":
		if err := stopService(svcName); err != nil {
			return err
		}
	case "restart":
		if err := stopService(svcName); err != nil {
			return err
		}
		if err := startService(svcName); err != nil {
			return err
		}
	}

	return nil
}

// StartStopAllMonitoring start/stop all metric services.
func (a *Admin) StartStopAllMonitoring(action string) (int, error) {
	node, _, err := a.consulAPI.Catalog().Node(a.Config.ClientName, nil)
	if err != nil || node == nil || len(node.Services) == 0 {
		return 0, nil
	}

	for _, svc := range node.Services {
		svcName := fmt.Sprintf("pmm-%s-%d", strings.Replace(svc.Service, ":", "-", 1), svc.Port)
		switch action {
		case "start":
			if err := startService(svcName); err != nil {
				return 0, err
			}
		case "stop":
			if err := stopService(svcName); err != nil {
				return 0, err
			}
		case "restart":
			if err := stopService(svcName); err != nil {
				return 0, err
			}
			if err := startService(svcName); err != nil {
				return 0, err
			}
		}
	}

	return len(node.Services), nil
}

// RemoveAllMonitoring remove all the monitoring services.
func (a *Admin) RemoveAllMonitoring(ignoreErrors bool) (uint16, error) {
	node, _, err := a.consulAPI.Catalog().Node(a.Config.ClientName, nil)
	if err != nil || node == nil || len(node.Services) == 0 {
		return 0, nil
	}

	var count uint16
	for _, svc := range node.Services {
		for _, tag := range svc.Tags {
			if !strings.HasPrefix(tag, "alias_") {
				continue
			}
			a.ServiceName = tag[6:]
			switch svc.Service {
			case "linux:metrics":
				if err := a.RemoveLinuxMetrics(); err != nil && !ignoreErrors {
					return count, err
				}
			case "mysql:metrics":
				if err := a.RemoveMySQLMetrics(); err != nil && !ignoreErrors {
					return count, err
				}
			case "mysql:queries":
				if err := a.RemoveMySQLQueries(); err != nil && !ignoreErrors {
					return count, err
				}
			case "mongodb:metrics":
				if err := a.RemoveMongoDBMetrics(); err != nil && !ignoreErrors {
					return count, err
				}
			case "proxysql:metrics":
				if err := a.RemoveProxySQLMetrics(); err != nil && !ignoreErrors {
					return count, err
				}
			}
			count++
		}
	}

	return count, nil
}

// PurgeMetrics purge metrics data on the server by its metric type and name.
func (a *Admin) PurgeMetrics(svcType string) (uint, error) {
	if svcType != "linux:metrics" && svcType != "mysql:metrics" && svcType != "mongodb:metrics" && svcType != "proxysql:metrics" {
		return 0, errors.New(`bad service type.

Service type takes the following values: linux:metrics, mysql:metrics, mongodb:metrics, proxysql:metrics.`)
	}

	match := fmt.Sprintf(`{job="%s",instance="%s"}`, strings.Split(svcType, ":")[0], a.ServiceName)
	// XXX need this https://github.com/prometheus/client_golang/pull/248
	//count, err := a.promSeriesAPI.Delete(context.Background(), []string{match})
	//if err != nil {
	//	return 0, err
	//}
	url := a.qanAPI.URL(a.serverURL, fmt.Sprintf("prometheus/api/v1/series?match[]=%s", match))
	_, data, err := a.qanAPI.Delete(url)
	if err != nil {
		return 0, err
	}
	var res map[string]interface{}
	_ = json.Unmarshal(data, &res)
	count := uint(res["data"].(map[string]interface{})["numDeleted"].(float64))

	return count, nil
}

// getConsulService get service from Consul by service type and optionally name (alias).
func (a *Admin) getConsulService(service, name string) (*consul.AgentService, error) {
	node, _, err := a.consulAPI.Catalog().Node(a.Config.ClientName, nil)
	if err != nil || node == nil {
		return nil, err
	}
	for _, svc := range node.Services {
		if svc.Service != service {
			continue
		}
		if name == "" {
			return svc, nil
		}
		for _, tag := range svc.Tags {
			if tag == fmt.Sprintf("alias_%s", name) {
				return svc, nil
			}
		}
	}

	return nil, nil
}

// checkGlobalDuplicateService check if new service is globally unique and prevent duplicate clients.
func (a *Admin) checkGlobalDuplicateService(service, name string) error {
	// Prevent duplicate clients (2 or more nodes using the same name).
	// This should not usually happen unless the config file is edited manually.
	node, _, err := a.consulAPI.Catalog().Node(a.Config.ClientName, nil)
	if err != nil {
		return err
	}
	if node != nil && node.Node.Address != a.Config.ClientAddress && len(node.Services) > 0 {
		return fmt.Errorf(`another client with the same name '%s' but different address detected.

This client address is %s, the other one - %s.
Re-configure this client with the different name using 'pmm-admin config' command.`,
			a.Config.ClientName, a.Config.ClientAddress, node.Node.Address)
	}

	// Check if service with the name (tag) is globally unique.
	services, _, err := a.consulAPI.Catalog().Service(service, fmt.Sprintf("alias_%s", name), nil)
	if err != nil {
		return err
	}
	if len(services) > 0 {
		return fmt.Errorf(`another client '%s' by address '%s' is monitoring %s instance under the name '%s'.

Choose different name for this service.`,
			services[0].Node, services[0].Address, service, name)
	}

	return nil
}

// choosePort automatically choose the port for service.
func (a *Admin) choosePort(port uint16, userDefined bool) (uint16, error) {
	// Check if user defined port is not used.
	if userDefined {
		ok, err := a.availablePort(port)
		if err != nil {
			return port, err
		}
		if ok {
			return port, nil
		}
		return port, fmt.Errorf("port %d is reserved by other service. Choose the different one.", port)
	}
	// Find the first available port starting the default one.
	for i := port; i < port+1000; i++ {
		ok, err := a.availablePort(i)
		if err != nil {
			return port, err
		}
		if ok {
			return i, nil
		}
	}
	return port, fmt.Errorf("ports %d-%d are reserved by other services. Try to specify the other port using --service-port",
		port, port+1000)
}

// availablePort check if port is occupied by any service on Consul.
func (a *Admin) availablePort(port uint16) (bool, error) {
	node, _, err := a.consulAPI.Catalog().Node(a.Config.ClientName, nil)
	if err != nil {
		return false, err
	}
	if node != nil {
		for _, svc := range node.Services {
			if port == uint16(svc.Port) {
				return false, nil
			}
		}
	}
	return true, nil
}

// CheckInstallation check for broken installation.
func (a *Admin) CheckInstallation() ([]string, []string) {
	var (
		dir              string
		extension        string
		services         []string
		orphanedServices []string
		missingServices  []string
	)
	switch service.Platform() {
	case "linux-systemd":
		dir = "/etc/systemd/system"
		extension = ".service"
	case "linux-upstart":
		dir = "/etc/init"
		extension = ".conf"
	case "unix-systemv":
		dir = "/etc/init.d"
		extension = ""
	}

	filesFound, _ := filepath.Glob(fmt.Sprintf("%s/pmm-*%s", dir, extension))
	rService, _ := regexp.Compile(fmt.Sprintf("%s/(pmm-.+)%s", dir, extension))
	for _, f := range filesFound {
		if data := rService.FindStringSubmatch(f); data != nil {
			services = append(services, data[1])
		}
	}

	node, _, err := a.consulAPI.Catalog().Node(a.Config.ClientName, nil)
	if err != nil || node == nil || len(node.Services) == 0 {
		return services, []string{}
	}

	// Find orphaned services: local system services that are not associated with Consul services.
ForLoop1:
	for _, s := range services {
		for _, svc := range node.Services {
			svcName := fmt.Sprintf("pmm-%s-%d", strings.Replace(svc.Service, ":", "-", 1), svc.Port)
			if s == svcName {
				continue ForLoop1
			}
		}
		orphanedServices = append(orphanedServices, s)
	}

	// Find missing services: Consul services that are missing locally.
ForLoop2:
	for _, svc := range node.Services {
		svcName := fmt.Sprintf("pmm-%s-%d", strings.Replace(svc.Service, ":", "-", 1), svc.Port)
		for _, s := range services {
			if s == svcName {
				continue ForLoop2
			}
		}
		missingServices = append(missingServices, svc.ID)
	}

	return orphanedServices, missingServices
}

// RepairInstallation repair installation.
func (a *Admin) RepairInstallation() error {
	orphanedServices, missingServices := a.CheckInstallation()
	// Uninstall local services.
	for _, s := range orphanedServices {
		if err := uninstallService(s); err != nil {
			return err
		}
	}

	// Remove remote services from Consul.
	for _, s := range missingServices {
		dereg := consul.CatalogDeregistration{
			Node:      a.Config.ClientName,
			ServiceID: s,
		}
		if _, err := a.consulAPI.Catalog().Deregister(&dereg, nil); err != nil {
			return err
		}

		prefix := fmt.Sprintf("%s/%s/", a.Config.ClientName, s)

		// Try to delete mysql instances from QAN associated with queries service on KV.
		names, _, err := a.consulAPI.KV().Keys(prefix, "", nil)
		if err == nil {
			for _, name := range names {
				if !strings.HasSuffix(name, "/qan_mysql_uuid") {
					continue
				}
				data, _, err := a.consulAPI.KV().Get(name, nil)
				if err == nil && data != nil {
					a.deleteMySQLinstance(string(data.Value))
				}
			}
		}

		a.consulAPI.KV().DeleteTree(prefix, nil)
	}

	if len(orphanedServices) > 0 || len(missingServices) > 0 {
		fmt.Printf("OK, removed %d orphaned services.\n", len(orphanedServices)+len(missingServices))
	} else {
		fmt.Println("No orphaned services found.")
	}
	return nil
}

// Uninstall remove all monitoring services with the best effort.
func (a *Admin) Uninstall(flagConfigFile string) uint16 {
	var count uint16
	if FileExists(flagConfigFile) {
		err := a.LoadConfig(flagConfigFile)
		if err == nil {
			a.apiTimeout = 5 * time.Second
			if err := a.SetAPI(); err == nil {
				// Try remove all services normally ignoring the errors.
				count, _ = a.RemoveAllMonitoring(true)
			}
		}
	}

	var dir, extension string
	switch service.Platform() {
	case "linux-systemd":
		dir = "/etc/systemd/system"
		extension = ".service"
	case "linux-upstart":
		dir = "/etc/init"
		extension = ".conf"
	case "unix-systemv":
		dir = "/etc/init.d"
		extension = ""
	}

	// Find any local PMM services and try to uninstall ignoring the errors.
	filesFound, _ := filepath.Glob(fmt.Sprintf("%s/pmm-*%s", dir, extension))
	rService, _ := regexp.Compile(fmt.Sprintf("%s/(pmm-.+)%s", dir, extension))
	for _, f := range filesFound {
		data := rService.FindStringSubmatch(f)
		if data == nil {
			continue
		}
		if err := uninstallService(data[1]); err == nil {
			count++
		}
	}

	return count
}

// FileExists check if file exists.
func FileExists(file string) bool {
	_, err := os.Stat(file)
	if err == nil {
		return true
	}
	if os.IsNotExist(err) {
		return false
	}
	return true
}

// SanitizeDSN remove password from DSN
func SanitizeDSN(dsn string) string {
	dsn = strings.TrimRight(strings.Split(dsn, "?")[0], "/")
	if strings.HasPrefix(dsn, "mongodb://") {
		dsn = dsn[10:]
	}

	if strings.Index(dsn, "@") > 0 {
		dsnParts := strings.Split(dsn, "@")
		userPart := dsnParts[0]
		hostPart := ""
		if len(dsnParts) > 1 {
			hostPart = dsnParts[len(dsnParts)-1]
		}
		userPasswordParts := strings.Split(userPart, ":")
		dsn = fmt.Sprintf("%s:***@%s", userPasswordParts[0], hostPart)
	}
	return dsn
}

// CheckBinaries check if all PMM Client binaries are at their paths
func CheckBinaries() string {
	paths := []string{
		fmt.Sprintf("%s/node_exporter", PMMBaseDir),
		fmt.Sprintf("%s/mysqld_exporter", PMMBaseDir),
		fmt.Sprintf("%s/mongodb_exporter", PMMBaseDir),
		fmt.Sprintf("%s/proxysql_exporter", PMMBaseDir),
		fmt.Sprintf("%s/bin/percona-qan-agent", agentBaseDir),
		fmt.Sprintf("%s/bin/percona-qan-agent-installer", agentBaseDir),
	}
	for _, p := range paths {
		if !FileExists(p) {
			return p
		}
	}
	return ""
}

// Sort rows of formatted table output (list, check-networks commands).
type sortOutput []instanceStatus

func (s sortOutput) Len() int {
	return len(s)
}

func (s sortOutput) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func (s sortOutput) Less(i, j int) bool {
	if strings.Compare(s[i].Port, s[j].Port) == -1 {
		return true
	}
	return false
}
