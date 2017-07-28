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

package mrms

import (
	"time"

	"github.com/percona/pmm/proto"
	"github.com/percona/qan-agent/pct"
)

const (
	SERVICE_NAME     = "mrms"
	MONITOR_INTERVAL = 50 * time.Second
)

type Manager struct {
	logger  *pct.Logger
	monitor Monitor
	// --
	status *pct.Status
}

func NewManager(logger *pct.Logger, monitor Monitor) *Manager {
	m := &Manager{
		logger:  logger,
		monitor: monitor,
		// --
		status: pct.NewStatus([]string{SERVICE_NAME}),
	}
	return m
}

func (m *Manager) Start() error {
	m.status.Update(SERVICE_NAME, "Starting")
	if err := m.monitor.Start(MONITOR_INTERVAL); err != nil {
		m.logger.Warn("Failed to start %s: %s", SERVICE_NAME, err)
		return err
	}
	m.logger.Info("Started")
	m.status.Update(SERVICE_NAME, "Running")
	return nil
}

func (m *Manager) Stop() error {
	m.status.Update(SERVICE_NAME, "Stopping")
	if err := m.monitor.Stop(); err != nil {
		m.logger.Warn("Failed to stop %s: %s", SERVICE_NAME, err)
		return err
	}
	m.logger.Info("Stopped")
	m.status.Update(SERVICE_NAME, "Stopped")
	return nil
}

func (m *Manager) GetConfig() ([]proto.AgentConfig, []error) {
	return nil, nil
}

func (m *Manager) GetDefaults() map[string]interface{} {
	return nil
}

func (m *Manager) Handle(cmd *proto.Cmd) *proto.Reply {
	return cmd.Reply(nil, pct.UnknownCmdError{Cmd: cmd.Cmd})
}

func (m *Manager) Status() (status map[string]string) {
	status = m.status.All()
	monitorStatus := m.monitor.Status()
	for k, v := range monitorStatus {
		status[k] = v
	}
	return status
}
