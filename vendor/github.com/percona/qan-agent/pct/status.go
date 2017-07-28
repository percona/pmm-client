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
	"github.com/percona/pmm/proto"
	"sync"
)

type StatusReporter interface {
	Status() map[string]string
}

type Status struct {
	status map[string]string
	mux    *sync.RWMutex
}

func NewStatus(procs []string) *Status {
	status := make(map[string]string)
	for _, proc := range procs {
		status[proc] = ""
	}
	s := &Status{
		status: status,
		mux:    &sync.RWMutex{},
	}
	return s
}

func (s *Status) Update(proc string, status string) {
	s.mux.Lock()
	defer s.mux.Unlock()
	if _, ok := s.status[proc]; !ok {
		return
	}
	s.status[proc] = status
}

func (s *Status) UpdateRe(proc string, status string, cmd *proto.Cmd) {
	s.mux.Lock()
	defer s.mux.Unlock()
	if _, ok := s.status[proc]; !ok {
		return
	}
	s.status[proc] = fmt.Sprintf("%s %s", status, cmd)
}

func (s *Status) Get(proc string) string {
	s.mux.RLock()
	defer s.mux.RUnlock()
	status, ok := s.status[proc]
	if !ok {
		return "ERROR: " + proc + " status not found"
	}
	return status
}

func (s *Status) All() map[string]string {
	all := make(map[string]string)
	s.mux.RLock()
	defer s.mux.RUnlock()
	for proc, status := range s.status {
		all[proc] = status
	}
	return all
}

func (s *Status) Merge(others ...map[string]string) map[string]string {
	status := s.All()
	for _, otherStatus := range others {
		for k, v := range otherStatus {
			status[k] = v
		}
	}
	return status
}
