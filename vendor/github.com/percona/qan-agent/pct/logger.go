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
	"time"

	"github.com/percona/pmm/proto"
)

type Logger struct {
	logChan chan proto.LogEntry
	service string
	cmd     *proto.Cmd
}

func NewLogger(logChan chan proto.LogEntry, service string) *Logger {
	l := &Logger{
		logChan: logChan,
		service: service,
	}
	return l
}

func (l *Logger) Service() string {
	return l.service
}

func (l *Logger) LogChan() chan proto.LogEntry {
	return l.logChan
}

func (l *Logger) Debug(entry ...interface{}) {
	l.log(false, proto.LOG_DEBUG, entry)
}

func (l *Logger) DebugOffline(entry ...interface{}) {
	// Log entry is not sent to log API, only log file if enabled.
	l.log(true, proto.LOG_DEBUG, entry)
}

func (l *Logger) Info(entry ...interface{}) {
	l.log(false, proto.LOG_INFO, entry)
}

func (l *Logger) Warn(entry ...interface{}) {
	l.log(false, proto.LOG_WARNING, entry)
}

func (l *Logger) Error(entry ...interface{}) {
	l.log(false, proto.LOG_ERROR, entry)
}

func (l *Logger) Fatal(entry ...interface{}) {
	l.log(false, proto.LOG_CRITICAL, entry)
}

func (l *Logger) log(offline bool, level byte, entry []interface{}) {
	fullMsg := ""
	for i, str := range entry {
		if i > 0 {
			fullMsg += " "
		}
		fullMsg += fmt.Sprintf("%v", str)
	}
	logEntry := proto.LogEntry{
		Ts:      time.Now().UTC(),
		Level:   level,
		Service: l.service,
		Msg:     fullMsg,
		Offline: offline,
	}
	select {
	case l.logChan <- logEntry:
	default:
		// todo: lot.Println()?
		// This happens when LogRelay.LogChan() is full, which means the log relay
		// is receiving log entries faster than it can buffer and send them.
	}
}
