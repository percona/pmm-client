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

package mock

import (
	"github.com/percona/qan-agent/pct"
	"github.com/percona/qan-agent/qan"
	"github.com/percona/qan-agent/qan/slowlog"
	"time"
)

type IntervalIterFactory struct {
	Iters     []qan.IntervalIter
	iterNo    int
	TickChans map[qan.IntervalIter]chan time.Time
}

func (tf *IntervalIterFactory) Make(collectFrom string, filename slowlog.FilenameFunc, tickChan chan time.Time) qan.IntervalIter {
	if tf.iterNo >= len(tf.Iters) {
		return tf.Iters[tf.iterNo-1]
	}
	nextIter := tf.Iters[tf.iterNo]
	tf.TickChans[nextIter] = tickChan
	tf.iterNo++
	return nextIter
}

func (tf *IntervalIterFactory) Reset() {
	tf.iterNo = 0
}

// --------------------------------------------------------------------------

type Iter struct {
	testIntervalChan chan *qan.Interval
	intervalChan     chan *qan.Interval
	sync             *pct.SyncChan
	tickChan         chan time.Time
	calls            []string
}

func NewIter(intervalChan chan *qan.Interval) *Iter {
	iter := &Iter{
		testIntervalChan: intervalChan,
		// --
		intervalChan: make(chan *qan.Interval, 1),
		sync:         pct.NewSyncChan(),
		tickChan:     make(chan time.Time),
		calls:        []string{},
	}
	return iter
}

func (i *Iter) Start() {
	i.calls = append(i.calls, "Start")
	go i.run()
}

func (i *Iter) Stop() {
	i.calls = append(i.calls, "Stop")
	i.sync.Stop()
	i.sync.Wait()
}

func (i *Iter) IntervalChan() chan *qan.Interval {
	return i.intervalChan
}

func (i *Iter) TickChan() chan time.Time {
	return i.tickChan
}

func (i *Iter) run() {
	defer func() {
		i.sync.Done()
	}()
	for {
		select {
		case <-i.sync.StopChan:
			return
		case interval := <-i.testIntervalChan:
			i.intervalChan <- interval
		}
	}
}

func (i *Iter) Calls() []string {
	return i.calls
}

func (i *Iter) Reset() {
	i.calls = []string{}
}
