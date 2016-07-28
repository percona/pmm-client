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
	"net"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/prometheus/client_golang/api/prometheus"
	"golang.org/x/net/context"
)

// CheckNetwork check connectivity between client and server.
func (a *Admin) CheckNetwork(noEmoji bool) error {
	// Check Consul health.
	consulStatus := a.ServerAlive()

	// Check QAN API health.
	qanStatus := false
	url := a.qanapi.URL(a.Config.ServerAddress, qanAPIBasePath, "ping")
	if resp, _, err := a.qanapi.Get(url); err == nil {
		if resp.StatusCode == http.StatusOK && resp.Header.Get("X-Percona-Qan-Api-Version") != "" {
			qanStatus = true
		}
	}

	// Check Prometheus API.
	promStatus := false
	promData := a.getPromTargets()
	if promData != "" {
		promStatus = true
	}

	fmt.Println("PMM Network Status\n")
	fmt.Printf("%-6s | %s\n", "Server", a.Config.ServerAddress)
	fmt.Printf("%-6s | %s\n\n", "Client", a.Config.ClientAddress)
	fmt.Println("* Client > Server")
	fmt.Printf("%-15s %-13s\n", strings.Repeat("-", 15), strings.Repeat("-", 13))
	fmt.Printf("%-15s %-13s\n", "SERVICE", "CONNECTIVITY")
	fmt.Printf("%-15s %-13s\n", strings.Repeat("-", 15), strings.Repeat("-", 13))
	fmt.Printf("%-15s %-13s\n", "Consul API", emojiStatus(noEmoji, consulStatus))
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

	fmt.Println("* Server > Client")
	if len(node.Services) == 0 {
		fmt.Println("No Prometheus endpoints found.\n")
		return nil
	}

	// Check Prometheus endpoint status.
	svcTable := []instanceStatus{}
	errStatus := false
	for _, svc := range node.Services {
		metricType := svc.Service
		if metricType == "mysql-hr" || metricType == "mysql-mr" || metricType == "mysql-lr" {
			metricType = "mysql"
		} else if metricType == "queries" {
			continue
		}

		name := "-"
		for _, tag := range svc.Tags {
			if strings.HasPrefix(tag, "alias_") {
				name = tag[6:]
				continue
			}

		}

		status := a.checkPromTargetStatus(promData, name, metricType, svc.Port)
		if !status {
			errStatus = true
		}
		row := instanceStatus{
			Type:   metricType,
			Name:   name,
			Port:   fmt.Sprintf("%d", svc.Port),
			Status: emojiStatus(noEmoji, status),
		}
		svcTable = append(svcTable, row)
	}

	maxNameLen := 5
	for _, in := range svcTable {
		if len(in.Name) > maxNameLen {
			maxNameLen = len(in.Name)
		}
	}
	maxNameLen++
	linefmt := "%-8s %-" + fmt.Sprintf("%d", maxNameLen) + "s %-22s %-13s\n"
	fmt.Printf(linefmt, strings.Repeat("-", 8), strings.Repeat("-", maxNameLen), strings.Repeat("-", 22),
		strings.Repeat("-", 13))
	fmt.Printf(linefmt, "METRIC", "NAME", "PROMETHEUS ENDPOINT", "REMOTE STATE")
	fmt.Printf(linefmt, strings.Repeat("-", 8), strings.Repeat("-", maxNameLen), strings.Repeat("-", 22),
		strings.Repeat("-", 13))
	sort.Sort(sortOutput(svcTable))
	for _, i := range svcTable {
		fmt.Printf(linefmt, i.Type, i.Name, a.Config.ClientAddress+":"+i.Port, i.Status)
	}

	if errStatus {
		fmt.Println(`
For endpoints in problem state, please check if the corresponding service is running ("pmm-admin list").
If all endpoints are down here and "pmm-admin list" shows all services are up,
please check the firewall settings whether this system allows incoming connections by address:port in question.`)
	}
	fmt.Println()
	return nil
}

// getPromTargets get Prometheus targets and states.
func (a *Admin) getPromTargets() string {
	config := prometheus.Config{Address: fmt.Sprintf("http://%s/prometheus", a.Config.ServerAddress)}
	client, err := prometheus.New(config)
	if err != nil {
		return ""
	}

	// Retrieve all "up" time series.
	if res, err := prometheus.NewQueryAPI(client).Query(context.Background(), "up", time.Now()); err == nil {
		return res.String()
	}
	return ""
}

// checkPromTargetStatus check Prometheus target state by metric labels.
func (a *Admin) checkPromTargetStatus(data, alias, job string, port int) bool {
	query := fmt.Sprintf(`up{alias="%s", instance="%s:%d", job="%s"}`, alias, a.Config.ClientAddress, port, job)
	for _, row := range strings.Split(data, "\n") {
		vals := strings.Split(row, " => ")
		if vals[0] != query || len(vals) != 2 {
			continue
		}
		if string(vals[1][0]) == "1" {
			return true
		} else {
			return false
		}
	}
	return false
}

// testNetwork measure round trip duration of server connection.
func (a *Admin) testNetwork() {
	conn := &networkTransport{
		dialer: &net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		},
	}
	conn.rtp = &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		Dial:  conn.dial,
	}
	client := &http.Client{Transport: conn}

	resp, err := client.Get(fmt.Sprintf("http://%s", a.Config.ServerAddress))
	if err != nil {
		fmt.Println("Unable to measure a connection performance as it takes longer than 30 sec.")
		fmt.Println("Looks like there is a bad connectivity and it may affect your monitoring.")
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

// Map status to emoji or text.
func emojiStatus(noEmoji, status bool) string {
	switch true {
	case noEmoji && status:
		return "OK"
	case noEmoji && !status:
		return "PROBLEM"
	case !noEmoji && status:
		return emojiHappy
	case !noEmoji && !status:
		return emojiUnhappy
	}
	return "N/A"
}
