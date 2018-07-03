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
	"errors"
	"fmt"
	"time"
)

const (
	qanAPIBasePath = "qan-api"
	noMonitoring   = "No monitoring registered for this node identified as"
	apiTimeout     = 10 * time.Second
	NameRegex      = `^[-\w:\.]{2,60}$`
)

var (
	// you can use `-ldflags -X github.com/percona/pmm-client/pmm.Version=`
	// to set build version number
	Version = "1.12.0"

	// you can use `-ldflags -X github.com/percona/pmm-client/pmm.RootDir=`
	// to set root filesystem for pmm-admin
	RootDir = ""

	PMMBaseDir   = RootDir + "/usr/local/percona/pmm-client"
	AgentBaseDir = RootDir + "/usr/local/percona/qan-agent"

	ConfigFile  = fmt.Sprintf("%s/pmm.yml", PMMBaseDir)
	SSLCertFile = fmt.Sprintf("%s/server.crt", PMMBaseDir)
	SSLKeyFile  = fmt.Sprintf("%s/server.key", PMMBaseDir)

	ErrDuplicate  = errors.New("there is already one instance with this name under monitoring.")
	ErrNoService  = errors.New("no service found.")
	ErrOneLinux   = errors.New("there could be only one instance of linux metrics being monitored for this system.")
	errNoInstance = errors.New("no instance found on QAN API.")
)

const nodeExporterArgs = "-collectors.enabled=diskstats,filefd,filesystem,loadavg,meminfo,netdev,netstat,stat,time,uname,vmstat,meminfo_numa"

var mysqldExporterArgs = []string{
	"-collect.auto_increment.columns=true",
	"-collect.binlog_size=true",
	"-collect.global_status=true",
	"-collect.global_variables=true",
	"-collect.info_schema.innodb_metrics=true",
	"-collect.info_schema.innodb_cmp=true",
	"-collect.info_schema.innodb_cmpmem=true",
	"-collect.info_schema.processlist=true",
	"-collect.info_schema.query_response_time=true",
	"-collect.info_schema.tables=true",
	"-collect.info_schema.tablestats=true",
	"-collect.info_schema.userstats=true",
	"-collect.perf_schema.eventswaits=true",
	"-collect.perf_schema.file_events=true",
	"-collect.perf_schema.indexiowaits=true",
	"-collect.perf_schema.tableiowaits=true",
	"-collect.perf_schema.tablelocks=true",
	"-collect.slave_status=true",
	//"-collect.engine_innodb_status=true",
	//"-collect.engine_tokudb_status=true",
	//"-collect.info_schema.clientstats=true",
	//"-collect.info_schema.innodb_tablespaces=true",
	//"-collect.perf_schema.eventsstatements=true",
}

// mysqld_exporter args to disable optionally.
var mysqldExporterDisableArgs = map[string][]string{
	"tablestats": {
		"-collect.auto_increment.columns=",
		"-collect.info_schema.tables=",
		"-collect.info_schema.tablestats=",
		"-collect.perf_schema.indexiowaits=",
		"-collect.perf_schema.tableiowaits=",
		"-collect.perf_schema.tablelocks=",
	},
	"userstats":   {"-collect.info_schema.userstats="},
	"binlogstats": {"-collect.binlog_size="},
	"processlist": {"-collect.info_schema.processlist="},
}

type Errors []error

func (e Errors) Error() string {
	return join(e, ", ")
}

// join concatenates the elements of a to create a single string. The separator string
// sep is placed between elements in the resulting string.
func join(a []error, sep string) string {
	if len(a) == 0 {
		return ""
	}
	if len(a) == 1 {
		return a[0].Error()
	}
	nilErr := fmt.Sprintf("%v", error(nil))
	n := len(sep) * (len(a) - 1)
	for i := 0; i < len(a); i++ {
		if a[i] == nil {
			n += len(nilErr)
		} else {
			n += len(a[i].Error())
		}
	}

	b := make([]byte, n)
	bp := copy(b, a[0].Error())
	for _, s := range a[1:] {
		bp += copy(b[bp:], sep)
		if s == nil {
			bp += copy(b[bp:], nilErr)
		} else {
			bp += copy(b[bp:], s.Error())
		}
	}
	return string(b)
}
