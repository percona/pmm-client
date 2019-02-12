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
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/docker/cli/templates"
	"github.com/fatih/color"
	consul "github.com/hashicorp/consul/api"
	"github.com/percona/kardianos-service"
	"github.com/percona/pmm/version"
	"github.com/prometheus/client_golang/api/prometheus"

	"github.com/percona/pmm-client/pmm/managed"
)

// Admin main class.
type Admin struct {
	ServiceName  string
	ServicePort  int
	Args         []string // Args defines additional arguments to pass through to *_exporter or qan-agent
	Config       *Config
	Verbose      bool
	SkipAdmin    bool
	Format       string
	serverURL    string
	apiTimeout   time.Duration
	qanAPI       *API
	consulAPI    *consul.Client
	promQueryAPI prometheus.QueryAPI
	managedAPI   *managed.Client
	//promSeriesAPI prometheus.SeriesAPI
}

// SetAPI setups QAN, Consul, Prometheus, pmm-managed clients and verifies connections.
func (a *Admin) SetAPI() error {
	// Set default API timeout if unset.
	if a.apiTimeout == 0 {
		a.apiTimeout = apiTimeout
	}

	scheme := "http"
	helpText := ""
	insecureTransport := &http.Transport{}
	if a.Config.ServerInsecureSSL {
		scheme = "https"
		insecureTransport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
		helpText = "--server-insecure-ssl"
	}
	if a.Config.ServerSSL {
		scheme = "https"
		helpText = "--server-ssl"
	}

	// QAN API.
	a.qanAPI = NewAPI(a.Config.ServerInsecureSSL, a.apiTimeout, a.Verbose)
	httpClient := a.qanAPI.NewClient()

	// Consul API.
	config := consul.Config{
		Address:    a.Config.ServerAddress,
		HttpClient: httpClient,
		Scheme:     scheme,
	}
	var authStr string
	if a.Config.ServerUser != "" {
		config.HttpAuth = &consul.HttpBasicAuth{
			Username: a.Config.ServerUser,
			Password: a.Config.ServerPassword,
		}
		authStr = fmt.Sprintf("%s:%s@", url.QueryEscape(a.Config.ServerUser), url.QueryEscape(a.Config.ServerPassword))
	}
	a.consulAPI, _ = consul.NewClient(&config)

	// Full URL.
	a.serverURL = fmt.Sprintf("%s://%s%s", scheme, authStr, a.Config.ServerAddress)

	// Prometheus API.
	cfg := prometheus.Config{Address: fmt.Sprintf("%s/prometheus", a.serverURL)}
	// cfg.Transport = httpClient.Transport
	// above should be used instead below but
	// https://github.com/prometheus/client_golang/issues/292
	if a.Config.ServerInsecureSSL {
		cfg.Transport = insecureTransport
	}
	client, _ := prometheus.New(cfg)
	a.promQueryAPI = prometheus.NewQueryAPI(client)
	//a.promSeriesAPI = prometheus.NewSeriesAPI(client)

	// Check if server is alive.
	qanApiURL := a.qanAPI.URL(a.serverURL, qanAPIBasePath, "ping")
	resp, _, err := a.qanAPI.Get(qanApiURL)
	if err != nil {
		if strings.Contains(err.Error(), "x509: cannot validate certificate") {
			return fmt.Errorf(`Unable to connect to PMM server by address: %s

Looks like PMM server running with self-signed SSL certificate.
Run 'pmm-admin config --server-insecure-ssl' to enable such configuration.`, a.Config.ServerAddress)
		}
		serverURL := fmt.Sprintf("%s://%s", scheme, a.Config.ServerAddress)
		cleanedErr := strings.Replace(err.Error(), a.serverURL, serverURL, -1)
		return fmt.Errorf(`Unable to connect to PMM server by address: %s
%s

* Check if the configured address is correct.
* If server is running on non-default port, ensure it was specified along with the address.
* If server is enabled for SSL or self-signed SSL, enable the corresponding option.
* You may also check the firewall settings.`, a.Config.ServerAddress, cleanedErr)
	}

	// Try to detect 400 (SSL) and 401 (HTTP auth).
	if resp.StatusCode == http.StatusBadRequest {
		return fmt.Errorf(`Unable to connect to PMM server by address: %s

Looks like the server is enabled for SSL or self-signed SSL.
Use 'pmm-admin config' to enable the corresponding SSL option.`, a.Config.ServerAddress)
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf(`Unable to connect to PMM server by address: %s

Looks like the server is password protected.
Use 'pmm-admin config' to define server user and password.`, a.Config.ServerAddress)
	}

	// Check Consul status.
	if leader, err := a.consulAPI.Status().Leader(); err != nil || leader == "" {
		return fmt.Errorf(`Unable to connect to PMM server by address: %s

Even though the server is reachable it does not look to be PMM server.
Check if the configured address is correct. %s`, a.Config.ServerAddress, err)
	}

	// Check if server is not password protected but client is configured so.
	if a.Config.ServerUser != "" {
		serverURL := fmt.Sprintf("%s://%s", scheme, a.Config.ServerAddress)
		qanApiURL = a.qanAPI.URL(serverURL, qanAPIBasePath, "ping")
		if resp, _, err := a.qanAPI.Get(qanApiURL); err == nil && resp.StatusCode == http.StatusOK {
			return fmt.Errorf(`This client is configured with HTTP basic authentication.
However, PMM server is not.

If you forgot to enable password protection on the server, you may want to do so.

Otherwise, run the following command to reset the config and disable authentication:
pmm-admin config --server %s %s`, a.Config.ServerAddress, helpText)
		}
	}

	var user *url.Userinfo
	if a.Config.ServerUser != "" {
		user = url.UserPassword(a.Config.ServerUser, a.Config.ServerPassword)
	}
	a.managedAPI = managed.NewClient(a.Config.ServerAddress, scheme, user, a.Config.ServerInsecureSSL, a.Verbose)

	return nil
}

// PrintInfo print PMM client info.
func (a *Admin) PrintInfo() {
	fmt.Printf("pmm-admin %s\n\n", Version)
	a.ServerInfo()
	fmt.Printf("%-15s | %s\n\n", "Service Manager", service.Platform())

	fmt.Printf("%-15s | %s\n", "Go Version", strings.Replace(runtime.Version(), "go", "", 1))
	fmt.Printf("%-15s | %s/%s\n\n", "Runtime Info", runtime.GOOS, runtime.GOARCH)
}

const (
	ServerInfoTemplate = `{{define "ServerInfo"}}{{printf "%-15s | %s %s" "PMM Server" .ServerAddress .ServerSecurity}}
{{printf "%-15s | %s" "Client Name" .ClientName}}
{{printf "%-15s | %s %s" "Client Address" .ClientAddress .ClientBindAddress}}{{end}}`

	DefaultServerInfoTemplate = `{{template "ServerInfo" .}}
`
)

type ServerInfo struct {
	ServerAddress     string
	ServerSecurity    string
	ClientName        string
	ClientAddress     string
	ClientBindAddress string
}

// ServerInfo print server info.
func (a *Admin) ServerInfo() error {
	serverInfo := a.serverInfo()

	tmpl, err := templates.Parse(DefaultServerInfoTemplate)
	if err != nil {
		return err
	}
	tmpl, err = tmpl.Parse(ServerInfoTemplate)
	if err != nil {
		return err
	}
	if err := tmpl.Execute(os.Stdout, serverInfo); err != nil {
		return err
	}

	return nil
}

func (a *Admin) serverInfo() ServerInfo {
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

	return ServerInfo{
		ServerAddress:     a.Config.ServerAddress,
		ServerSecurity:    securityInfo,
		ClientName:        a.Config.ClientName,
		ClientAddress:     a.Config.ClientAddress,
		ClientBindAddress: bindAddress,
	}
}

// StartStopMonitoring start/stop system service by its metric type and name.
func (a *Admin) StartStopMonitoring(action, svcType string) (affected bool, err error) {
	err = isValidSvcType(svcType)
	if err != nil {
		return false, err
	}

	// Check if we have this service on Consul.
	consulSvc, err := a.getConsulService(svcType, a.ServiceName)
	if err != nil {
		return false, err
	}
	if consulSvc == nil {
		return false, ErrNoService
	}

	svcName := fmt.Sprintf("pmm-%s-%d", strings.Replace(svcType, ":", "-", 1), consulSvc.Port)
	switch action {
	case "start":
		if getServiceStatus(svcName) {
			// if it's already started then return
			return false, nil
		}
		if err := startService(svcName); err != nil {
			return false, err
		}
	case "stop":
		if !getServiceStatus(svcName) {
			// if it's already stopped then return
			return false, nil
		}
		if err := stopService(svcName); err != nil {
			return false, err
		}
	case "restart":
		if err := stopService(svcName); err != nil {
			return false, err
		}
		if err := startService(svcName); err != nil {
			return false, err
		}
	}

	return true, nil
}

// StartStopAllMonitoring start/stop all metric services.
func (a *Admin) StartStopAllMonitoring(action string) (numOfAffected, numOfAll int, err error) {
	var errs Errors

	localServices := GetLocalServices()
	numOfAll = len(localServices)

	for _, svcName := range localServices {
		switch action {
		case "start":
			if getServiceStatus(svcName) {
				// if it's already started then continue
				continue
			}
			if err := startService(svcName); err != nil {
				errs = append(errs, err)
				continue
			}
		case "stop":
			if !getServiceStatus(svcName) {
				// if it's already stopped then continue
				continue
			}
			if err := stopService(svcName); err != nil {
				errs = append(errs, err)
				continue
			}
		case "restart":
			if err := stopService(svcName); err != nil {
				errs = append(errs, err)
				continue
			}
			if err := startService(svcName); err != nil {
				errs = append(errs, err)
				continue
			}
		}
		numOfAffected++
	}

	if len(errs) > 0 {
		return numOfAffected, numOfAll, errs
	}

	return numOfAffected, numOfAll, nil
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
				if err := a.RemoveMetrics("linux"); err != nil && !ignoreErrors {
					return count, err
				}
			case "mysql:metrics":
				if err := a.RemoveMetrics("mysql"); err != nil && !ignoreErrors {
					return count, err
				}
			case "mysql:queries":
				if err := a.RemoveQueries("mysql"); err != nil && !ignoreErrors {
					return count, err
				}
			case "mongodb:metrics":
				if err := a.RemoveMetrics("mongodb"); err != nil && !ignoreErrors {
					return count, err
				}
			case "mongodb:queries":
				if err := a.RemoveQueries("mongodb"); err != nil && !ignoreErrors {
					return count, err
				}
			case "postgresql:metrics":
				if err := a.RemoveMetrics("postgresql"); err != nil && !ignoreErrors {
					return count, err
				}
			case "proxysql:metrics":
				if err := a.RemoveMetrics("proxysql"); err != nil && !ignoreErrors {
					return count, err
				}
			}
			count++
		}
	}

	// PMM-606: Remove generated password.
	a.Config.MySQLPassword = ""
	a.writeConfig()

	return count, nil
}

// PurgeMetrics purge metrics data on the server by its metric type and name.
func (a *Admin) PurgeMetrics(svcType string) error {
	if svcType != "linux:metrics" && svcType != "mysql:metrics" && svcType != "mongodb:metrics" && svcType != "proxysql:metrics" && svcType != "postgresql:metrics" {
		return errors.New(`bad service type.

Service type takes the following values: linux:metrics, mysql:metrics, mongodb:metrics, proxysql:metrics, postgresql:metrics.`)
	}

	var promError error

	// Delete series in Prometheus v1.
	match := fmt.Sprintf(`{job="%s",instance="%s"}`, strings.Split(svcType, ":")[0], a.ServiceName)
	url := a.qanAPI.URL(a.serverURL, fmt.Sprintf("prometheus1/api/v1/series?match[]=%s", match))
	resp, _, err := a.qanAPI.Delete(url)
	if err != nil || resp.StatusCode != http.StatusOK {
		promError = fmt.Errorf("%v:%v resp: %v", promError, err, resp)
	}

	// Delete series in Prometheus v2.
	url = a.qanAPI.URL(a.serverURL, fmt.Sprintf("prometheus/api/v1/admin/tsdb/delete_series?match[]=%s", match))
	resp, _, err = a.qanAPI.Post(url, []byte{})
	if err != nil || resp.StatusCode != http.StatusNoContent {
		promError = fmt.Errorf("%v:%v resp: %v", promError, err, resp)
	}

	// Clean tombstones in Prometheus v2.
	url = a.qanAPI.URL(a.serverURL, "prometheus/api/v1/admin/tsdb/clean_tombstones")
	resp, _, err = a.qanAPI.Post(url, []byte{})
	if err != nil || resp.StatusCode != http.StatusNoContent {
		promError = fmt.Errorf("%v:%v resp: %v", promError, err, resp)
	}

	return promError
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
func (a *Admin) choosePort(port int, defaultPort int) (int, error) {
	// If port is already defined then just verify that port.
	if port > 0 {
		// Check if user defined port is not used.
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
	for i := defaultPort; i < defaultPort+1000; i++ {
		ok, err := a.availablePort(i)
		if err != nil {
			return i, err
		}
		if ok {
			return i, nil
		}
	}
	return port, fmt.Errorf("ports %d-%d are reserved by other services. Try to specify the other port using --service-port",
		port, port+1000)
}

// availablePort check if port is occupied by any service on Consul.
func (a *Admin) availablePort(port int) (bool, error) {
	node, _, err := a.consulAPI.Catalog().Node(a.Config.ClientName, nil)
	if err != nil {
		return false, err
	}
	if node != nil {
		for _, svc := range node.Services {
			if port == svc.Port {
				return false, nil
			}
		}
	}
	return true, nil
}

// checkSSLCertificate check if SSL cert and key files exist and generate them if not.
func (a *Admin) checkSSLCertificate() error {
	if FileExists(SSLCertFile) && FileExists(SSLKeyFile) {
		return nil
	}

	// Generate SSL cert and key.
	return generateSSLCertificate(a.Config.ClientAddress, SSLCertFile, SSLKeyFile)
}

// CheckVersion check server and client versions and returns boolean and error; boolean is true if error is fatal.
func (a *Admin) CheckVersion(ctx context.Context) (fatal bool, err error) {
	clientVersion, err := version.Parse(Version)
	if err != nil {
		return true, err
	}
	versionResponse, err := a.managedAPI.VersionGet(ctx)
	if err != nil {
		return true, err
	}
	serverVersion, err := version.Parse(versionResponse.Version)
	if err != nil {
		return true, err
	}

	// Return fatal error if major versions do not match.
	// Texts are slightly different, including anchors.
	if serverVersion.Major < clientVersion.Major {
		return true, fmt.Errorf(
			"Error: You cannot run PMM Server %d.x with PMM Client %d.x.\n"+
				"Please upgrade PMM Server by following the instructions at "+
				"https://www.percona.com/doc/percona-monitoring-and-management/deploy/index.html#deploy-pmm-updating",
			serverVersion.Major, clientVersion.Major,
		)
	}
	if serverVersion.Major > clientVersion.Major {
		return true, fmt.Errorf(
			"Error: You cannot run PMM Server %d.x with PMM Client %d.x.\n"+
				"Please upgrade PMM Client by following the instructions at "+
				"https://www.percona.com/doc/percona-monitoring-and-management/deploy/index.html#updating",
			serverVersion.Major, clientVersion.Major,
		)
	}

	// Return warning if versions do not match.
	if serverVersion.Less(&clientVersion) {
		return false, fmt.Errorf(
			"Warning: The recommended upgrade process is to upgrade PMM Server first, then PMM Clients.\n" +
				"See Percona's instructions for upgrading at " +
				"https://www.percona.com/doc/percona-monitoring-and-management/deploy/index.html#deploy-pmm-updating.",
		)
	}
	if clientVersion.Less(&serverVersion) {
		return false, fmt.Errorf(
			"Warning: It is recommended to use the same version on both PMM Server and Client, otherwise some features will not work correctly.\n" +
				"Please upgrade your PMM Client by following the instructions from " +
				"https://www.percona.com/doc/percona-monitoring-and-management/deploy/index.html#updating",
		)
	}

	return false, nil
}

// CheckInstallation check for broken installation.
func (a *Admin) CheckInstallation() (orphanedServices, missingServices []string) {
	localServices := GetLocalServices()

	node, _, err := a.consulAPI.Catalog().Node(a.Config.ClientName, nil)
	if err != nil || node == nil || len(node.Services) == 0 {
		return localServices, []string{}
	}

	// Find orphaned services: local system services that are not associated with Consul services.
ForLoop1:
	for _, s := range localServices {
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
		for _, s := range localServices {
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

		// Try to delete instances from QAN associated with queries service on KV.
		names, _, err := a.consulAPI.KV().Keys(prefix, "", nil)
		if err == nil {
			for _, name := range names {
				for _, serviceName := range []string{"mysql", "mongodb"} {
					if strings.HasSuffix(name, fmt.Sprintf("/qan_%s_uuid", serviceName)) {
						data, _, err := a.consulAPI.KV().Get(name, nil)
						if err == nil && data != nil {
							a.deleteInstance(string(data.Value))
						}
						break
					}
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
func (a *Admin) Uninstall() uint16 {
	var count uint16
	if FileExists(ConfigFile) {
		err := a.LoadConfig()
		if err == nil {
			a.apiTimeout = 5 * time.Second
			if err := a.SetAPI(); err == nil {
				// Try remove all services normally ignoring the errors.
				count, _ = a.RemoveAllMonitoring(true)
			}
		}
	}

	// Find any local PMM services and try to uninstall ignoring the errors.
	localServices := GetLocalServices()

	for _, service := range localServices {
		if err := uninstallService(service); err == nil {
			count++
		}
	}

	return count
}

// GetLocalServices finds any local PMM services
func GetLocalServices() (services []string) {
	dir, extension := GetServiceDirAndExtension()

	filesFound, _ := filepath.Glob(fmt.Sprintf("%s/pmm-*%s", dir, extension))
	rService, _ := regexp.Compile(fmt.Sprintf("%s/(pmm-.+)%s", dir, extension))
	for _, f := range filesFound {
		if data := rService.FindStringSubmatch(f); data != nil {
			services = append(services, data[1])
		}
	}

	return services
}

// GetServiceDirAndExtension returns dir and extension used to create system service
func GetServiceDirAndExtension() (dir, extension string) {
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
	case "darwin-launchd":
		dir = "/Library/LaunchDaemons"
		extension = ".plist"
	}

	return RootDir + dir, extension
}

// ShowPasswords display passwords from config file.
func (a *Admin) ShowPasswords() {
	fmt.Println("HTTP basic authentication")
	fmt.Printf("%-8s | %s\n", "User", a.Config.ServerUser)
	fmt.Printf("%-8s | %s\n\n", "Password", a.Config.ServerPassword)

	fmt.Println("MySQL new user creation")
	fmt.Printf("%-8s | %s\n", "Password", a.Config.MySQLPassword)
	fmt.Println()
}

// FileExists check if file exists.
func FileExists(file string) bool {
	if _, err := os.Stat(file); os.IsNotExist(err) {
		return false
	}
	return true
}

// CheckBinaries check if all PMM Client binaries are at their paths
func CheckBinaries() string {
	paths := []string{
		fmt.Sprintf("%s/node_exporter", PMMBaseDir),
		fmt.Sprintf("%s/mysqld_exporter", PMMBaseDir),
		fmt.Sprintf("%s/mongodb_exporter", PMMBaseDir),
		fmt.Sprintf("%s/proxysql_exporter", PMMBaseDir),
		fmt.Sprintf("%s/postgres_exporter", PMMBaseDir),
		fmt.Sprintf("%s/bin/percona-qan-agent", AgentBaseDir),
		fmt.Sprintf("%s/bin/percona-qan-agent-installer", AgentBaseDir),
	}
	for _, p := range paths {
		if !FileExists(p) {
			return p
		}
	}
	return ""
}

// Output colored text.
func colorStatus(msgOK string, msgNotOK string, ok bool) string {
	c := color.New(color.FgRed, color.Bold).SprintFunc()
	if ok {
		c = color.New(color.FgGreen, color.Bold).SprintFunc()
		return c(msgOK)
	}

	return c(msgNotOK)
}

// generateSSLCertificate generate SSL certificate and key and write them into the files.
func generateSSLCertificate(host, certFile, keyFile string) error {
	// Generate key.
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return fmt.Errorf("failed to generate private key: %s", err)
	}

	// Generate cert.
	notBefore, _ := time.Parse("Jan 2 15:04:05 2006", "Nov 25 15:00:00 2016")
	notAfter, _ := time.Parse("Jan 2 15:04:05 2006", "Nov 25 15:00:00 2026")
	serialNumber, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	cert := x509.Certificate{
		Subject:               pkix.Name{Organization: []string{"PMM Client"}},
		SerialNumber:          serialNumber,
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	if ip := net.ParseIP(host); ip != nil {
		cert.IPAddresses = append(cert.IPAddresses, ip)
	} else {
		cert.DNSNames = append(cert.DNSNames, host)
	}

	certBytes, err := x509.CreateCertificate(rand.Reader, &cert, &cert, &privKey.PublicKey, privKey)
	if err != nil {
		return fmt.Errorf("failed to generate certificate: %s", err)
	}

	// Write files.
	out, err := os.OpenFile(certFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("failed to write %s: %s", certFile, err)
	}
	pem.Encode(out, &pem.Block{Type: "CERTIFICATE", Bytes: certBytes})
	out.Close()

	out, err = os.OpenFile(keyFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("failed to write %s: %s", keyFile, err)
	}
	pem.Encode(out, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privKey)})
	out.Close()

	return nil
}

var svcTypes = []string{
	"linux:metrics",
	"mysql:metrics",
	"mysql:queries",
	"mongodb:metrics",
	"mongodb:queries",
	"proxysql:metrics",
	"postgresql:metrics",
}

// isValidSvcType checks if given service type is allowed
func isValidSvcType(svcType string) error {
	for _, v := range svcTypes {
		if v == svcType {
			return nil
		}
	}

	return fmt.Errorf(`bad service type.

Service type takes the following values: %s.`, strings.Join(svcTypes, ", "))
}
