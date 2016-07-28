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
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	consul "github.com/hashicorp/consul/api"
	"github.com/roman-vynar/service"
	"gopkg.in/yaml.v2"
)

// PMM client config structure.
type Config struct {
	ServerAddress string `yaml:"server_address"`
	ClientAddress string `yaml:"client_address"`
	ClientName    string `yaml:"client_name"`
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
	qanapi      *API
	consulapi   *consul.Client
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

// SetConfig configure PMM client with server and client addresses, write them to the config.
func (a *Admin) SetConfig(serverAddress, clientAddress, clientName string) error {
	if (clientAddress != "" && a.Config.ClientAddress != "" && clientAddress != a.Config.ClientAddress) ||
		(clientName != "" && a.Config.ClientName != "" && clientName != a.Config.ClientName) {
		return fmt.Errorf("changing of client address or name will disassociate this client from PMM server.")
	}

	if serverAddress != "" {
		a.Config.ServerAddress = serverAddress
	}
	if clientAddress != "" {
		a.Config.ClientAddress = clientAddress
	}
	if clientName != "" {
		a.Config.ClientName = clientName
	}

	bytes, _ := yaml.Marshal(a.Config)
	return ioutil.WriteFile(a.filename, bytes, 0644)
}

// SetAPI setup Consul and QAN APIs.
func (a *Admin) SetAPI() {
	config := consul.Config{
		Address:    a.Config.ServerAddress,
		HttpClient: &http.Client{Timeout: 30 * time.Second},
	}
	a.consulapi, _ = consul.NewClient(&config)
	a.qanapi = NewAPI(nil)
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

	// Parse all services except queries.
	var queryService *consul.AgentService
	var svcTable []instanceStatus
	for _, svc := range node.Services {
		metricType := svc.Service
		if metricType == "queries" {
			queryService = svc
			continue
		}

		port := fmt.Sprintf("%d", svc.Port)
		status := "NO"
		if metricType == "mysql-hr" {
			metricType = "mysql"
			port = fmt.Sprintf("%d-%d", svc.Port, svc.Port+2)
		} else if metricType == "mysql-mr" || metricType == "mysql-lr" {
			// If mysqld_exporter for mysql-hr job is running, we consider "mysql" status as running.
			continue
		}
		if getServiceStatus(fmt.Sprintf("pmm-%s-exporter-%d", metricType, svc.Port)) {
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
			Type:    metricType,
			Name:    name,
			Port:    port,
			Status:  status,
			DSN:     dsn,
			Options: strings.Join(opts, ", "),
		}
		svcTable = append(svcTable, row)
	}

	// Parse queries service.
	if queryService != nil {
		status := "NO"
		if getServiceStatus(fmt.Sprintf("pmm-queries-exporter-%d", queryService.Port)) {
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
				Type:    "queries",
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
	maxNameLen := 5
	maxDSNlen := 12
	maxOptsLen := 7
	for _, in := range svcTable {
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
	maxNameLen++
	maxDSNlen++
	maxOptsLen++
	linefmt := "%-15s %-" + fmt.Sprintf("%d", maxNameLen) + "s %-12s %-8s %-" +
		fmt.Sprintf("%d", maxDSNlen) + "s %-" + fmt.Sprintf("%d", maxOptsLen) + "s\n"
	fmt.Printf(linefmt, strings.Repeat("-", 15), strings.Repeat("-", maxNameLen), strings.Repeat("-", 12),
		strings.Repeat("-", 8), strings.Repeat("-", maxDSNlen), strings.Repeat("-", maxOptsLen))
	fmt.Printf(linefmt, "METRIC SERVICE", "NAME", "CLIENT PORT", "RUNNING", "DATA SOURCE", "OPTIONS")
	fmt.Printf(linefmt, strings.Repeat("-", 15), strings.Repeat("-", maxNameLen), strings.Repeat("-", 12),
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
	fmt.Printf("%-15s | %s\n", "PMM Server", a.Config.ServerAddress)
	fmt.Printf("%-15s | %s\n", "Client Name", a.Config.ClientName)
	fmt.Printf("%-15s | %s\n", "Client Address", a.Config.ClientAddress)
	fmt.Printf("%-15s | %s\n\n", "Service manager", service.Platform())
}

// ServerAlive check if PMM server is alive.
func (a *Admin) ServerAlive() bool {
	leader, err := a.consulapi.Status().Leader()
	if err == nil && leader == "127.0.0.1:8300" {
		return true
	}
	return false
}

// StartStopMonitoring start/stop system service by its metric type and name.
func (a *Admin) StartStopMonitoring(action, metric, name string) error {
	consulMetric := metric
	if metric == "mysql" {
		consulMetric = "mysql-hr"
	}
	// Check if we have this service on Consul.
	consulSvc, err := a.getConsulService(consulMetric, name)
	if err != nil {
		return err
	}
	if consulSvc == nil {
		return errNoService
	}

	endPort := consulSvc.Port
	if metric == "mysql" {
		endPort = consulSvc.Port + 2
	}
	for i := consulSvc.Port; i <= endPort; i++ {
		if action == "start" {
			if err := startService(fmt.Sprintf("pmm-%s-exporter-%d", metric, i)); err != nil {
				return err
			}
			continue
		}
		if err := stopService(fmt.Sprintf("pmm-%s-exporter-%d", metric, i)); err != nil {
			return err
		}
	}

	return nil
}

// StartStopAllMonitoring start/stop all metric services.
func (a *Admin) StartStopAllMonitoring(action string) (error, bool) {
	node, _, err := a.consulapi.Catalog().Node(a.Config.ClientName, nil)
	if err != nil || node == nil || len(node.Services) == 0 {
		return nil, true
	}

	for _, svc := range node.Services {
		metric := svc.Service
		if metric == "mysql-hr" || metric == "mysql-mr" || metric == "mysql-lr" {
			metric = "mysql"
		}
		if action == "start" {
			if err := startService(fmt.Sprintf("pmm-%s-exporter-%d", metric, svc.Port)); err != nil {
				return err, false
			}
			continue
		}
		if err := stopService(fmt.Sprintf("pmm-%s-exporter-%d", metric, svc.Port)); err != nil {
			return err, false
		}
	}

	return nil, false
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
		return port, fmt.Errorf("port %d is used by other service. Choose the different one.", port)
	}
	// Find the first available port starting the given one.
	for i := port; i < port+50; i++ {
		ok, err := a.availablePort(i)
		if err != nil {
			return port, err
		}
		if ok {
			return i, nil
		}
	}
	return port, fmt.Errorf("ports %d-%d are used by other service. Try to specify the other port using --service-port",
		port, port+50)
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
