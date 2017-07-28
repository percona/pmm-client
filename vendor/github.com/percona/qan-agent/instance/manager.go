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

package instance

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/percona/go-mysql/dsn"
	"github.com/percona/pmm/proto"
	"github.com/percona/qan-agent/mrms"
	"github.com/percona/qan-agent/mysql"
	"github.com/percona/qan-agent/pct"
)

var (
	ErrDuplicateInstance = errors.New("duplicate instance")
	ErrInstanceNotFound  = errors.New("instance not found")
	ErrCmdNotSupport     = errors.New("command not supported")
	ErrNoLink            = errors.New("no instances link from API")
)

type Manager struct {
	logger *pct.Logger
	api    pct.APIConnector
	// --
	status      *pct.Status
	repo        *Repo
	restartChan chan proto.Instance
	stopChan    chan struct{}
}

func NewManager(logger *pct.Logger, instanceDir string, api pct.APIConnector, monitor mrms.Monitor) *Manager {
	repo := NewRepo(pct.NewLogger(logger.LogChan(), "instance-repo"), instanceDir, api)

	m := &Manager{
		logger: logger,
		api:    api,
		// --
		status:      pct.NewStatus([]string{"instance", "instance-repo", "instance-mrms"}),
		repo:        repo,
		restartChan: monitor.Add(proto.Instance{}),
		stopChan:    make(chan struct{}),
	}
	return m
}

/////////////////////////////////////////////////////////////////////////////
// Interface
/////////////////////////////////////////////////////////////////////////////

func (m *Manager) Start() error {
	m.status.Update("instance", "Starting")
	if err := m.repo.Init(); err != nil {
		m.status.Update("instance", "Failed to start repo: "+err.Error())
		m.status.Update("instance-repo", "Failed to start: "+err.Error())
		return fmt.Errorf("cannot start repo: %s", err)
	}
	m.status.Update("instance-repo", "Idle")

	go m.updateMySQLInstances()
	go m.monitor()

	m.logger.Info("Started")
	m.status.Update("instance", "Running")
	return nil
}

func (m *Manager) Stop() error {
	close(m.stopChan) // stop monitor()
	m.status.Update("instance", "Stopped")
	m.status.Update("instance-repo", "Stopped")
	return nil
}

func (m *Manager) Status() map[string]string {
	return m.status.All()
}

func (m *Manager) Handle(cmd *proto.Cmd) *proto.Reply {
	m.status.UpdateRe("instance", "Handling", cmd)
	defer m.status.Update("instance", "Running")

	switch cmd.Cmd {
	case "RemoveInstance":
		err := m.repo.Remove(string(cmd.Data))
		return cmd.Reply(nil, err)
	case "GetInfo":
		var in proto.Instance
		if err := json.Unmarshal(cmd.Data, &in); err != nil {
			return cmd.Reply(nil, err)
		}
		if in.Subsystem != "mysql" {
			return cmd.Reply(nil, ErrCmdNotSupport)
		}
		err := GetMySQLInfo(&in)
		return cmd.Reply(in, err)
	}
	return cmd.Reply(nil, pct.UnknownCmdError{Cmd: cmd.Cmd})
}

func (m *Manager) GetConfig() ([]proto.AgentConfig, []error) {
	return nil, nil
}

func (m *Manager) GetDefaults() map[string]interface{} {
	return nil
}

func (m *Manager) Repo() *Repo {
	return m.repo
}

func GetMySQLInfo(in *proto.Instance) error {
	conn := mysql.NewConnection(in.DSN)
	if err := conn.Connect(); err != nil {
		return err
	}
	defer conn.Close()
	sql := "SELECT /* percona-qan-agent */" +
		" CONCAT_WS('.', @@hostname, IF(@@port='3306',NULL,@@port)) AS Hostname," +
		" @@version_comment AS Distro," +
		" @@version AS Version"
	// Need auxiliary vars because can't get map attribute addresses
	var hostname, distro, version string
	if err := conn.DB().QueryRow(sql).Scan(&hostname, &distro, &version); err != nil {
		return err
	}
	in.Distro = distro
	in.Version = version
	return nil
}

/////////////////////////////////////////////////////////////////////////////
// Implementation
/////////////////////////////////////////////////////////////////////////////

func (m *Manager) monitor() {
	m.logger.Debug("monitor:call")
	defer m.logger.Debug("monitor:return")

	defer func() {
		if err := recover(); err != nil {
			m.logger.Error("MySQL connection crashed: ", err)
			m.status.Update("instance-mrms", "Crashed")
		} else {
			m.status.Update("instance-mrms", "Stopped")
		}
	}()

	for {
		m.status.Update("instance-mrms", "Idle")
		select {
		case in := <-m.restartChan:
			safeDSN := dsn.HidePassword(in.DSN)
			m.logger.Debug("mrms:restart:" + fmt.Sprintf("%s:%s", in.UUID, safeDSN))
			m.status.Update("instance-mrms", "Getting info "+safeDSN)
			if err := GetMySQLInfo(&in); err != nil {
				m.logger.Warn(fmt.Sprintf("Failed to get MySQL info %s: %s", safeDSN, err))
				continue
			}
			if err := m.updateInstance(in); err != nil {
				m.logger.Warn(err)
			}
		case <-m.stopChan:
			return
		}
	}
}

func (m *Manager) updateInstance(in proto.Instance) error {
	m.logger.Debug("updateInstance:call")
	defer m.logger.Debug("updateInstance:return")

	m.status.Update("instance-mrms", "Updating info "+in.UUID)

	data, err := json.Marshal(&in)
	if err != nil {
		return err
	}

	link := m.api.EntryLink("instances")
	if link == "" {
		return ErrNoLink
	}
	link += "/" + in.UUID
	resp, body, err := m.api.Put(link, data)
	if err != nil {
		return err
	}

	if resp.StatusCode >= 400 {
		return fmt.Errorf("PUT %s failed: code %d: %s", link, resp.StatusCode, string(body))
	}

	m.logger.Info(fmt.Sprintf("Updated %s %s", in.Subsystem, in.Name))

	return nil
}

func (m *Manager) updateMySQLInstances() {
	ready := false
	for i := 0; i < 60; i++ {
		if m.api.EntryLink("instances") == "" {
			time.Sleep(1 * time.Second)
			continue
		}
		ready = true
		break
	}
	if !ready {
		m.logger.Warn("Timeout waiting for instances link from API; MySQL instances not updated")
		return
	}
	m.logger.Info("Updating MySQL instances")
	defer m.logger.Info("Update all MySQL instances")
	for _, in := range m.repo.List("mysql") {
		safeDSN := dsn.HidePassword(in.DSN)
		m.status.Update("instance", "Getting info "+safeDSN)
		if err := GetMySQLInfo(&in); err != nil {
			m.logger.Warn(fmt.Sprintf("Failed to get MySQL info %s: %s", safeDSN, err))
			continue
		}
		if err := m.updateInstance(in); err != nil {
			m.logger.Warn(fmt.Sprintf("Cannot update %s %s (%s): %s", in.Subsystem, in.Name, in.UUID, err))
		}
	}
}
