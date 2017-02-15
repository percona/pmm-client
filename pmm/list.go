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
	"sort"
	"strings"

	consul "github.com/hashicorp/consul/api"
	"github.com/percona/kardianos-service"
)

// Service status description.
type instanceStatus struct {
	Type     string
	Name     string
	Port     string
	Status   bool
	DSN      string
	Options  string
	SSL      string
	Password string
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

// List get all services from Consul.
func (a *Admin) List() error {
	fmt.Printf("pmm-admin %s\n\n", Version)
	a.ServerInfo()
	fmt.Printf("%-15s | %s\n\n", "Service Manager", service.Platform())

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
		// When server hostname == client name, we have to exclude consul.
		if svc.Service == "consul" {
			continue
		}
		if svc.Service == "mysql:queries" {
			queryService = svc
			continue
		}

		status := getServiceStatus(fmt.Sprintf("pmm-%s-%d", strings.Replace(svc.Service, ":", "-", 1), svc.Port))

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
			if tag == "scheme_https" {
				continue
			}
			tag := strings.Replace(tag, "_", "=", 1)
			opts = append(opts, tag)
		}

		row := instanceStatus{
			Type:    svc.Service,
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
		status := getServiceStatus(fmt.Sprintf("pmm-mysql-queries-%d", queryService.Port))

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
						queryExamples, _ := getQueryExamples(f)
						opts = append(opts, fmt.Sprintf("query_examples=%s", queryExamples))
					}
				}
			}
			row := instanceStatus{
				Type:    queryService.Service,
				Name:    name,
				Port:    "-",
				Status:  status,
				DSN:     dsn,
				Options: strings.Join(opts, ", "),
			}
			svcTable = append(svcTable, row)
		}
	}

	// Print table.
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
	maxStatusLen := 8

	fmtPattern := "%%-%ds %%-%ds %%-%ds %%-%ds %%-%ds %%-%ds\n"
	linefmt := fmt.Sprintf(fmtPattern, maxTypeLen, maxNameLen, 11, maxStatusLen, maxDSNlen, maxOptsLen)

	fmt.Printf(linefmt, strings.Repeat("-", maxTypeLen), strings.Repeat("-", maxNameLen), strings.Repeat("-", 11),
		strings.Repeat("-", maxStatusLen), strings.Repeat("-", maxDSNlen), strings.Repeat("-", maxOptsLen))
	fmt.Printf(linefmt, "SERVICE TYPE", "NAME", "LOCAL PORT", "RUNNING", "DATA SOURCE", "OPTIONS")
	fmt.Printf(linefmt, strings.Repeat("-", maxTypeLen), strings.Repeat("-", maxNameLen), strings.Repeat("-", 11),
		strings.Repeat("-", maxStatusLen), strings.Repeat("-", maxDSNlen), strings.Repeat("-", maxOptsLen))

	sort.Sort(sortOutput(svcTable))
	maxStatusLen += 11
	linefmt = fmt.Sprintf(fmtPattern, maxTypeLen, maxNameLen, 11, maxStatusLen, maxDSNlen, maxOptsLen)
	for _, i := range svcTable {
		fmt.Printf(linefmt, i.Type, i.Name, i.Port, colorStatus("YES", "NO", i.Status), i.DSN, i.Options)
	}
	fmt.Println()

	return nil
}
