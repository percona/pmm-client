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
	"path"
	"time"

	"github.com/percona/pmm/proto"
	"github.com/percona/qan-agent/mysql"
	"github.com/percona/qan-agent/pct"
	"github.com/percona/qan-agent/qan"
	"github.com/percona/qan-agent/qan/perfschema"
	"github.com/percona/qan-agent/qan/slowlog"
)

type RealIntervalIterFactory struct {
	logChan chan proto.LogEntry
}

func NewRealIntervalIterFactory(logChan chan proto.LogEntry) *RealIntervalIterFactory {
	f := &RealIntervalIterFactory{
		logChan: logChan,
	}
	return f
}

func (f *RealIntervalIterFactory) Make(analyzerType string, mysqlConn mysql.Connector, tickChan chan time.Time) qan.IntervalIter {
	switch analyzerType {
	case "slowlog":
		// The interval iter gets the slow log file (@@global.slow_query_log_file)
		// every tick because it can change (not typical, but possible). If it changes,
		// the start offset is reset to 0 for the new file.
		getSlowLogFunc := func() (string, error) {
			if err := mysqlConn.Connect(); err != nil {
				return "", err
			}
			defer mysqlConn.Close()
			// Slow log file can be absolute or relative. If it's relative,
			// then prepend the datadir.
			dataDir := mysqlConn.GetGlobalVarString("datadir")
			filename := AbsDataFile(dataDir, mysqlConn.GetGlobalVarString("slow_query_log_file"))
			return filename, nil
		}
		return slowlog.NewIter(pct.NewLogger(f.logChan, "qan-interval"), getSlowLogFunc, tickChan)
	case "perfschema":
		return perfschema.NewIter(pct.NewLogger(f.logChan, "qan-interval"), tickChan)
	default:
		panic("Invalid analyzerType: " + analyzerType)
	}
}

func AbsDataFile(dataDir, fileName string) string {
	if !path.IsAbs(fileName) {
		fileName = path.Join(dataDir, fileName)
	}
	return fileName
}
