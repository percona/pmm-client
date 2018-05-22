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
	"math"
	"net"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/beevik/ntp"
	"github.com/fatih/color"
	"golang.org/x/net/context"
)

// CheckNetwork check connectivity between client and server.
func (a *Admin) CheckNetwork() error {
	// Check QAN API health.
	qanStatus := false
	url := a.qanAPI.URL(a.serverURL, qanAPIBasePath, "ping")
	if resp, _, err := a.qanAPI.Get(url); err == nil {
		if resp.StatusCode == http.StatusOK && resp.Header.Get("X-Percona-Qan-Api-Version") != "" {
			qanStatus = true
		}
	}

	// Check Prometheus API by retrieving all "up" time series.
	promStatus := true
	promData, err := a.promQueryAPI.Query(context.Background(), "up", time.Now())
	if err != nil {
		promStatus = false
	}

	bindAddress := ""
	if a.Config.ClientAddress != a.Config.BindAddress {
		bindAddress = fmt.Sprintf("(%s)", a.Config.BindAddress)
	}

	fmt.Print("PMM Network Status\n\n")
	fmt.Printf("%-14s | %s\n", "Server Address", a.Config.ServerAddress)
	fmt.Printf("%-14s | %s %s\n\n", "Client Address", a.Config.ClientAddress, bindAddress)

	t := a.getNginxHeader("X-Server-Time")
	if t != "" {
		timeFormat := "2006-01-02 15:04:05 -0700 MST"

		// Real time (ntp server time)
		ntpHost := "0.pool.ntp.org"
		var ntpResponse *ntp.Response
		var ntpTimeErr error
		// ntp.Time() has default timeout of 5s, which should be enough,
		// but still ntp.Time() fails to get time too often.
		// Let's try smaller timeouts (1s) but try several times (5 times * 1 second = default 5s).
		for i := 0; i <= 5; i++ {
			ntpResponse, ntpTimeErr = ntp.QueryWithOptions(ntpHost, ntp.QueryOptions{
				Timeout: 1 * time.Second,
			})
			if ntpTimeErr == nil {
				break
			}
			time.Sleep(1 * time.Second)
		}
		ntpTimeText := ""
		if ntpTimeErr != nil {
			ntpTimeText = fmt.Sprintf("unable to get ntp time: %s", err)
		} else {
			ntpTimeText = ntpResponse.Time.Format(timeFormat)
		}

		// Server time
		color.New(color.Bold).Println("* System Time")
		var serverTime time.Time
		if s, err := strconv.ParseInt(t, 10, 64); err == nil {
			serverTime = time.Unix(s, 0)
		} else {
			serverTime, _ = time.Parse("Monday, 02-Jan-2006 15:04:05 MST", t)
		}

		// Client Time
		clientTime := time.Now()

		// Print times
		fmt.Printf("%-35s | %s\n", fmt.Sprintf("NTP Server (%s)", ntpHost), ntpTimeText)
		fmt.Printf("%-35s | %s\n", "PMM Server", serverTime.Format(timeFormat))
		fmt.Printf("%-35s | %s\n", "PMM Client", clientTime.Format(timeFormat))

		allowedDriftTime := float64(60) // seconds
		if ntpTimeErr == nil {
			// Calculate time drift between NTP Server and PMM Server
			drift := math.Abs(float64(serverTime.Unix()) - float64(ntpResponse.Time.Unix()))
			fmt.Printf("%-35s | %s\n", "PMM Server Time Drift", colorStatus("OK", fmt.Sprintf("%.0fs", drift), drift <= allowedDriftTime))
			if drift > allowedDriftTime {
				fmt.Print("Time is out of sync. Please make sure the server time is correct to see the metrics.\n")
			}
			// Calculate time drift between NTP Server and PMM Client
			drift = math.Abs(float64(clientTime.Unix()) - float64(ntpResponse.Time.Unix()))
			fmt.Printf("%-35s | %s\n", "PMM Client Time Drift", colorStatus("OK", fmt.Sprintf("%.0fs", drift), drift <= allowedDriftTime))
			if drift > allowedDriftTime {
				fmt.Print("Time is out of sync. Please make sure the client time is correct to see the metrics.\n")
			}
		}

		// Calculate time drift between server and client
		drift := math.Abs(float64(serverTime.Unix()) - float64(clientTime.Unix()))
		fmt.Printf("%-35s | %s\n", "PMM Client to PMM Server Time Drift", colorStatus("OK", fmt.Sprintf("%.0fs", drift), drift <= allowedDriftTime))
		if drift > allowedDriftTime {
			fmt.Print("Time is out of sync. Please make sure the server time is correct to see the metrics.\n")
		}
	}

	fmt.Println()
	color.New(color.Bold).Println("* Connection: Client --> Server")
	fmt.Printf("%-20s %-13s\n", strings.Repeat("-", 20), strings.Repeat("-", 7))
	fmt.Printf("%-20s %-13s\n", "SERVER SERVICE", "STATUS")
	fmt.Printf("%-20s %-13s\n", strings.Repeat("-", 20), strings.Repeat("-", 7))
	// Consul is always alive if we are at this point.
	fmt.Printf("%-20s %-13s\n", "Consul API", colorStatus("OK", "", true))
	fmt.Printf("%-20s %-13s\n", "Prometheus API", colorStatus("OK", "DOWN", promStatus))
	fmt.Printf("%-20s %-13s\n\n", "Query Analytics API", colorStatus("OK", "DOWN", qanStatus))

	a.testNetwork()
	fmt.Println()

	node, _, err := a.consulAPI.Catalog().Node(a.Config.ClientName, nil)
	if err != nil || node == nil {
		fmt.Printf("%s '%s'.\n\n", noMonitoring, a.Config.ClientName)
		return nil
	}

	if !promStatus {
		fmt.Print("Prometheus is down. Please check if PMM server container runs properly.\n\n")
		return nil
	}

	fmt.Println()
	color.New(color.Bold).Println("* Connection: Client <-- Server")
	if len(node.Services) == 0 {
		fmt.Print("No metric endpoints registered.\n\n")
		return nil
	}

	// Check Prometheus endpoint status.
	svcTable := []ServiceStatus{}
	errStatus := false
	for _, svc := range node.Services {
		if !strings.HasSuffix(svc.Service, ":metrics") {
			continue
		}

		name := "-"
		for _, tag := range svc.Tags {
			if strings.HasPrefix(tag, "alias_") {
				name = tag[6:]
				continue
			}

		}

		running := checkPromTargetStatus(promData.String(), name, strings.Split(svc.Service, ":")[0])
		if !running {
			errStatus = true
		}

		// Check protection status.
		localStatus := getServiceStatus(fmt.Sprintf("pmm-%s-%d", strings.Replace(svc.Service, ":", "-", 1), svc.Port))
		sslVal := "-"
		protectedVal := "-"
		if localStatus {
			sslVal = colorStatus("YES", "NO", a.isSSLProtected(svc.Service, svc.Port))
			if a.Config.ServerUser != "" {
				protectedVal = colorStatus("YES", "NO", a.isPasswordProtected(svc.Service, svc.Port))
			}
		}

		row := ServiceStatus{
			Type:     svc.Service,
			Name:     name,
			Port:     fmt.Sprintf("%d", svc.Port),
			Running:  running,
			SSL:      sslVal,
			Password: protectedVal,
		}
		svcTable = append(svcTable, row)
	}

	maxTypeLen := len("SERVICE TYPE")
	maxNameLen := len("NAME")
	for _, in := range svcTable {
		if len(in.Type) > maxTypeLen {
			maxTypeLen = len(in.Type)
		}
		if len(in.Name) > maxNameLen {
			maxNameLen = len(in.Name)
		}
	}
	maxTypeLen++
	maxNameLen++
	maxAddrLen := len(a.Config.ClientAddress) + 7
	maxStatusLen := 7
	maxProtectedLen := 9
	maxSSLLen := 10
	if a.Config.ClientAddress != a.Config.BindAddress {
		maxAddrLen = len(a.Config.ClientAddress) + len(a.Config.BindAddress) + 10
	}

	fmtPattern := "%%-%ds %%-%ds %%-%ds %%-%ds %%-%ds %%-%ds\n"
	linefmt := fmt.Sprintf(fmtPattern, maxTypeLen, maxNameLen, maxAddrLen, maxStatusLen, maxSSLLen, maxProtectedLen)

	fmt.Printf(linefmt, strings.Repeat("-", maxTypeLen), strings.Repeat("-", maxNameLen), strings.Repeat("-", maxAddrLen),
		strings.Repeat("-", maxStatusLen), strings.Repeat("-", maxSSLLen), strings.Repeat("-", maxProtectedLen))
	fmt.Printf(linefmt, "SERVICE TYPE", "NAME", "REMOTE ENDPOINT", "STATUS", "HTTPS/TLS", "PASSWORD")
	fmt.Printf(linefmt, strings.Repeat("-", maxTypeLen), strings.Repeat("-", maxNameLen), strings.Repeat("-", maxAddrLen),
		strings.Repeat("-", maxStatusLen), strings.Repeat("-", maxSSLLen), strings.Repeat("-", maxProtectedLen))

	sort.Sort(sortOutput(svcTable))
	maxStatusLen += 11
	linefmt = fmt.Sprintf(fmtPattern, maxTypeLen, maxNameLen, maxAddrLen, maxStatusLen, maxSSLLen, maxProtectedLen)
	for _, i := range svcTable {
		if i.SSL != "-" {
			linefmt = fmt.Sprintf(fmtPattern, maxTypeLen, maxNameLen, maxAddrLen, maxStatusLen, maxSSLLen+11, maxProtectedLen)
		}
		if a.Config.ClientAddress != a.Config.BindAddress {
			fmt.Printf(linefmt, i.Type, i.Name, a.Config.ClientAddress+"-->"+a.Config.BindAddress+":"+i.Port,
				colorStatus("OK", "DOWN", i.Running), i.SSL, i.Password)
		} else {
			fmt.Printf(linefmt, i.Type, i.Name, a.Config.ClientAddress+":"+i.Port,
				colorStatus("OK", "DOWN", i.Running), i.SSL, i.Password)
		}

	}

	if errStatus {
		scheme := "http"
		if a.Config.ServerInsecureSSL || a.Config.ServerSSL {
			scheme = "https"
		}
		url := fmt.Sprintf("%s://%s/prometheus/targets", scheme, a.Config.ServerAddress)
		fmt.Printf(`
When an endpoint is down it may indicate that the corresponding service is stopped (run 'pmm-admin list' to verify).
If it's running, check out the logs /var/log/pmm-*.log

When all endpoints are down but 'pmm-admin list' shows they are up and no errors in the logs,
check the firewall settings whether this system allows incoming connections from server to address:port in question.

Also you can check the endpoint status by the URL: %s
			`, url)
		if a.Config.ClientAddress != a.Config.BindAddress {
			fmt.Println(`
IMPORTANT: client and bind addresses are not the same which means you need to configure NAT/port forwarding to map them.`)
		}
	}
	fmt.Println()
	return nil
}

// testNetwork measure round trip duration of server connection.
func (a *Admin) testNetwork() {
	insecureFlag := false
	if a.Config.ServerInsecureSSL {
		insecureFlag = true
	}

	conn := &networkTransport{
		dialer: &net.Dialer{
			Timeout:   a.apiTimeout,
			KeepAlive: a.apiTimeout,
		},
	}
	conn.rtp = &http.Transport{
		Proxy:           http.ProxyFromEnvironment,
		Dial:            conn.dial,
		TLSClientConfig: &tls.Config{InsecureSkipVerify: insecureFlag},
	}
	client := &http.Client{Transport: conn}

	resp, err := client.Get(a.serverURL)
	if err != nil {
		fmt.Println("Unable to measure the connection performance.")
		return
	}
	defer resp.Body.Close()

	fmt.Printf("%-19s | %v\n", "Connection duration", conn.connEnd.Sub(conn.connStart))
	fmt.Printf("%-19s | %v\n", "Request duration", conn.reqEnd.Sub(conn.reqStart)-conn.connEnd.Sub(conn.connStart))
	fmt.Printf("%-19s | %v\n", "Full round trip", conn.reqEnd.Sub(conn.reqStart))
}

type networkTransport struct {
	rtp       http.RoundTripper
	dialer    *net.Dialer
	connStart time.Time
	connEnd   time.Time
	reqStart  time.Time
	reqEnd    time.Time
}

func (conn *networkTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	conn.reqStart = time.Now()
	resp, err := conn.rtp.RoundTrip(r)
	conn.reqEnd = time.Now()
	return resp, err
}

func (conn *networkTransport) dial(network, addr string) (net.Conn, error) {
	conn.connStart = time.Now()
	cn, err := conn.dialer.Dial(network, addr)
	conn.connEnd = time.Now()
	return cn, err
}

// checkPromTargetStatus check Prometheus target state by metric labels.
func checkPromTargetStatus(data, alias, job string) bool {
	r, _ := regexp.Compile(fmt.Sprintf(`up{.*instance="%s".*job="%s".*} => 1`, alias, job))
	for _, row := range strings.Split(data, "\n") {
		if r.MatchString(row) {
			return true
		}
	}
	return false
}

// isPasswordProtected check if endpoint is password protected.
func (a *Admin) isPasswordProtected(svcType string, port int) bool {
	urlPath := "metrics"
	if svcType == "mysql:metrics" {
		urlPath = "metrics-hr"
	}
	scheme := "http"
	api := a.qanAPI
	if a.isSSLProtected(svcType, port) {
		scheme = "https"
		// Enforce InsecureSkipVerify true to bypass err and check http code.
		api = NewAPI(true, apiTimeout, a.Verbose)
	}
	url := api.URL(fmt.Sprintf("%s://%s:%d", scheme, a.Config.BindAddress, port), urlPath)
	if resp, _, err := api.Get(url); err == nil && resp.StatusCode == http.StatusUnauthorized {
		return true
	}

	return false
}

// isSSLProtected check if endpoint is https/tls protected.
func (a *Admin) isSSLProtected(svcType string, port int) bool {
	url := a.qanAPI.URL(fmt.Sprintf("http://%s:%d", a.Config.BindAddress, port))
	if _, _, err := a.qanAPI.Get(url); err != nil && strings.Contains(err.Error(), "malformed HTTP response") {
		return true
	}

	return false
}
