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
	"github.com/percona/qan-agent/qan"
	pc "github.com/percona/pmm/proto/config"
)

type QanWorker struct {
	SetupChan        chan bool
	RunChan          chan bool
	StopChan         chan bool
	CleanupChan      chan bool
	ErrorChan        chan error
	SetupCrashChan   chan bool
	RunCrashChan     chan bool
	CleanupCrashChan chan bool
	Interval         *qan.Interval
	Result           *qan.Result
}

func NewQanWorker() *QanWorker {
	w := &QanWorker{
		SetupChan:        make(chan bool, 1),
		RunChan:          make(chan bool, 1),
		StopChan:         make(chan bool, 1),
		CleanupChan:      make(chan bool, 1),
		ErrorChan:        make(chan error, 1),
		SetupCrashChan:   make(chan bool, 1),
		RunCrashChan:     make(chan bool, 1),
		CleanupCrashChan: make(chan bool, 1),
	}
	return w
}

func (w *QanWorker) Setup(interval *qan.Interval) error {
	w.Interval = interval
	w.SetupChan <- true
	return w.crashOrError()
}

func (w *QanWorker) Run() (*qan.Result, error) {
	w.RunChan <- true
	return w.Result, w.crashOrError()
}

func (w *QanWorker) Stop() error {
	w.StopChan <- true
	return w.crashOrError()
}

func (w *QanWorker) Cleanup() error {
	w.CleanupChan <- true
	return w.crashOrError()
}

func (w *QanWorker) Status() map[string]string {
	return map[string]string{
		"qan-worker": "ok",
	}
}

func (w *QanWorker) SetConfig(config pc.QAN) {
	return
}

// --------------------------------------------------------------------------

func (w *QanWorker) crashOrError() error {
	select {
	case <-w.SetupCrashChan:
		panic("mock.QanWorker setup crash")
	case <-w.RunCrashChan:
		panic("mock.QanWorker run crash")
	case <-w.CleanupCrashChan:
		panic("mock.QanWorker cleanup crash")
	default:
	}
	select {
	case err := <-w.ErrorChan:
		return err
	default:
	}
	return nil
}
