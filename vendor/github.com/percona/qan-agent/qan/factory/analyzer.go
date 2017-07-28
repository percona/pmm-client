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

package factory

import (
	"time"

	"github.com/percona/pmm/proto"
	pc "github.com/percona/pmm/proto/config"
	"github.com/percona/qan-agent/data"
	"github.com/percona/qan-agent/mysql"
	"github.com/percona/qan-agent/pct"
	"github.com/percona/qan-agent/qan"
	"github.com/percona/qan-agent/qan/perfschema"
	"github.com/percona/qan-agent/qan/slowlog"
	"github.com/percona/qan-agent/ticker"
)

type RealAnalyzerFactory struct {
	logChan                 chan proto.LogEntry
	iterFactory             qan.IntervalIterFactory
	slowlogWorkerFactory    slowlog.WorkerFactory
	perfschemaWorkerFactory perfschema.WorkerFactory
	spool                   data.Spooler
	clock                   ticker.Manager
}

func NewRealAnalyzerFactory(
	logChan chan proto.LogEntry,
	iterFactory qan.IntervalIterFactory,
	slowlogWorkerFactory slowlog.WorkerFactory,
	perfschemaWorkerFactory perfschema.WorkerFactory,
	spool data.Spooler,
	clock ticker.Manager,
) *RealAnalyzerFactory {
	f := &RealAnalyzerFactory{
		logChan:                 logChan,
		iterFactory:             iterFactory,
		slowlogWorkerFactory:    slowlogWorkerFactory,
		perfschemaWorkerFactory: perfschemaWorkerFactory,
		spool: spool,
		clock: clock,
	}
	return f
}

func (f *RealAnalyzerFactory) Make(
	config pc.QAN,
	name string,
	mysqlConn mysql.Connector,
	restartChan chan proto.Instance,
	tickChan chan time.Time,
) qan.Analyzer {
	var worker qan.Worker
	analyzerType := config.CollectFrom
	switch analyzerType {
	case "slowlog":
		worker = f.slowlogWorkerFactory.Make(name+"-worker", config, mysqlConn)
	case "perfschema":
		worker = f.perfschemaWorkerFactory.Make(name+"-worker", mysqlConn)
	default:
		panic("Invalid analyzerType: " + analyzerType)
	}
	return qan.NewRealAnalyzer(
		pct.NewLogger(f.logChan, name),
		config,
		f.iterFactory.Make(analyzerType, mysqlConn, tickChan),
		mysqlConn,
		restartChan,
		worker,
		f.clock,
		f.spool,
	)
}
