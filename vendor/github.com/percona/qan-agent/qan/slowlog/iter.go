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

package slowlog

import (
	"fmt"
	"os"
	"time"

	"github.com/percona/qan-agent/pct"
	"github.com/percona/qan-agent/qan"
)

type FilenameFunc func() (string, error)

type Iter struct {
	logger   *pct.Logger
	filename FilenameFunc
	tickChan chan time.Time
	// --
	intervalNo   int
	intervalChan chan *qan.Interval
	sync         *pct.SyncChan
}

func NewIter(logger *pct.Logger, filename FilenameFunc, tickChan chan time.Time) *Iter {
	iter := &Iter{
		logger:   logger,
		filename: filename,
		tickChan: tickChan,
		// --
		intervalChan: make(chan *qan.Interval, 1),
		sync:         pct.NewSyncChan(),
	}
	return iter
}

func (i *Iter) Start() {
	go i.run()
}

func (i *Iter) Stop() {
	i.sync.Stop()
	i.sync.Wait()
	return
}

func (i *Iter) IntervalChan() chan *qan.Interval {
	return i.intervalChan
}

func (i *Iter) TickChan() chan time.Time {
	return i.tickChan
}

// --------------------------------------------------------------------------

func (i *Iter) run() {
	defer func() {
		if err := recover(); err != nil {
			i.logger.Error("slowlog.Iter crashed: ", err)
		}
		i.sync.Done()
	}()

	var prevFileInfo os.FileInfo
	cur := &qan.Interval{}

	for {
		i.logger.Debug("run:idle")

		select {
		case now := <-i.tickChan:
			i.logger.Debug("run:tick")

			// Get the MySQL slow log file name at each interval because it can change.
			curFile, err := i.filename()
			if err != nil {
				i.logger.Warn(err)
				cur = new(qan.Interval)
				continue
			}

			// Get the current size of the MySQL slow log.
			i.logger.Debug("run:file size")
			curSize, err := pct.FileSize(curFile)
			if err != nil {
				i.logger.Warn(err)
				cur = new(qan.Interval)
				continue
			}
			i.logger.Debug(fmt.Sprintf("run:%s:%d", curFile, curSize))

			// File changed if prev file not same as current file.
			// @todo: Normally this only changes when QAN manager rotates slow log
			//        at interval.  If it changes for another reason (e.g. user
			//        renames slow log) then StartOffset=0 may not be ideal.
			curFileInfo, _ := os.Stat(curFile)
			fileChanged := !os.SameFile(prevFileInfo, curFileInfo)
			prevFileInfo = curFileInfo

			if !cur.StartTime.IsZero() { // StartTime is set
				i.logger.Debug("run:next")
				i.intervalNo++

				// End of current interval:
				cur.Filename = curFile
				if fileChanged {
					// Start from beginning of new file.
					i.logger.Info("File changed")
					cur.StartOffset = 0
				}
				cur.EndOffset = curSize
				cur.StopTime = now
				cur.Number = i.intervalNo

				// Send interval to manager which should be ready to receive it.
				select {
				case i.intervalChan <- cur:
				case <-time.After(1 * time.Second):
					i.logger.Warn(fmt.Sprintf("Lost interval: %+v", cur))
				}

				// Next interval:
				cur = &qan.Interval{
					StartTime:   now,
					StartOffset: curSize,
				}
			} else {
				// First interval, either due to first tick or because an error
				// occurred earlier so a new interval was started.
				i.logger.Debug("run:first")
				cur.StartOffset = curSize
				cur.StartTime = now
				prevFileInfo, _ = os.Stat(curFile)
			}
		case <-i.sync.StopChan:
			i.logger.Debug("run:stop")
			return
		}
	}
}
