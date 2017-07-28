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
	"fmt"
	"github.com/percona/pmm/proto"
	"github.com/percona/qan-agent/pct"
	"time"
)

const (
	MAX_SEND_ERRORS    = 3
	CONNECT_ERROR_WAIT = 3
)

type Sender struct {
	logger *pct.Logger
	client pct.WebsocketClient
	// --
	spool      Spooler
	tickerChan <-chan time.Time
	timeout    uint
	blackhole  bool
	sync       *pct.SyncChan
	status     *pct.Status
	// --
	lastStats  *SenderStats
	dailyStats *SenderStats
}

func NewSender(logger *pct.Logger, client pct.WebsocketClient) *Sender {
	s := &Sender{
		logger:     logger,
		client:     client,
		sync:       pct.NewSyncChan(),
		status:     pct.NewStatus([]string{"data-sender", "data-sender-last", "data-sender-1d"}),
		lastStats:  NewSenderStats(0),
		dailyStats: NewSenderStats(24 * time.Hour),
	}
	return s
}

func (s *Sender) Start(spool Spooler, tickerChan <-chan time.Time, timeout uint, blackhole bool) error {
	s.spool = spool
	s.tickerChan = tickerChan
	s.timeout = timeout
	s.blackhole = blackhole
	go s.run()
	s.logger.Info("Started")
	return nil
}

func (s *Sender) Stop() error {
	s.sync.Stop()
	s.sync.Wait()
	s.spool = nil
	s.tickerChan = nil
	s.logger.Info("Stopped")
	return nil
}

func (s *Sender) Status() map[string]string {
	return s.status.Merge(s.client.Status())
}

/////////////////////////////////////////////////////////////////////////////
// Implementation
/////////////////////////////////////////////////////////////////////////////

func (s *Sender) run() {
	defer func() {
		if err := recover(); err != nil {
			s.logger.Error("Data sender crashed: ", err)
		}
		if s.sync.IsGraceful() {
			s.logger.Info("Stop")
			s.status.Update("data-sender", "Stopped")
		} else {
			s.logger.Error("Crash")
			s.status.Update("data-sender", "Crashed")
		}
		s.sync.Done()
	}()

	for {
		s.status.Update("data-sender", "Idle")
		select {
		case <-s.tickerChan:
			s.send()
		case <-s.sync.StopChan:
			s.sync.Graceful()
			return
		}
	}
}

func (s *Sender) send() {
	s.logger.Debug("send:call")
	defer s.logger.Debug("send:return")

	sent := SentInfo{}
	defer func() {
		sent.End = time.Now()

		s.status.Update("data-sender", "Disconnecting")
		s.client.DisconnectOnce()

		// Stats for this run.
		s.lastStats.Sent(sent)
		r := s.lastStats.Report()
		report := fmt.Sprintf("at %s: %s", pct.TimeString(r.Begin), FormatSentReport(r))
		s.status.Update("data-sender-last", report)
		s.logger.Info(report)

		// Stats for the last day.
		s.dailyStats.Sent(sent)
		r = s.dailyStats.Report()
		report = fmt.Sprintf("since %s: %s", pct.TimeString(r.Begin), FormatSentReport(r))
		s.status.Update("data-sender-1d", report)
	}()

	// Connect and send files until too many errors occur.
	startTime := time.Now()
	sent.Begin = startTime
	for sent.ApiErrs == 0 && sent.Errs < MAX_SEND_ERRORS && sent.Timeouts == 0 {

		// Check runtime, don't send forever.
		runTime := time.Now().Sub(startTime).Seconds()
		if uint(runTime) > s.timeout {
			sent.Timeouts++
			s.logger.Warn(fmt.Sprintf("Timeout sending data: %.2fs > %ds", runTime, s.timeout))
			return
		}

		// Connect to API, or retry.
		s.status.Update("data-sender", "Connecting")
		s.logger.Debug("send:connecting")
		if sent.Errs > 0 {
			time.Sleep(CONNECT_ERROR_WAIT * time.Second)
		}
		if err := s.client.ConnectOnce(10); err != nil {
			sent.Errs++
			s.logger.Warn("Cannot connect to API: ", err)
			continue // retry
		}
		s.logger.Debug("send:connected")

		// Send all files, or stop on error or timeout.
		if err := s.sendAllFiles(startTime, &sent); err != nil {
			sent.Errs++
			s.logger.Warn(err)
			s.client.DisconnectOnce()
			continue // error sending files, re-connect and try again
		}
		return // success or API error, either way, stop sending
	}
}

func (s *Sender) sendAllFiles(startTime time.Time, sent *SentInfo) error {
	defer s.spool.CancelFiles()
	for file := range s.spool.Files() {
		s.logger.Debug("send:" + file)

		// Check runtime, don't send forever.
		runTime := time.Now().Sub(startTime).Seconds()
		if uint(runTime) > s.timeout {
			sent.Timeouts++
			s.logger.Warn(fmt.Sprintf("Timeout sending data: %.2fs > %ds", runTime, s.timeout))
			return nil // warn about timeout error here, not in caller
		}

		s.status.Update("data-sender", "Reading "+file)
		data, err := s.spool.Read(file)
		if err != nil {
			return fmt.Errorf("spool.Read: %s", err)
		}

		if s.blackhole {
			s.status.Update("data-sender", "Removing "+file+" (blackhole)")
			s.spool.Remove(file)
			s.logger.Info("Removed " + file + " (blackhole)")
			continue // next file
		}

		if len(data) == 0 {
			s.spool.Remove(file)
			s.logger.Warn("Removed " + file + " because it's empty")
			continue // next file
		}

		// todo: number/time/rate limit so we dont DDoS API
		s.status.Update("data-sender", "Sending "+file)
		t0 := time.Now()
		if err := s.client.SendBytes(data, s.timeout); err != nil {
			return fmt.Errorf("Sending %s: %s", file, err)
		}
		sent.SendTime += time.Now().Sub(t0).Seconds()
		sent.Bytes += uint64(len(data))

		s.status.Update("data-sender", "Waiting for API to ack "+file)
		resp := &proto.Response{}
		if err := s.client.Recv(resp, 5); err != nil {
			return fmt.Errorf("Waiting for API to ack %s: %s", file, err)
		}
		s.logger.Debug(fmt.Sprintf("send:resp:%+v", resp.Code))

		switch {
		case resp.Code >= 500:
			// API had problem, try sending files again later.
			sent.ApiErrs++
			return nil // don't warn about API errors
		case resp.Code >= 400:
			// File is bad, remove it.
			s.status.Update("data-sender", "Removing "+file)
			s.spool.Remove(file)
			s.logger.Warn(fmt.Sprintf("Removed %s because API returned %d: %s", file, resp.Code, resp.Error))
			sent.Files++
			sent.BadFiles++
		case resp.Code >= 300:
			// This shouldn't happen.
			return fmt.Errorf("Recieved unhandled response code from API: %d: %s", resp.Code, resp.Error)
		case resp.Code >= 200:
			s.status.Update("data-sender", "Removing "+file)
			s.spool.Remove(file)
			sent.Files++
			if resp.Code == 299 {
				s.logger.Warn("Not all data sent because API is throttling. Check the agent status to see the data spool size.")
				return nil
			}
		default:
			// This shouldn't happen.
			return fmt.Errorf("Recieved unknown response code from API: %d: %s", resp.Code, resp.Error)
		}
	}
	return nil // success
}
