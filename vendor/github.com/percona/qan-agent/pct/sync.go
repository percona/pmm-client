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

package pct

import (
	"fmt"
	golog "log"
	"os"
	"time"
)

type SyncChan struct {
	StartChan chan bool
	StopChan  chan bool
	DoneChan  chan bool
	CrashChan chan bool
	Crash     bool
}

func NewSyncChan() *SyncChan {
	sc := &SyncChan{
		StartChan: make(chan bool),
		StopChan:  make(chan bool),
		DoneChan:  make(chan bool, 1),
		CrashChan: make(chan bool, 1),
		Crash:     true,
	}
	return sc
}

func (sync *SyncChan) Start() bool {
	started := false
	select {
	case sync.StartChan <- true:
		started = <-sync.StartChan
	default:
	}
	return started
}

func (sync *SyncChan) Stop() {
	sync.StopChan <- true
}

func (sync *SyncChan) Wait() {
	select {
	case <-sync.CrashChan:
	case <-sync.DoneChan:
	}
}

func (sync *SyncChan) Done() {
	if sync.Crash {
		sync.CrashChan <- true
	} else {
		sync.DoneChan <- true
	}
}

func (sync *SyncChan) Graceful() {
	sync.Crash = false
}

func (sync *SyncChan) IsGraceful() bool {
	return !sync.Crash
}

// --------------------------------------------------------------------------

func MakeStartLock() error {
	flags := os.O_CREATE | os.O_EXCL | os.O_WRONLY
	file, err := os.OpenFile(Basedir.File("start-lock"), flags, 0644)
	if err != nil {
		return err
	}
	return file.Close()
}

func WaitStartLock() error {
	startLockFile := Basedir.File("start-lock")
	if startLockExists := FileExists(startLockFile); startLockExists {
		golog.Printf("Start-lock file %s exists; agent starts when removed, else aborts in 1 minute...",
			startLockFile)
		for i := 0; i < 60 && startLockExists; i++ {
			time.Sleep(500 * time.Millisecond)
			startLockExists = FileExists(startLockFile)
		}
		if startLockExists {
			return fmt.Errorf("Start-lock file %s not removed after 1 minute", startLockFile)
		}
	}
	return nil
}
