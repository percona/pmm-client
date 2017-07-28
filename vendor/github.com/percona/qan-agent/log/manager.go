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

package log

import (
	"encoding/json"
	"errors"
	"os"
	"sync"
	"time"

	"github.com/percona/pmm/proto"
	pc "github.com/percona/pmm/proto/config"
	"github.com/percona/qan-agent/pct"
)

const (
	DEFAULT_LOG_LEVEL = "warning"
)

type Manager struct {
	client  pct.WebsocketClient
	logChan chan proto.LogEntry
	// --
	setConfig string
	config    *pc.Log
	running   bool
	mux       *sync.RWMutex // guards config and running
	logger    *pct.Logger
	relay     *Relay
	status    *pct.Status
}

func NewManager(client pct.WebsocketClient, logChan chan proto.LogEntry) *Manager {
	m := &Manager{
		client:  client,
		logChan: logChan,
		// --
		status: pct.NewStatus([]string{"log"}),
		mux:    &sync.RWMutex{},
	}
	return m
}

func (m *Manager) Start() error {
	m.mux.Lock()
	defer m.mux.Unlock()

	if m.config != nil {
		return pct.ServiceIsRunningError{Service: "log"}
	}

	// Load config from disk.
	config := &pc.Log{}
	set, err := pct.Basedir.ReadConfig("log", config)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := m.validateConfig(config); err != nil {
		return err
	}
	m.setConfig = set

	// Start relay (it buffers and sends log entries to API).
	level := proto.LogLevelNumber[config.Level]
	m.relay = NewRelay(m.client, m.logChan, level, pct.ToBool(config.Offline))
	go m.relay.Run()

	m.logger = pct.NewLogger(m.relay.LogChan(), "log")
	m.config = config
	m.running = true

	m.logger.Info("Started")
	m.status.Update("log", "Running")
	return nil
}

func (m *Manager) Stop() error {
	m.relay.Stop()
	return nil
}

func (m *Manager) Handle(cmd *proto.Cmd) *proto.Reply {
	m.status.UpdateRe("log", "Handling", cmd)
	defer m.status.Update("log", "Running")

	switch cmd.Cmd {
	case "SetConfig":
		m.mux.Lock()
		defer m.mux.Unlock()

		// proto.Cmd[Service:log, Cmd:SetConfig, Data:log.Config]
		newConfig := &pc.Log{}
		if err := json.Unmarshal(cmd.Data, newConfig); err != nil {
			return cmd.Reply(nil, err)
		}

		if err := m.validateConfig(newConfig); err != nil {
			return cmd.Reply(nil, err)
		}
		m.setConfig = string(cmd.Data)

		errs := []error{}
		if m.config.Level != newConfig.Level { // log level has changed
			level := proto.LogLevelNumber[newConfig.Level] // already validated
			select {
			case m.relay.LogLevelChan() <- level:
				m.config.Level = newConfig.Level
			case <-time.After(3 * time.Second):
				errs = append(errs, errors.New("Timeout setting new log level"))
			}
		}

		// Write the new, updated config.  If this fails, agent will use old config if restarted.
		if err := pct.Basedir.WriteConfig("log", m.config); err != nil {
			errs = append(errs, errors.New("log.WriteConfig:"+err.Error()))
		}

		return cmd.Reply(m.config, errs...)
	case "GetConfig":
		config, errs := m.GetConfig()
		return cmd.Reply(config, errs...)
	case "Reconnect":
		m.client.Disconnect()
		return cmd.Reply(nil)
	default:
		return cmd.Reply(nil, pct.UnknownCmdError{Cmd: cmd.Cmd})
	}
}

func (m *Manager) Status() map[string]string {
	return m.status.Merge(m.client.Status(), m.relay.Status())
}

func (m *Manager) GetConfig() ([]proto.AgentConfig, []error) {
	m.mux.Lock()
	defer m.mux.Unlock()
	bytes, err := json.Marshal(m.config)
	if err != nil {
		return nil, []error{err}
	}
	// Configs are always returned as array of AgentConfig resources.
	config := proto.AgentConfig{
		Service: "log",
		Set:     m.setConfig,
		Running: string(bytes),
	}
	return []proto.AgentConfig{config}, nil
}

func (m *Manager) GetDefaults() map[string]interface{} {
	return map[string]interface{}{
		"LogLevel": DEFAULT_LOG_LEVEL,
	}
}

func (m *Manager) Relay() *Relay {
	return m.relay
}

func (m *Manager) validateConfig(config *pc.Log) error {
	if config.Level == "" {
		config.Level = DEFAULT_LOG_LEVEL
	} else {
		if _, ok := proto.LogLevelNumber[config.Level]; !ok {
			return errors.New("Invalid log level: " + config.Level)
		}
	}
	return nil
}
