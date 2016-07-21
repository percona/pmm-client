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

import "fmt"

var VERSION = "1.0.2"

const (
	PMMBaseDir     = "/usr/local/percona/pmm-client"
	agentBaseDir   = "/usr/local/percona/qan-agent"
	qanAPIBasePath = "qan-api"
	emojiUnhappy   = "😡"
	emojiHappy     = "🙂"
	noMonitoring   = "No monitoring registered for this node under identifier"
)

var (
	ConfigFile      = fmt.Sprintf("%s/pmm.yml", PMMBaseDir)
	agentConfigFile = fmt.Sprintf("%s/config/agent.conf", agentBaseDir)

	errDuplicate = fmt.Errorf("you have already the instance with this name under monitoring.")
	errNoService = fmt.Errorf("no service found.")
)

const nodeExporterArgs = "-collectors.enabled=diskstats,filesystem,loadavg,meminfo,netdev,netstat,stat,time,uname,vmstat"

// Among all, engine_tokudb_status and info_schema.innodb_tablespaces are false everywhere.
var mysqldExporterArgs = map[string][]string{
	"mysql-hr": {
		"-collect.auto_increment.columns=false",
		"-collect.binlog_size=false",
		"-collect.engine_tokudb_status=false",
		"-collect.global_status=true",
		"-collect.global_variables=false",
		"-collect.info_schema.innodb_metrics=true",
		"-collect.info_schema.innodb_tablespaces=false",
		"-collect.info_schema.processlist=false",
		"-collect.info_schema.query_response_time=false",
		"-collect.info_schema.tables=false",
		"-collect.info_schema.tablestats=false",
		"-collect.info_schema.userstats=false",
		"-collect.perf_schema.eventsstatements=false",
		"-collect.perf_schema.eventswaits=false",
		"-collect.perf_schema.file_events=false",
		"-collect.perf_schema.indexiowaits=false",
		"-collect.perf_schema.tableiowaits=false",
		"-collect.perf_schema.tablelocks=false",
		"-collect.slave_status=false",
	},
	"mysql-mr": {
		"-collect.auto_increment.columns=false",
		"-collect.binlog_size=false",
		"-collect.engine_tokudb_status=false",
		"-collect.global_status=false",
		"-collect.global_variables=false",
		"-collect.info_schema.innodb_metrics=false",
		"-collect.info_schema.innodb_tablespaces=false",
		"-collect.info_schema.processlist=true",
		"-collect.info_schema.query_response_time=true",
		"-collect.info_schema.tables=false",
		"-collect.info_schema.tablestats=false",
		"-collect.info_schema.userstats=false",
		"-collect.perf_schema.eventsstatements=false",
		"-collect.perf_schema.eventswaits=true",
		"-collect.perf_schema.file_events=true",
		"-collect.perf_schema.indexiowaits=false",
		"-collect.perf_schema.tableiowaits=false",
		"-collect.perf_schema.tablelocks=true",
		"-collect.slave_status=true",
	},
	"mysql-lr": {
		"-collect.auto_increment.columns=true",
		"-collect.binlog_size=true",
		"-collect.engine_tokudb_status=false",
		"-collect.global_status=false",
		"-collect.global_variables=true",
		"-collect.info_schema.innodb_metrics=false",
		"-collect.info_schema.innodb_tablespaces=false",
		"-collect.info_schema.processlist=false",
		"-collect.info_schema.query_response_time=false",
		"-collect.info_schema.tables=true",
		"-collect.info_schema.tablestats=true",
		"-collect.info_schema.userstats=true",
		"-collect.perf_schema.eventsstatements=true",
		"-collect.perf_schema.eventswaits=false",
		"-collect.perf_schema.file_events=false",
		"-collect.perf_schema.indexiowaits=true",
		"-collect.perf_schema.tableiowaits=true",
		"-collect.perf_schema.tablelocks=false",
		"-collect.slave_status=false",
	},
}
