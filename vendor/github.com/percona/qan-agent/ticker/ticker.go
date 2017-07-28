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
	"github.com/percona/qan-agent/pct"
	"log"
	"math"
	"sync"
	"time"
)

type Ticker interface {
	Run(nowNanosecond int64)
	Stop()
	Add(c chan time.Time)
	Remove(c chan time.Time)
	ETA(now int64) float64
}

type TickerFactory interface {
	Make(atInterval uint, sync bool) Ticker
}

type RealTickerFactory struct {
}

func (f *RealTickerFactory) Make(atInterval uint, sync bool) Ticker {
	if sync {
		return NewEvenTicker(atInterval, time.Sleep)
	} else {
		return NewWaitTicker(atInterval)
	}
}

type EvenTicker struct {
	atInterval uint
	sleep      func(time.Duration)
	ticker     *time.Ticker
	watcher    map[chan time.Time]bool
	watcherMux *sync.Mutex
	sync       *pct.SyncChan
}

func NewEvenTicker(atInterval uint, sleep func(time.Duration)) *EvenTicker {
	et := &EvenTicker{
		atInterval: atInterval,
		sleep:      sleep,
		watcher:    make(map[chan time.Time]bool),
		watcherMux: new(sync.Mutex),
		sync:       pct.NewSyncChan(),
	}
	return et
}

func (et *EvenTicker) Run(nowNanosecond int64) {
	defer func() {
		if err := recover(); err != nil {
			log.Println("EvenTicker.Run crashed: ", err)
		}
		et.sync.Done()
	}()
	i := float64(time.Duration(et.atInterval) * time.Second)
	d := i - math.Mod(float64(nowNanosecond), i)
	et.sleep(time.Duration(d) * time.Nanosecond)
	et.ticker = time.NewTicker(time.Duration(et.atInterval) * time.Second)
	et.tick(time.Now().UTC()) // first tick
	for {
		select {
		case now := <-et.ticker.C:
			et.tick(now.UTC())
		case <-et.sync.StopChan:
			return
		}
	}
}

func (et *EvenTicker) Stop() {
	et.sync.Stop()
	et.sync.Wait()
	et.ticker.Stop()
	et.ticker = nil
}

func (et *EvenTicker) Add(c chan time.Time) {
	et.watcherMux.Lock()
	defer et.watcherMux.Unlock()
	if !et.watcher[c] {
		et.watcher[c] = true
	}
}

func (et *EvenTicker) Remove(c chan time.Time) {
	et.watcherMux.Lock()
	defer et.watcherMux.Unlock()
	if et.watcher[c] {
		delete(et.watcher, c)
	}
}

func (et *EvenTicker) ETA(nowNanosecond int64) float64 {
	i := float64(time.Duration(et.atInterval) * time.Second)
	d := i - math.Mod(float64(nowNanosecond), i)
	return time.Duration(d).Seconds()
}

func (et *EvenTicker) tick(t time.Time) {
	et.watcherMux.Lock()
	defer et.watcherMux.Unlock()
	for c, _ := range et.watcher {
		select {
		case c <- t:
		case <-time.After(20 * time.Millisecond):
			// watcher missed this tick
		}
	}
}
