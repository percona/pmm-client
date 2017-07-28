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

package qan_test

import (
	"encoding/json"
	"io/ioutil"
	"time"

	"github.com/percona/pmm/proto"
	pc "github.com/percona/pmm/proto/config"
	"github.com/percona/qan-agent/pct"
	"github.com/percona/qan-agent/qan"
	"github.com/percona/qan-agent/qan/slowlog"
	"github.com/percona/qan-agent/test/mock"
	. "gopkg.in/check.v1"
)

type ReportTestSuite struct{}

var _ = Suite(&ReportTestSuite{})

func (s *ReportTestSuite) TestResult001(t *C) {
	data, err := ioutil.ReadFile(outputDir + "/result001.json")
	t.Assert(err, IsNil)

	result := &qan.Result{}
	err = json.Unmarshal(data, result)
	t.Assert(err, IsNil)

	start := time.Now().Add(-1 * time.Second)
	stop := time.Now()

	interval := &qan.Interval{
		Filename:    "slow.log",
		StartTime:   start,
		StopTime:    stop,
		StartOffset: 0,
		EndOffset:   1000,
	}
	config := pc.QAN{
		UUID:        "1",
		ReportLimit: 10,
	}
	report := qan.MakeReport(config, interval, result)

	// 1st: 2.9
	t.Check(report.Class[0].Id, Equals, "3000000000000003")
	t.Check(report.Class[0].Metrics.TimeMetrics["Query_time"].Sum, Equals, float64(2.9))
	// 2nd: 2
	t.Check(report.Class[1].Id, Equals, "2000000000000002")
	t.Check(report.Class[1].Metrics.TimeMetrics["Query_time"].Sum, Equals, float64(2))
	// ...
	// 5th: 0.101001
	t.Check(report.Class[4].Id, Equals, "5000000000000005")
	t.Check(report.Class[4].Metrics.TimeMetrics["Query_time"].Sum, Equals, float64(0.101001))

	// Limit=2 results in top 2 queries and the rest in 1 LRQ "query".
	config.ReportLimit = 2
	report = qan.MakeReport(config, interval, result)
	t.Check(len(report.Class), Equals, 3)

	t.Check(report.Class[0].Id, Equals, "3000000000000003")
	t.Check(report.Class[0].Metrics.TimeMetrics["Query_time"].Sum, Equals, float64(2.9))

	t.Check(report.Class[1].Id, Equals, "2000000000000002")
	t.Check(report.Class[1].Metrics.TimeMetrics["Query_time"].Sum, Equals, float64(2))

	t.Check(report.Class[2].Id, Equals, "lrq")
	t.Check(int(report.Class[2].TotalQueries), Equals, 10)
	t.Check(report.Class[2].Metrics.TimeMetrics["Query_time"].Sum, Equals, float64(1+1+0.101001))
	t.Check(report.Class[2].Metrics.TimeMetrics["Query_time"].Min, Equals, float64(0.000100))
	t.Check(report.Class[2].Metrics.TimeMetrics["Query_time"].Max, Equals, float64(1.12))
	t.Check(report.Class[2].Metrics.TimeMetrics["Query_time"].Avg, Equals, float64((1+1+0.101001)/10))
}

func (s *ReportTestSuite) TestResult014(t *C) {
	config := pc.QAN{
		UUID:           "1",
		CollectFrom:    "slowlog",
		Interval:       60,
		WorkerRunTime:  60,
		ReportLimit:    500,
		MaxSlowLogSize: 1024 * 1024 * 1000,
	}
	logChan := make(chan proto.LogEntry, 1000)
	w := slowlog.NewWorker(pct.NewLogger(logChan, "w"), config, mock.NewNullMySQL())
	i := &qan.Interval{
		Filename:    inputDir + "slow014.log",
		StartOffset: 0,
		EndOffset:   127118681,
	}
	w.Setup(i)
	result, err := w.Run()
	t.Assert(err, IsNil)
	w.Cleanup()

	start := time.Now().Add(-1 * time.Second)
	stop := time.Now()
	interval := &qan.Interval{
		Filename:    "slow.log",
		StartTime:   start,
		StopTime:    stop,
		StartOffset: 0,
		EndOffset:   127118680,
	}
	report := qan.MakeReport(config, interval, result)

	t.Check(report.Global.TotalQueries, Equals, uint(4))
	t.Check(report.Global.UniqueQueries, Equals, uint(4))
	t.Assert(report.Class, HasLen, 4)
	// This query required improving the log parser to get the correct checksum ID:
	t.Check(report.Class[0].Id, Equals, "DB9EF18846547B8C")
}
