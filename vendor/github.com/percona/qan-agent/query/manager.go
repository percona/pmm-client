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

package query

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/percona/pmm/proto"
	"github.com/percona/qan-agent/instance"
	"github.com/percona/qan-agent/mysql"
	"github.com/percona/qan-agent/pct"
	mysqlExec "github.com/percona/qan-agent/query/mysql"
)

const (
	SERVICE_NAME = "query"
)

type Manager struct {
	logger       *pct.Logger
	instanceRepo *instance.Repo
	connFactory  mysql.ConnectionFactory
	// --
	running bool
	sync.Mutex
	status *pct.Status
}

func NewManager(logger *pct.Logger, instanceRepo *instance.Repo, connFactory mysql.ConnectionFactory) *Manager {
	m := &Manager{
		logger:       logger,
		instanceRepo: instanceRepo,
		connFactory:  connFactory,
		// --
		status: pct.NewStatus([]string{SERVICE_NAME}),
	}
	return m
}

/////////////////////////////////////////////////////////////////////////////
// Interface
/////////////////////////////////////////////////////////////////////////////

func (m *Manager) Start() error {
	m.Lock()
	defer m.Unlock()
	if m.running {
		return pct.ServiceIsRunningError{Service: SERVICE_NAME}
	}
	m.running = true
	m.logger.Info("Started")
	m.status.Update(SERVICE_NAME, "Idle")
	return nil
}

func (m *Manager) Stop() error {
	// Let user stop this tool in case they don't want agent executing queries.
	m.Lock()
	defer m.Unlock()
	if !m.running {
		return nil
	}
	m.running = false
	m.logger.Info("Stopped")
	m.status.Update(SERVICE_NAME, "Stopped")
	return nil
}

func (m *Manager) Handle(cmd *proto.Cmd) *proto.Reply {
	m.Lock()
	defer m.Unlock()

	// Don't query if this tool is stopped.
	if !m.running {
		return cmd.Reply(nil, pct.ServiceIsNotRunningError{})
	}

	m.status.UpdateRe(SERVICE_NAME, "Handling", cmd)
	defer m.status.Update(SERVICE_NAME, "Idle")

	// See which type of subsystem this query is for. Right now we only support
	// MySQL, but this abstraction will make adding other subsystems easy.
	var in proto.Instance
	if err := json.Unmarshal(cmd.Data, &in); err != nil {
		return cmd.Reply(nil, err)
	}

	in, err := m.instanceRepo.Get(in.UUID, false) // false = don't cache
	if err != nil {
		return cmd.Reply(nil, err)
	}

	switch in.Subsystem {
	case "mysql":
		return m.handleMySQLQuery(cmd, in)
	default:
		return cmd.Reply(nil, fmt.Errorf("Can only execute MySQL queries"))
	}
}

func (m *Manager) Status() map[string]string {
	return m.status.All()
}

func (m *Manager) GetConfig() ([]proto.AgentConfig, []error) {
	return nil, nil
}

func (m *Manager) GetDefaults() map[string]interface{} {
	return nil
}

// --------------------------------------------------------------------------

func (m *Manager) handleMySQLQuery(cmd *proto.Cmd, in proto.Instance) *proto.Reply {
	m.logger.Debug("handleMySQLQuery:call")
	defer m.logger.Debug("handleMySQLQuery:return")

	conn := m.connFactory.Make(in.DSN)
	if err := conn.Connect(); err != nil {
		return cmd.Reply(nil, err)
	}
	defer conn.Close()

	// Create a MySQL query executor to do the actual work.
	e := mysqlExec.NewQueryExecutor(conn)

	// Execute the query.
	m.logger.Debug(cmd.Cmd + ":" + in.Name)
	switch cmd.Cmd {
	case "Explain":
		m.status.Update(SERVICE_NAME, "EXPLAIN query on "+in.Name)
		q := &proto.ExplainQuery{}
		if err := json.Unmarshal(cmd.Data, q); err != nil {
			return cmd.Reply(nil, err)
		}
		res, err := e.Explain(q.Db, q.Query, q.Convert)
		if err != nil {
			return cmd.Reply(nil, fmt.Errorf("EXPLAIN failed: %s", err))
		}
		return cmd.Reply(res, nil)
	case "TableInfo":
		m.status.Update(SERVICE_NAME, "Table Info queries on "+in.Name)
		tableInfo := &proto.TableInfoQuery{}
		if err := json.Unmarshal(cmd.Data, tableInfo); err != nil {
			return cmd.Reply(nil, err)
		}
		res, err := e.TableInfo(tableInfo)
		if err != nil {
			return cmd.Reply(nil, fmt.Errorf("Table Info failed: %s", err))
		}
		return cmd.Reply(res, nil)
	default:
		return cmd.Reply(nil, pct.UnknownCmdError{Cmd: cmd.Cmd})
	}
}
