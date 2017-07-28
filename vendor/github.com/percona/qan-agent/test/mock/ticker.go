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

/**
 * Implements ticker.Ticker interface
 */

import (
	"time"
)

type Ticker struct {
	syncChan    chan bool
	Added       []chan time.Time
	RunningChan chan bool
}

func NewTicker(syncChan chan bool) *Ticker {
	t := &Ticker{
		syncChan:    syncChan,
		Added:       []chan time.Time{},
		RunningChan: make(chan bool),
	}
	return t
}

func (t *Ticker) Run(now int64) {
	if t.syncChan != nil {
		<-t.syncChan
	}
	t.RunningChan <- true
	return
}

func (t *Ticker) Stop() {
	t.RunningChan <- false
	return
}

func (t *Ticker) Add(c chan time.Time) {
	t.Added = append(t.Added, c)
}

func (t *Ticker) Remove(c chan time.Time) {
}

func (t *Ticker) ETA(now int64) float64 {
	return 0.1
}
