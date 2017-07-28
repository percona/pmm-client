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
	"github.com/percona/go-mysql/log"
)

type LogParser struct {
	eventChan chan *log.Event
}

func NewLogParser() *LogParser {
	p := &LogParser{
		eventChan: make(chan *log.Event),
	}
	return p
}

func (p *LogParser) Start() error {
	return nil
}

func (p *LogParser) Stop() {
	close(p.eventChan)
	return
}

func (p *LogParser) EventChan() <-chan *log.Event {
	return p.eventChan
}

func (p *LogParser) Send(e *log.Event) {
	p.eventChan <- e
}
