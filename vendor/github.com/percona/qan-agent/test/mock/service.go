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
	"fmt"

	"github.com/percona/qan-agent/pct"
	"github.com/percona/pmm/proto"
)

type MockServiceManager struct {
	name         string
	traceChan    chan string
	readyChan    chan bool
	StartErr     error
	StopErr      error
	IsRunningVal bool
	status       *pct.Status
	Cmds         []*proto.Cmd
}

func NewMockServiceManager(name string, readyChan chan bool, traceChan chan string) *MockServiceManager {
	m := &MockServiceManager{
		name:      name,
		readyChan: readyChan,
		traceChan: traceChan,
		status:    pct.NewStatus([]string{name}),
		Cmds:      []*proto.Cmd{},
	}
	return m
}

func (m *MockServiceManager) Start() error {
	m.traceChan <- fmt.Sprintf("Start %s", m.name)
	// Return when caller is ready.  This allows us to simulate slow starts.
	m.status.Update(m.name, "Starting")
	<-m.readyChan
	m.IsRunningVal = true
	m.status.Update(m.name, "Ready")
	return m.StartErr
}

func (m *MockServiceManager) Stop() error {
	m.traceChan <- "Stop " + m.name
	// Return when caller is ready.  This allows us to simulate slow stops.
	m.status.Update(m.name, "Stopping")
	<-m.readyChan
	m.IsRunningVal = false
	m.status.Update(m.name, "Stopped")
	return m.StopErr
}

func (m *MockServiceManager) Status() map[string]string {
	m.traceChan <- "Status " + m.name
	return m.status.All()
}

func (m *MockServiceManager) GetConfig() ([]proto.AgentConfig, []error) {
	configs := []proto.AgentConfig{
		{
			Service: m.name,
			Set:     `{"Foo":"bar"}`,
		},
	}
	return configs, nil
}

func (m *MockServiceManager) IsRunning() bool {
	m.traceChan <- "IsRunning " + m.name
	return m.IsRunningVal
}

func (m *MockServiceManager) Handle(cmd *proto.Cmd) *proto.Reply {
	m.Cmds = append(m.Cmds, cmd)
	return cmd.Reply(nil)
}

func (m *MockServiceManager) Reset() {
	m.status.Update(m.name, "")
}
