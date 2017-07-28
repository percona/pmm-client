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

package data_test

import (
	"fmt"
	. "github.com/go-test/test"
	"github.com/percona/qan-agent/data"
	. "gopkg.in/check.v1"
	"time"
)

type StatsTestSuite struct {
	now  time.Time
	send []data.SentInfo
}

var _ = Suite(&StatsTestSuite{})

func (s *StatsTestSuite) SetUpSuite(t *C) {
	s.now = time.Now()

	// +1s results in 0.999999s diff, so +1.1s to workaround.
	s.send = []data.SentInfo{
		data.SentInfo{
			Begin:    s.now.Add(1100 * time.Millisecond),
			End:      s.now.Add(1400 * time.Millisecond),
			SendTime: 0.2,
			Files:    1,
			Bytes:    11100,
		},
		data.SentInfo{
			Begin:    s.now.Add(2100 * time.Millisecond),
			End:      s.now.Add(2500 * time.Millisecond),
			SendTime: 0.3,
			Files:    1,
			Bytes:    22200,
		},
		data.SentInfo{
			Begin:    s.now.Add(3100 * time.Millisecond),
			End:      s.now.Add(3500 * time.Millisecond),
			SendTime: 0.3,
			Files:    1,
			Bytes:    33300,
		},
		data.SentInfo{
			Begin:    s.now.Add(4100 * time.Millisecond),
			End:      s.now.Add(4700 * time.Millisecond),
			SendTime: 0.5,
			Files:    1,
			Bytes:    44400,
		},
		data.SentInfo{
			Begin:    s.now.Add(5100 * time.Millisecond),
			End:      s.now.Add(5800 * time.Millisecond),
			SendTime: 0.6,
			Files:    3,
			Bytes:    5155505,
		},
		data.SentInfo{
			Begin:    s.now.Add(6100 * time.Millisecond),
			End:      s.now.Add(6900 * time.Millisecond),
			SendTime: 0.850,
			Files:    2,
			Bytes:    606061,
		},
	}
}

// --------------------------------------------------------------------------

func (s *StatsTestSuite) TestRoundRobinFull(t *C) {
	ss := data.NewSenderStats(time.Duration(3 * time.Second))
	t.Assert(ss, NotNil)

	d := ss.Dump()
	t.Check(d, HasLen, 0)

	for _, info := range s.send {
		ss.Sent(info)
	}

	d = ss.Dump()
	if len(d) != 4 {
		Dump(d)
		t.Errorf("len(d)=%d, expected 4", len(d))
	}
	if same, diff := IsDeeply(d[0], s.send[2]); !same {
		t.Error(diff)
	}
	if same, diff := IsDeeply(d[1], s.send[3]); !same {
		t.Error(diff)
	}
	if same, diff := IsDeeply(d[2], s.send[4]); !same {
		t.Error(diff)
	}
	if same, diff := IsDeeply(d[3], s.send[5]); !same {
		t.Error(diff)
	}

	got := ss.Report()
	expect := data.SentReport{
		Begin:       s.send[2].Begin,
		End:         s.send[5].End,
		Bytes:       "5.84 MB",
		Duration:    "3.8s",
		Utilization: "12.29 Mbps",
		Throughput:  "20.76 Mbps",
		Files:       7,
		Errs:        0,
		ApiErrs:     0,
		Timeouts:    0,
		BadFiles:    0,
	}
	if same, diff := IsDeeply(got, expect); !same {
		Dump(got)
		t.Error(diff)
	}

	t.Check(
		data.FormatSentReport(got),
		Equals,
		fmt.Sprintf(data.BaseReportFormat,
			expect.Files, expect.Bytes, expect.Duration, expect.Utilization, expect.Throughput),
	)
}

func (s *StatsTestSuite) TestRoundRobinPartial(t *C) {
	ss := data.NewSenderStats(time.Duration(3 * time.Second))
	t.Assert(ss, NotNil)

	d := ss.Dump()
	t.Check(d, HasLen, 0)

	for i := 0; i < 4; i++ {
		ss.Sent(s.send[i])
	}

	d = ss.Dump()
	if len(d) != 4 {
		Dump(d)
		t.Errorf("len(d)=%d, expected 4", len(d))
	}
	if same, diff := IsDeeply(d[0], s.send[0]); !same {
		t.Error(diff)
	}
	if same, diff := IsDeeply(d[1], s.send[1]); !same {
		t.Error(diff)
	}
	if same, diff := IsDeeply(d[2], s.send[2]); !same {
		t.Error(diff)
	}
	if same, diff := IsDeeply(d[3], s.send[3]); !same {
		t.Error(diff)
	}

	got := ss.Report()
	expect := data.SentReport{
		Begin:       s.send[0].Begin,
		End:         s.send[3].End,
		Bytes:       "111.00 kB",
		Duration:    "3.6s",
		Utilization: "0.25 Mbps",
		Throughput:  "0.68 Mbps",
		Files:       4,
		Errs:        0,
		ApiErrs:     0,
		Timeouts:    0,
		BadFiles:    0,
	}
	if same, diff := IsDeeply(got, expect); !same {
		Dump(got)
		t.Error(diff)
	}
}

func (s *StatsTestSuite) TestOnlyLast(t *C) {
	ss := data.NewSenderStats(time.Duration(0))
	t.Assert(ss, NotNil)

	d := ss.Dump()
	t.Check(d, HasLen, 0)

	for i := 0; i < 4; i++ {
		ss.Sent(s.send[i])
	}

	d = ss.Dump()
	if len(d) != 1 {
		Dump(d)
		t.Errorf("len(d)=%d, expected 1", len(d))
	}
	if same, diff := IsDeeply(d[0], s.send[3]); !same {
		t.Error(diff)
	}

	got := ss.Report()
	expect := data.SentReport{
		Begin:       s.send[3].Begin,
		End:         s.send[3].End,
		Bytes:       "44.40 kB",
		Duration:    "600ms",
		Utilization: "0.59 Mbps",
		Throughput:  "0.71 Mbps",
		Files:       1,
		Errs:        0,
		ApiErrs:     0,
		Timeouts:    0,
		BadFiles:    0,
	}
	if same, diff := IsDeeply(got, expect); !same {
		Dump(got)
		t.Error(diff)
	}
}

func (s *StatsTestSuite) TestErrors(t *C) {
	ss := data.NewSenderStats(time.Duration(10 * time.Second))
	t.Assert(ss, NotNil)

	d := ss.Dump()
	t.Check(d, HasLen, 0)

	// Copy data so we can add errors.
	send := make([]data.SentInfo, len(s.send))
	for i, info := range s.send {
		send[i] = info
	}
	send[0].Errs++
	send[1].ApiErrs++
	send[2].BadFiles++
	send[3].Timeouts++
	for _, info := range send {
		ss.Sent(info)
	}

	d = ss.Dump()
	if len(d) != len(send) {
		Dump(d)
		t.Errorf("len(d)=%d, expected %d", len(d), len(send))
	}

	got := ss.Report()
	expect := data.SentReport{
		Begin:       send[0].Begin,
		End:         send[5].End,
		Bytes:       "5.87 MB",
		Duration:    "5.8s",
		Utilization: "8.10 Mbps",
		Throughput:  "17.08 Mbps",
		Files:       9,
		Errs:        1,
		ApiErrs:     1,
		Timeouts:    1,
		BadFiles:    1,
	}
	if same, diff := IsDeeply(got, expect); !same {
		Dump(got)
		t.Error(diff)
	}

	t.Check(
		data.FormatSentReport(got),
		Equals,
		fmt.Sprintf(data.BaseReportFormat+", "+data.ErrorReportFormat,
			expect.Files, expect.Bytes, expect.Duration, expect.Utilization, expect.Throughput,
			expect.Errs, expect.ApiErrs, expect.Timeouts, expect.BadFiles),
	)
}

func (s *StatsTestSuite) TestDelayBeforeFull(t *C) {
	s.now = time.Now()

	send := []data.SentInfo{
		data.SentInfo{
			Begin:    s.now.Add(1100 * time.Millisecond),
			End:      s.now.Add(1400 * time.Millisecond),
			SendTime: 0.2,
			Files:    1,
			Bytes:    11100,
		},
		// 4s delay
		data.SentInfo{
			Begin:    s.now.Add(5100 * time.Millisecond),
			End:      s.now.Add(5500 * time.Millisecond),
			SendTime: 0.3,
			Files:    2,
			Bytes:    22200,
		},
		// resume normal
		data.SentInfo{
			Begin:    s.now.Add(6100 * time.Millisecond),
			End:      s.now.Add(6500 * time.Millisecond),
			SendTime: 0.3,
			Files:    3,
			Bytes:    33300,
		},
		data.SentInfo{
			Begin:    s.now.Add(7100 * time.Millisecond),
			End:      s.now.Add(7700 * time.Millisecond),
			SendTime: 0.5,
			Files:    4,
			Bytes:    44400,
		},
		data.SentInfo{
			Begin:    s.now.Add(8100 * time.Millisecond),
			End:      s.now.Add(8800 * time.Millisecond),
			SendTime: 0.6,
			Files:    5,
			Bytes:    5155505,
		},
		data.SentInfo{
			Begin:    s.now.Add(9100 * time.Millisecond),
			End:      s.now.Add(9900 * time.Millisecond),
			SendTime: 0.850,
			Files:    6,
			Bytes:    606061,
		},
	}
	ss := data.NewSenderStats(time.Duration(3 * time.Second))
	t.Assert(ss, NotNil)

	d := ss.Dump()
	t.Check(d, HasLen, 0)

	for _, info := range send {
		ss.Sent(info)
	}

	d = ss.Dump()
	if len(d) != 4 {
		Dump(d)
		t.Errorf("len(d)=%d, expected 4", len(d))
	}
	if same, diff := IsDeeply(d[0], send[2]); !same {
		t.Error(diff)
	}
	if same, diff := IsDeeply(d[1], send[3]); !same {
		t.Error(diff)
	}
	if same, diff := IsDeeply(d[2], send[4]); !same {
		t.Error(diff)
	}
	if same, diff := IsDeeply(d[3], send[5]); !same {
		t.Error(diff)
	}

	got := ss.Report()
	expect := data.SentReport{
		Begin:       send[2].Begin,
		End:         send[5].End,
		Bytes:       "5.84 MB",
		Duration:    "3.8s",
		Utilization: "12.29 Mbps",
		Throughput:  "20.76 Mbps",
		Files:       18,
		Errs:        0,
		ApiErrs:     0,
		Timeouts:    0,
		BadFiles:    0,
	}
	if same, diff := IsDeeply(got, expect); !same {
		Dump(got)
		t.Error(diff)
	}
}

func (s *StatsTestSuite) TestGaps(t *C) {
	s.now = time.Now()

	send := []data.SentInfo{
		data.SentInfo{
			Begin:    s.now.Add(1100 * time.Millisecond),
			End:      s.now.Add(1400 * time.Millisecond),
			SendTime: 0.2,
			Files:    1,
			Bytes:    11100,
		},
		data.SentInfo{
			Begin:    s.now.Add(2100 * time.Millisecond),
			End:      s.now.Add(2500 * time.Millisecond),
			SendTime: 0.3,
			Files:    2,
			Bytes:    22200,
		},
		data.SentInfo{
			Begin:    s.now.Add(3100 * time.Millisecond),
			End:      s.now.Add(3500 * time.Millisecond),
			SendTime: 0.3,
			Files:    3,
			Bytes:    33300,
		},
		// missing 3
		data.SentInfo{
			Begin:    s.now.Add(7100 * time.Millisecond),
			End:      s.now.Add(7700 * time.Millisecond),
			SendTime: 0.5,
			Files:    4,
			Bytes:    44400,
		},
		// missing 2
		data.SentInfo{
			Begin:    s.now.Add(10100 * time.Millisecond),
			End:      s.now.Add(10700 * time.Millisecond),
			SendTime: 0.6,
			Files:    5,
			Bytes:    5155505,
		},
		// missing 1
		data.SentInfo{
			Begin:    s.now.Add(12100 * time.Millisecond),
			End:      s.now.Add(12900 * time.Millisecond),
			SendTime: 0.850,
			Files:    6,
			Bytes:    606061,
		},
	}
	ss := data.NewSenderStats(time.Duration(3 * time.Second))
	t.Assert(ss, NotNil)

	d := ss.Dump()
	t.Check(d, HasLen, 0)

	for _, info := range send {
		ss.Sent(info)
	}

	d = ss.Dump()
	if len(d) != 3 {
		Dump(d)
		t.Errorf("len(d)=%d, expected 3", len(d))
	}

	got := ss.Report()
	expect := data.SentReport{
		Begin:       send[3].Begin,
		End:         send[5].End,
		Bytes:       "5.81 MB",
		Duration:    "5.8s",
		Utilization: "8.01 Mbps",
		Throughput:  "23.82 Mbps",
		Files:       15,
		Errs:        0,
		ApiErrs:     0,
		Timeouts:    0,
		BadFiles:    0,
	}
	if same, diff := IsDeeply(got, expect); !same {
		Dump(got)
		t.Error(diff)
	}
}

func (s *StatsTestSuite) TestEmptyReport(t *C) {
	ss := data.NewSenderStats(time.Duration(3 * time.Second))
	t.Assert(ss, NotNil)

	got := ss.Report()
	expect := data.SentReport{
		Begin:       time.Time{},
		End:         time.Time{},
		Bytes:       "0",
		Duration:    "0",
		Utilization: "0.00 Mbps",
		Throughput:  "0.00 Mbps",
		Files:       0,
		Errs:        0,
		ApiErrs:     0,
		Timeouts:    0,
		BadFiles:    0,
	}
	if same, diff := IsDeeply(got, expect); !same {
		Dump(got)
		t.Error(diff)
	}
}
