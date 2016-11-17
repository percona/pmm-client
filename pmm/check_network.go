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

	// Check Prometheus API by retriving all "up" time series.
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
		color.New(color.Bold).Println("* System Time")
		var serverTime time.Time
		if s, err := strconv.ParseInt(t, 10, 64); err == nil {
			serverTime = time.Unix(s, 0)
		} else {
			serverTime, _ = time.Parse("Monday, 02-Jan-2006 15:04:05 MST", t)
		}
		clientTime := time.Now()
		fmt.Printf("%-10s | %s\n", "Server", serverTime.Format("2006-01-02 15:04:05 -0700 MST"))
		fmt.Printf("%-10s | %s\n", "Client", clientTime.Format("2006-01-02 15:04:05 -0700 MST"))
		drift := math.Abs(float64(serverTime.Unix()) - float64(clientTime.Unix()))
		fmt.Printf("%-10s | %s\n\n", "Time Drift", colorStatus("OK", fmt.Sprintf("%.0fs", drift), drift <= 120))
		if drift > 120 {
			fmt.Print("Time is out of sync. Please make sure the server time is correct to see the metrics.\n\n")
		}
	}

	fmt.Println()
	color.New(color.Bold).Println("* Client --> Server")
	fmt.Printf("%-15s %-13s\n", strings.Repeat("-", 15), strings.Repeat("-", 7))
	fmt.Printf("%-15s %-13s\n", "SERVER SERVICE", "STATUS")
	fmt.Printf("%-15s %-13s\n", strings.Repeat("-", 15), strings.Repeat("-", 7))
	// Consul is always alive if we are at this point.
	fmt.Printf("%-15s %-13s\n", "Consul API", colorStatus("OK", "", true))
	fmt.Printf("%-15s %-13s\n", "QAN API", colorStatus("OK", "DOWN", qanStatus))
	fmt.Printf("%-15s %-13s\n\n", "Prometheus API", colorStatus("OK", "DOWN", promStatus))

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
	color.New(color.Bold).Println("* Client <-- Server")
	if len(node.Services) == 0 {
		fmt.Print("No metric endpoints registered.\n\n")
		return nil
	}

	// Check Prometheus endpoint status.
	svcTable := []instanceStatus{}
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

		status := checkPromTargetStatus(promData.String(), name, strings.Split(svc.Service, ":")[0])
		if !status {
			errStatus = true
		}
		row := instanceStatus{
			Type:   svc.Service,
			Name:   name,
			Port:   fmt.Sprintf("%d", svc.Port),
			Status: status,
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
	if a.Config.ClientAddress != a.Config.BindAddress {
		maxAddrLen = len(a.Config.ClientAddress) + len(a.Config.BindAddress) + 10
	}

	fmtPattern := "%%-%ds %%-%ds %%-%ds %%-%ds\n"
	linefmt := fmt.Sprintf(fmtPattern, maxTypeLen, maxNameLen, maxAddrLen, 7)

	fmt.Printf(linefmt, strings.Repeat("-", maxTypeLen), strings.Repeat("-", maxNameLen), strings.Repeat("-", maxAddrLen),
		strings.Repeat("-", 7))
	fmt.Printf(linefmt, "SERVICE TYPE", "NAME", "REMOTE ENDPOINT", "STATUS")
	fmt.Printf(linefmt, strings.Repeat("-", maxTypeLen), strings.Repeat("-", maxNameLen), strings.Repeat("-", maxAddrLen),
		strings.Repeat("-", 7))

	sort.Sort(sortOutput(svcTable))
	for _, i := range svcTable {
		if a.Config.ClientAddress != a.Config.BindAddress {
			fmt.Printf(linefmt, i.Type, i.Name, a.Config.ClientAddress+"-->"+a.Config.BindAddress+":"+i.Port,
				colorStatus("OK", "DOWN", i.Status))
		} else {
			fmt.Printf(linefmt, i.Type, i.Name, a.Config.ClientAddress+":"+i.Port,
				colorStatus("OK", "DOWN", i.Status))
		}

	}

	if errStatus {
		fmt.Println(`
When an endpoint is down it may indicate that the corresponding service is stopped (run 'pmm-admin list' to verify).
If it's running, check out the logs /var/log/pmm-*.log

When all endpoints are down but 'pmm-admin list' shows they are up and no errors in the logs,
check the firewall settings whether this system allows incoming connections from server to address:port in question.`)
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
