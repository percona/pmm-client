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
	"github.com/percona/pmm/proto"
)

type Spooler struct {
	FilesOut      []string          // test provides
	DataOut       map[string][]byte // test provides
	DataIn        []interface{}
	dataChan      chan interface{}
	RejectedFiles []string
}

func NewSpooler(dataChan chan interface{}) *Spooler {
	s := &Spooler{
		dataChan:      dataChan,
		DataIn:        []interface{}{},
		RejectedFiles: []string{},
	}
	return s
}

func (s *Spooler) Start(sz proto.Serializer) error {
	return nil
}

func (s *Spooler) Stop() error {
	return nil
}

func (s *Spooler) Status() map[string]string {
	return map[string]string{"spooler": "ok"}
}

func (s *Spooler) Write(service string, data interface{}) error {
	if s.dataChan != nil {
		s.dataChan <- data
	} else {
		s.DataIn = append(s.DataIn, data)
	}
	return nil
}

func (s *Spooler) Files() <-chan string {
	filesChan := make(chan string)
	go func() {
		for _, file := range s.FilesOut {
			filesChan <- file
		}
		close(filesChan)
	}()
	return filesChan
}

func (s *Spooler) CancelFiles() {
}

func (s *Spooler) Read(file string) ([]byte, error) {
	return s.DataOut[file], nil
}

func (s *Spooler) Remove(file string) error {
	delete(s.DataOut, file)
	return nil
}

func (s *Spooler) Reject(file string) error {
	s.RejectedFiles = append(s.RejectedFiles, file)
	return s.Remove(file)
}

func (s *Spooler) Reset() {
	s.DataIn = []interface{}{}
	s.RejectedFiles = []string{}
}
