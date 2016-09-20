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
	"net"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"golang.org/x/net/context"
)

// CheckNetwork check connectivity between client and server.
func (a *Admin) CheckNetwork(noEmoji bool) error {
	// Check QAN API health.
	qanStatus := false
	url := a.qanapi.URL(a.serverUrl, qanAPIBasePath, "ping")
	if resp, _, err := a.qanapi.Get(url); err == nil {
		if resp.StatusCode == http.StatusOK && resp.Header.Get("X-Percona-Qan-Api-Version") != "" {
			qanStatus = true
		}
	}

	// Check Prometheus API by retriving all "up" time series.
	promStatus := true
	promData, err := a.promapi.Query(context.Background(), "up", time.Now())
	if err != nil {
		promStatus = false
	}

	fmt.Println("PMM Network Status\n")
	fmt.Printf("%-6s | %s\n", "Server", a.Config.ServerAddress)
	fmt.Printf("%-6s | %s\n\n", "Client", a.Config.ClientAddress)
	fmt.Println("* Client --> Server")
	fmt.Printf("%-15s %-13s\n", strings.Repeat("-", 15), strings.Repeat("-", 7))
	fmt.Printf("%-15s %-13s\n", "SERVER SERVICE", "STATUS")
	fmt.Printf("%-15s %-13s\n", strings.Repeat("-", 15), strings.Repeat("-", 7))
	// Consul is always alive if we are at this point.
	fmt.Printf("%-15s %-13s\n", "Consul API", emojiStatus(noEmoji, true))
	fmt.Printf("%-15s %-13s\n", "QAN API", emojiStatus(noEmoji, qanStatus))
	fmt.Printf("%-15s %-13s\n", "Prometheus API", emojiStatus(noEmoji, promStatus))
	fmt.Println()

	a.testNetwork()
	fmt.Println()

	node, _, err := a.consulapi.Catalog().Node(a.Config.ClientName, nil)
	if err != nil || node == nil {
		fmt.Printf("%s '%s'.\n\n", noMonitoring, a.Config.ClientName)
		return nil
	}

	if !promStatus {
		fmt.Println("Prometheus is down. Please check if PMM server container runs properly.\n")
		return nil
	}

	fmt.Println("* Client <-- Server")
	if len(node.Services) == 0 {
		fmt.Println("No metric endpoints registered.\n")
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
			Status: emojiStatus(noEmoji, status),
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
	linefmt := "%-" + fmt.Sprintf("%d", maxTypeLen) + "s %-" + fmt.Sprintf("%d", maxNameLen) + "s %-22s %-7s\n"
	fmt.Printf(linefmt, strings.Repeat("-", maxTypeLen), strings.Repeat("-", maxNameLen), strings.Repeat("-", 22),
		strings.Repeat("-", 7))
	fmt.Printf(linefmt, "SERVICE TYPE", "NAME", "REMOTE ENDPOINT", "STATUS")
	fmt.Printf(linefmt, strings.Repeat("-", maxTypeLen), strings.Repeat("-", maxNameLen), strings.Repeat("-", 22),
		strings.Repeat("-", 7))
	sort.Sort(sortOutput(svcTable))
	for _, i := range svcTable {
		fmt.Printf(linefmt, i.Type, i.Name, a.Config.ClientAddress+":"+i.Port, i.Status)
	}

	if errStatus {
		fmt.Println(`
When an endpoint is down it may indicate that the corresponding service is stopped (run 'pmm-admin list' to verify).
If it's running, check out the logs /var/log/pmm-*.log

When all endpoints are down but 'pmm-admin list' shows they are up and no errors in the logs,
check the firewall settings whether this system allows incoming connections from server to address:port in question.`)
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
			Timeout:   apiTimeout,
			KeepAlive: apiTimeout,
		},
	}
	conn.rtp = &http.Transport{
		Proxy:           http.ProxyFromEnvironment,
		Dial:            conn.dial,
		TLSClientConfig: &tls.Config{InsecureSkipVerify: insecureFlag},
	}
	client := &http.Client{Transport: conn}

	resp, err := client.Get(a.serverUrl)
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

// Map status to emoji or text.
func emojiStatus(noEmoji, status bool) string {
	switch true {
	case noEmoji && status:
		return "OK"
	case noEmoji && !status:
		return "DOWN"
	case !noEmoji && status:
		return emojiHappy
	case !noEmoji && !status:
		return emojiUnhappy
	}
	return "N/A"
}
