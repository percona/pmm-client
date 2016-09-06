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
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"sort"
	"strings"

	consul "github.com/hashicorp/consul/api"
	"github.com/percona/kardianos-service"
	"github.com/prometheus/client_golang/api/prometheus"
	"gopkg.in/yaml.v2"
)

// PMM client config structure.
type Config struct {
	ServerAddress     string `yaml:"server_address"`
	ClientAddress     string `yaml:"client_address"`
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
	ServiceName string
	ServicePort uint
	Config      *Config
	filename    string
	serverUrl   string
	qanapi      *API
	consulapi   *consul.Client
	promapi     prometheus.QueryAPI
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
	return nil
}

// SetConfig configure PMM client, check connectivity and write the config.
func (a *Admin) SetConfig(cf Config) error {
	// Server options.
	if cf.ServerSSL && cf.ServerInsecureSSL {
		return fmt.Errorf("Flags --server-ssl and --server-insecure-ssl are mutually exclusive.")
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
		return fmt.Errorf("Server address is not set. Use --server flag to set it.")
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
	// Attempt to change client name.
	if cf.ClientName != "" && a.Config.ClientName != "" && cf.ClientName != a.Config.ClientName {
		// Checking source name.
		node, _, err := a.consulapi.Catalog().Node(a.Config.ClientName, nil)
		if err != nil {
			return fmt.Errorf("Unable to communicate with Consul: %s", err)
		}
		if node != nil && len(node.Services) > 0 {
			return fmt.Errorf("Changing of client name is allowed only if there are no services under monitoring.")
		}

		// Checking target name.
		node, _, err = a.consulapi.Catalog().Node(cf.ClientName, nil)
		if err != nil {
			return fmt.Errorf("Unable to communicate with Consul: %s", err)
		}
		if node != nil && len(node.Services) > 0 {
			return fmt.Errorf(`Another client with the same name '%s' detected, by the address %s.
It has the active services so you cannot change client name to this one.`,
				cf.ClientName, node.Node.Address)
		}
		// Allow to change below.
	}

	if cf.ClientName != "" {
		a.Config.ClientName = cf.ClientName
	}
	if a.Config.ClientName == "" {
		hostname, _ := os.Hostname()
		a.Config.ClientName = hostname
	}

	// Attempt to change client address.
	if cf.ClientAddress != "" && a.Config.ClientAddress != "" && cf.ClientAddress != a.Config.ClientAddress {
		node, _, err := a.consulapi.Catalog().Node(a.Config.ClientName, nil)
		if err != nil {
			return fmt.Errorf("Unable to communicate with Consul: %s", err)
		}
		if node != nil && len(node.Services) > 0 {
			return fmt.Errorf("Changing of client address is allowed only if there are no services under monitoring.")
		}
		// Allow to change below.
	}

	if cf.ClientAddress != "" {
		a.Config.ClientAddress = cf.ClientAddress
	}
	if a.Config.ClientAddress == "" {
		// If user does not define client address, we try to detect it from nginx response header.
		a.Config.ClientAddress = a.getMyRemoteIP()
		if a.Config.ClientAddress == "" {
			return fmt.Errorf("Client address is not set and cannot be detected. Use --client-address flag to set it.")
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
		HttpClient: &http.Client{Timeout: apiTimeout},
		Scheme:     scheme,
	}
	if a.Config.ServerInsecureSSL {
		config.HttpClient.Transport = insecureTransport
	}
	if a.Config.ServerUser != "" {
		config.HttpAuth = &consul.HttpBasicAuth{Username: a.Config.ServerUser, Password: a.Config.ServerPassword}
	}
	a.consulapi, _ = consul.NewClient(&config)

	// Full URL.
	a.serverUrl = fmt.Sprintf("%s://%s:%s@%s", scheme, a.Config.ServerUser, a.Config.ServerPassword, a.Config.ServerAddress)

	// QAN API.
	a.qanapi = NewAPI(a.Config.ServerInsecureSSL)

	// Prometheus API.
	cfg := prometheus.Config{Address: fmt.Sprintf("%s/prometheus", a.serverUrl)}
	if a.Config.ServerInsecureSSL {
		cfg.Transport = insecureTransport
	}
	client, _ := prometheus.New(cfg)
	a.promapi = prometheus.NewQueryAPI(client)

	// Check if server is alive.
	// Try to detect 404 (in case of SSL) and 403 (in case of HTTP auth) first to point this to user.
	url := a.qanapi.URL(a.serverUrl)
	resp, _, err := a.qanapi.Get(url)
	if err != nil {
		return fmt.Errorf(`Unable to connect to PMM server by address: %s

* Check if the configured address is correct.
* If server is running on non-default port, ensure it was specified along with the address.
* You may also check the firewall settings.`, a.Config.ServerAddress)
	}
	if err == nil && resp.StatusCode == http.StatusBadRequest {
		return fmt.Errorf(`Unable to connect to PMM server by address: %s

Looks like the server is enabled for SSL or self-signed SSL.
Use 'pmm-admin config' to enable the corresponding option.`, a.Config.ServerAddress)
	}
	if err == nil && resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf(`Unable to connect to PMM server by address: %s

Looks like the server is password protected.
Use 'pmm-admin config' to define server user and password.`, a.Config.ServerAddress)
	}

	// Finally, check Consul status.
	if leader, err := a.consulapi.Status().Leader(); err != nil || leader != "127.0.0.1:8300" {
		return fmt.Errorf(`Unable to connect to PMM server by address: %s

* Check if the configured address is correct.
* If server is running on non-default port, ensure it was specified along with the address.
* You may also check the firewall settings.`, a.Config.ServerAddress)
	}

	return nil
}

// getMyRemoteIP get client remote IP from nginx custom header X-Remote-IP.
func (a *Admin) getMyRemoteIP() string {
	url := a.qanapi.URL(a.serverUrl, "v1/status/leader")
	resp, _, err := a.qanapi.Get(url)
	if err != nil {
		return ""
	}
	return resp.Header.Get("X-Remote-IP")
}

// writeConfig write config to the file.
func (a *Admin) writeConfig() error {
	bytes, _ := yaml.Marshal(a.Config)
	return ioutil.WriteFile(a.filename, bytes, 0644)
}

// List get all services from Consul.
func (a *Admin) List() error {
	node, _, err := a.consulapi.Catalog().Node(a.Config.ClientName, nil)
	if err != nil || node == nil {
		fmt.Printf("%s '%s'.\n\n", noMonitoring, a.Config.ClientName)
		return nil
	}

	if len(node.Services) == 0 {
		fmt.Println("No services under monitoring.\n")
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
		if data, _, err := a.consulapi.KV().List(prefix, nil); err == nil {
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
			if data, _, err := a.consulapi.KV().List(prefix, nil); err == nil {
				for _, kvp := range data {
					key := kvp.Key[len(prefix):]
					switch key {
					case "dsn":
						dsn = string(kvp.Value)
					case "query_source":
						opts = append(opts, fmt.Sprintf("%s=%s", key, kvp.Value))
					}
					// We don't need other, e.g. qan_mysql_uuid.
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
	fmt.Printf("pmm-admin %s\n\n", VERSION)
	a.ServerInfo()
	fmt.Printf("%-15s | %s\n\n", "Service manager", service.Platform())
}

// ServerInfo print server info.
func (a *Admin) ServerInfo() {
	var labels []string
	if a.Config.ServerSSL {
		labels = append(labels, "SSL")
	}
	if a.Config.ServerInsecureSSL {
		labels = append(labels, "insecure SSL")
	}
	if a.Config.ServerUser != "" {
		labels = append(labels, "password-protected")
	}
	info := ""
	if len(labels) > 0 {
		info = fmt.Sprintf("(%s)", strings.Join(labels, ", "))
	}
	fmt.Printf("%-15s | %s %s\n", "PMM Server", a.Config.ServerAddress, info)
	fmt.Printf("%-15s | %s\n", "Client Name", a.Config.ClientName)
	fmt.Printf("%-15s | %s\n", "Client Address", a.Config.ClientAddress)
}

// StartStopMonitoring start/stop system service by its metric type and name.
func (a *Admin) StartStopMonitoring(action, svcType, name string) error {
	// Check if we have this service on Consul.
	consulSvc, err := a.getConsulService(svcType, name)
	if err != nil {
		return err
	}
	if consulSvc == nil {
		return ErrNoService
	}

	if action == "start" {
		if err := startService(fmt.Sprintf("pmm-%s-%d", strings.Replace(svcType, ":", "-", 1), consulSvc.Port)); err != nil {
			return err
		}
		return nil
	}
	if err := stopService(fmt.Sprintf("pmm-%s-%d", strings.Replace(svcType, ":", "-", 1), consulSvc.Port)); err != nil {
		return err
	}

	return nil
}

// StartStopAllMonitoring start/stop all metric services.
func (a *Admin) StartStopAllMonitoring(action string) (error, int) {
	node, _, err := a.consulapi.Catalog().Node(a.Config.ClientName, nil)
	if err != nil || node == nil || len(node.Services) == 0 {
		return nil, 0
	}

	for _, svc := range node.Services {
		metric := svc.Service
		if action == "start" {
			if err := startService(fmt.Sprintf("pmm-%s-%d", strings.Replace(metric, ":", "-", 1), svc.Port)); err != nil {
				return err, 0
			}
			continue
		}
		if err := stopService(fmt.Sprintf("pmm-%s-%d", strings.Replace(metric, ":", "-", 1), svc.Port)); err != nil {
			return err, 0
		}
	}

	return nil, len(node.Services)
}

// RemoveAllMonitoring remove all the monitoring services.
func (a *Admin) RemoveAllMonitoring(force bool) (error, int) {
	node, _, err := a.consulapi.Catalog().Node(a.Config.ClientName, nil)
	if err != nil || node == nil || len(node.Services) == 0 {
		return nil, 0
	}

	for _, svc := range node.Services {
		// Stop and uninstall service.
		name := fmt.Sprintf("pmm-%s-%d", strings.Replace(svc.Service, ":", "-", 1), svc.Port)
		if err := uninstallService(name); err != nil && !force {
			return err, 0
		}

		// Remove service from Consul.
		dereg := consul.CatalogDeregistration{
			Node:      a.Config.ClientName,
			ServiceID: svc.ID,
		}
		if _, err := a.consulapi.Catalog().Deregister(&dereg, nil); err != nil && !force {
			return err, 0
		}
	}

	return nil, len(node.Services)
}

// getConsulService get service from Consul by service type and optionally name (alias).
func (a *Admin) getConsulService(service, name string) (*consul.AgentService, error) {
	node, _, err := a.consulapi.Catalog().Node(a.Config.ClientName, nil)
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
	node, _, err := a.consulapi.Catalog().Node(a.Config.ClientName, nil)
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
	services, _, err := a.consulapi.Catalog().Service(service, fmt.Sprintf("alias_%s", name), nil)
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
func (a *Admin) choosePort(port uint, userDefined bool) (uint, error) {
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
func (a *Admin) availablePort(port uint) (bool, error) {
	node, _, err := a.consulapi.Catalog().Node(a.Config.ClientName, nil)
	if err != nil {
		return false, err
	}
	if node != nil {
		for _, svc := range node.Services {
			if port == uint(svc.Port) {
				return false, nil
			}
		}
	}
	return true, nil
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
