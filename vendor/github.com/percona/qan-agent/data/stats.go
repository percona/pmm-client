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

package data

import (
	"container/list"
	"fmt"
	"github.com/percona/qan-agent/pct"
	"time"
)

var DebugStats = false

type SentInfo struct {
	Begin    time.Time
	End      time.Time
	SendTime float64
	Files    uint
	Bytes    uint64
	Errs     uint
	ApiErrs  uint
	Timeouts uint
	BadFiles uint
}

type SentReport struct {
	bytes    uint64
	sendTime float64
	// --
	Begin       time.Time
	End         time.Time
	Bytes       string // humanized bytes, e.g. 443.59 kB
	Duration    string // End - Begin, humanized
	Utilization string // bytes / (End - Begin), Mbps
	Throughput  string // bytes / sendTime, Mbps
	Files       uint
	Errs        uint
	ApiErrs     uint
	Timeouts    uint
	BadFiles    uint
}

var (
	BaseReportFormat  string = "%d files, %s, %s, %s net util, %s net speed"
	ErrorReportFormat        = "%d errors, %d API errors, %d timeouts, %d bad files"
)

type SenderStats struct {
	d time.Duration
	// --
	begin time.Time
	end   time.Time
	sent  *list.List
	full  bool // sent is at max size
}

func NewSenderStats(d time.Duration) *SenderStats {
	s := &SenderStats{
		d: d,
		// --
		sent: list.New(),
	}
	return s
}

func (s *SenderStats) Sent(info SentInfo) {
	if DebugStats {
		fmt.Printf("\n%+v\n", info)
		fmt.Printf("range: %s to %s (%s)\n", pct.TimeString(s.begin), pct.TimeString(s.end), s.end.Sub(s.begin))
		defer func() {
			fmt.Printf("range: %s to %s (%s)\n", pct.TimeString(s.begin), pct.TimeString(s.end), s.end.Sub(s.begin))
		}()
	}

	// Save this info and make it the latest.
	s.sent.PushFront(info)
	s.end = info.End.UTC()

	if s.full {
		old := []*list.Element{}
		for e := s.sent.Back(); e != nil && e.Prev() != nil; e = e.Prev() {
			// We can remove this info (e) if the next info (e.Prev) to s.end
			// maintains the full duration.
			info := e.Prev().Value.(SentInfo)
			d := s.end.Sub(info.Begin.UTC())
			if DebugStats {
				fmt.Printf("have %s at %s\n", d, info.Begin.UTC())
			}
			if d < s.d {
				// Can't remove this info because next info to s.end makes
				// duration too short.
				break
			}
			// Remove this info because next info to s.end is sufficiently
			// long duration.
			old = append(old, e)
		}
		for _, e := range old {
			if DebugStats {
				fmt.Printf("pop %+v\n", e.Value.(SentInfo))
			}
			s.sent.Remove(e)
		}
	} else if info.End.UTC().Sub(s.begin) >= s.d {
		if DebugStats {
			fmt.Println("full")
		}
		s.full = true
	}

	// Keep oldest up to date so we can determine when duration is full.
	s.begin = s.sent.Back().Value.(SentInfo).Begin.UTC()
}

func (s *SenderStats) Report() SentReport {
	r := SentReport{
		Begin: s.begin,
		End:   s.end,
	}
	for e := s.sent.Back(); e != nil; e = e.Prev() {
		info := e.Value.(SentInfo)

		r.bytes += info.Bytes
		r.sendTime += info.SendTime
		r.Files += info.Files
		r.Errs += info.Errs
		r.ApiErrs += info.ApiErrs
		r.Timeouts += info.Timeouts
		r.BadFiles += info.BadFiles
	}
	r.Bytes = pct.Bytes(r.bytes)
	r.Duration = pct.Duration(s.end.Sub(s.begin).Seconds())
	r.Utilization = pct.Mbps(r.bytes, s.end.Sub(s.begin).Seconds()) + " Mbps"
	r.Throughput = pct.Mbps(r.bytes, r.sendTime) + " Mbps"
	return r
}

func FormatSentReport(r SentReport) string {
	report := fmt.Sprintf(BaseReportFormat, r.Files, r.Bytes, r.Duration, r.Utilization, r.Throughput)
	if (r.Errs + r.BadFiles + r.ApiErrs + r.Timeouts) > 0 {
		report += ", " + fmt.Sprintf(ErrorReportFormat, r.Errs, r.ApiErrs, r.Timeouts, r.BadFiles)
	}
	return report
}

func (s *SenderStats) Dump() []SentInfo {
	sent := make([]SentInfo, s.sent.Len())
	i := 0
	for e := s.sent.Back(); e != nil; e = e.Prev() {
		sent[i] = e.Value.(SentInfo)
		i++
	}
	return sent
}
