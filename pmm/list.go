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
	"bytes"
	"fmt"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/docker/cli/templates"
	consul "github.com/hashicorp/consul/api"
	"github.com/percona/kardianos-service"
)

// Service status description.
type ServiceStatus struct {
	Type     string
	Name     string
	Port     string
	Running  bool
	DSN      string
	Options  string
	SSL      string
	Password string
}

// Sort rows of formatted table output (list, check-networks commands).
type sortOutput []ServiceStatus

func (s sortOutput) Len() int {
	return len(s)
}

func (s sortOutput) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func (s sortOutput) Less(i, j int) bool {
	if s[i].Port != s[j].Port {
		return s[i].Port < s[j].Port
	}
	if s[i].Name != s[j].Name {
		return s[i].Name < s[j].Name
	}
	if s[i].Type != s[j].Type {
		return s[i].Type < s[j].Type
	}
	return false
}

type List struct {
	Version string
	ServerInfo
	Platform string
	Err      string
	Services []ServiceStatus
}

// Table formats *List.Services as table and returns result as string
func (l *List) Table() string {
	// Print table.
	maxTypeLen := len("SERVICE TYPE")
	maxNameLen := len("NAME")
	maxDSNlen := len("DATA SOURCE")
	maxOptsLen := len("OPTIONS")
	for _, in := range l.Services {
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

	out := ""

	fmtPattern := "%%-%ds %%-%ds %%-%ds %%-%ds %%-%ds %%-%ds\n"
	linefmt := fmt.Sprintf(fmtPattern, maxTypeLen, maxNameLen, 11, maxStatusLen, maxDSNlen, maxOptsLen)

	out = out + fmt.Sprintf(linefmt, strings.Repeat("-", maxTypeLen), strings.Repeat("-", maxNameLen), strings.Repeat("-", 11),
		strings.Repeat("-", maxStatusLen), strings.Repeat("-", maxDSNlen), strings.Repeat("-", maxOptsLen))
	out = out + fmt.Sprintf(linefmt, "SERVICE TYPE", "NAME", "LOCAL PORT", "RUNNING", "DATA SOURCE", "OPTIONS")
	out = out + fmt.Sprintf(linefmt, strings.Repeat("-", maxTypeLen), strings.Repeat("-", maxNameLen), strings.Repeat("-", 11),
		strings.Repeat("-", maxStatusLen), strings.Repeat("-", maxDSNlen), strings.Repeat("-", maxOptsLen))

	maxStatusLen += 11
	linefmt = fmt.Sprintf(fmtPattern, maxTypeLen, maxNameLen, 11, maxStatusLen, maxDSNlen, maxOptsLen)
	for _, i := range l.Services {
		out = out + fmt.Sprintf(linefmt, i.Type, i.Name, i.Port, colorStatus("YES", "NO", i.Running), i.DSN, i.Options)
	}

	return out
}

// Format formats *List with provided format template and returns result as string.
func (l *List) Format(format string) string {
	b := &bytes.Buffer{}
	w := tabwriter.NewWriter(b, 8, 8, 8, ' ', 0)

	if format == "" {
		format = DefaultListTemplate
	}

	tmpl, err := templates.Parse(format)
	if err != nil {
		return err.Error()
	}
	tmpl, err = tmpl.Parse(ServerInfoTemplate)
	if err != nil {
		return err.Error()
	}
	if err := tmpl.Execute(w, l); err != nil {
		return err.Error()
	}

	w.Flush()

	return b.String()
}

const (
	DefaultListTemplate = `pmm-admin {{.Version}}

{{template "ServerInfo" .ServerInfo}}
{{printf "%-15s | %s" "Service Manager" .Platform}}
{{if .Err}}
{{.Err}}{{end}}
{{if .Services}}{{.Table}}{{end}}`
)

// List prints to stdout all services from Consul.
func (a *Admin) List() error {
	l := &List{
		Version:    Version,
		Platform:   service.Platform(),
		ServerInfo: a.serverInfo(),
	}

	defer func() {
		fmt.Print(l.Format(a.Format))
	}()

	node, _, err := a.consulAPI.Catalog().Node(a.Config.ClientName, nil)
	if err != nil || node == nil {
		l.Err = fmt.Sprintf("%s '%s'.", noMonitoring, a.Config.ClientName)
		return nil
	}

	if len(node.Services) == 0 {
		l.Err = fmt.Sprint("No services under monitoring.")
		return nil
	}

	// Get service data
	svcTable := a.getSVCTable(node)
	sort.Sort(sortOutput(svcTable))
	l.Services = svcTable

	return nil
}

func (a *Admin) getSVCTable(node *consul.CatalogNode) []ServiceStatus {
	// Parse all services except mysql:queries.
	var queryServices []*consul.AgentService
	var svcTable []ServiceStatus
	for _, svc := range node.Services {
		// When server hostname == client name, we have to exclude consul.
		if svc.Service == "consul" {
			continue
		}
		switch svc.Service {
		case "mysql:queries", "mongodb:queries":
			queryServices = append(queryServices, svc)
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

		row := ServiceStatus{
			Type:    svc.Service,
			Name:    name,
			Port:    fmt.Sprintf("%d", svc.Port),
			Running: status,
			DSN:     dsn,
			Options: strings.Join(opts, ", "),
		}
		svcTable = append(svcTable, row)
	}

	// Parse queries service.
	for _, queryService := range queryServices {
		status := getServiceStatus(fmt.Sprintf("pmm-%s-%d", strings.Replace(queryService.Service, ":", "-", 1), queryService.Port))

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
					case "qan_mysql_uuid", "qan_mongodb_uuid":
						f := fmt.Sprintf("%s/config/qan-%s.conf", AgentBaseDir, kvp.Value)
						querySource, _ := getQuerySource(f)
						if querySource != "" {
							opts = append(opts, fmt.Sprintf("query_source=%s", querySource))
						}
						queryExamples, _ := getQueryExamples(f)
						opts = append(opts, fmt.Sprintf("query_examples=%t", queryExamples))
					}
				}
			}
			row := ServiceStatus{
				Type:    queryService.Service,
				Name:    name,
				Port:    "-",
				Running: status,
				DSN:     dsn,
				Options: strings.Join(opts, ", "),
			}
			svcTable = append(svcTable, row)
		}
	}

	return svcTable
}
