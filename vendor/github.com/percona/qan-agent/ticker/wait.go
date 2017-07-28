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

package ticker

import (
	"log"
	"time"

	"github.com/percona/qan-agent/pct"
)

type WaitTicker struct {
	atInterval uint
	ticker     *time.Ticker
	watcher    chan time.Time
	sync       *pct.SyncChan
}

func NewWaitTicker(atInterval uint) *WaitTicker {
	wt := &WaitTicker{
		atInterval: atInterval,
		sync:       pct.NewSyncChan(),
	}
	return wt
}

func (wt *WaitTicker) Run(nowNanosecond int64) {
	defer func() {
		if err := recover(); err != nil {
			log.Println("WaitTicker.Run crashed: ", err)
		}
		wt.sync.Done()
	}()

	// Wait for watcher (the code using this ticker) to recv the first tick.
	select {
	case wt.watcher <- time.Now().UTC():
	case <-wt.sync.StopChan:
		return
	}

	// Sleep for the interval after first tick has been received.
	wt.ticker = time.NewTicker(time.Duration(wt.atInterval) * time.Second)

	// Tick every interval seconds as usual.
	for {
		select {
		case now := <-wt.ticker.C:
			select {
			case wt.watcher <- now.UTC():
			default:
			}
		case <-wt.sync.StopChan:
			return
		}
	}
}

func (wt *WaitTicker) Stop() {
	wt.sync.Stop()
	wt.sync.Wait()
	wt.ticker.Stop()
	wt.ticker = nil
}

func (wt *WaitTicker) Add(c chan time.Time) {
	wt.watcher = c
}

func (wt *WaitTicker) Remove(c chan time.Time) {
	wt.watcher = nil
}

func (wt *WaitTicker) ETA(now int64) float64 {
	return 0 // todo
}
