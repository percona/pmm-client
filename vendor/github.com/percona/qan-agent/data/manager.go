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

package data

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/percona/pmm/proto"
	pc "github.com/percona/pmm/proto/config"
	"github.com/percona/qan-agent/pct"
)

const (
	DEFAULT_DATA_ENCODING      = "gzip"
	DEFAULT_DATA_SEND_INTERVAL = 63
	DEFAULT_DATA_MAX_AGE       = 3600 * 24         // 1d
	DEFAULT_DATA_MAX_SIZE      = 1024 * 1024 * 100 // 100 MiB
	DEFAULT_DATA_MAX_FILES     = 1000
)

type Manager struct {
	logger   *pct.Logger
	dataDir  string
	trashDir string
	hostname string
	client   pct.WebsocketClient
	// --
	setConfig string
	config    *pc.Data
	running   bool
	mux       *sync.Mutex // guards config and running
	sz        proto.Serializer
	spooler   Spooler
	sender    *Sender
	status    *pct.Status
}

func NewManager(logger *pct.Logger, dataDir, trashDir, hostname string, client pct.WebsocketClient) *Manager {
	m := &Manager{
		logger:   logger,
		dataDir:  dataDir,
		trashDir: trashDir,
		hostname: hostname,
		client:   client,
		// --
		status: pct.NewStatus([]string{"data"}),
		mux:    &sync.Mutex{},
	}
	return m
}

/////////////////////////////////////////////////////////////////////////////
// Interface
/////////////////////////////////////////////////////////////////////////////

func (m *Manager) Start() error {
	m.mux.Lock()
	defer m.mux.Unlock()

	if m.config != nil {
		return pct.ServiceIsRunningError{Service: "data"}
	}

	m.status.Update("data", "Starting")

	// Load config from disk (optional, but should exist).
	config := &pc.Data{}
	set, err := pct.Basedir.ReadConfig("data", config)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := m.validateConfig(config); err != nil {
		return err
	}
	m.setConfig = set

	// Make data and trash dirs used/shared by all services (mm, qan, etc.).
	if err := pct.MakeDir(m.dataDir); err != nil {
		return err
	}
	if err := pct.MakeDir(m.trashDir); err != nil {
		return err
	}

	// Make data serializer/encoder, e.g. T{} -> gzip -> []byte.
	sz, err := makeSerializer(config.Encoding)
	if err != nil {
		return err
	}

	// Make persistent (disk-back) key-value cache and start data spooler.
	m.status.Update("data", "Starting spooler")
	spooler := NewDiskvSpooler(
		pct.NewLogger(m.logger.LogChan(), "data-spooler"),
		m.dataDir,
		m.trashDir,
		m.hostname,
		config.Limits,
	)
	if err := spooler.Start(sz); err != nil {
		return err
	}
	m.spooler = spooler

	// Start data sender.
	m.status.Update("data", "Starting sender")
	sender := NewSender(
		pct.NewLogger(m.logger.LogChan(), "data-sender"),
		m.client,
	)
	err = sender.Start(
		m.spooler,
		time.Tick(time.Duration(config.SendInterval)*time.Second),
		config.SendInterval,
		pct.ToBool(config.Blackhole),
	)
	if err != nil {
		return err
	}
	m.sender = sender

	m.config = config
	m.running = true

	m.logger.Info("Started")
	m.status.Update("data", "Running")
	return nil
}

func (m *Manager) Stop() error {
	m.status.Update("data", "Stopping sender")
	m.sender.Stop()

	m.status.Update("data", "Stopping spooler")
	m.spooler.Stop()

	m.logger.Info("data", "Stopped")
	m.status.Update("data", "Stopped")

	m.mux.Lock()
	defer m.mux.Unlock()
	m.running = false

	return nil
}

func (m *Manager) Handle(cmd *proto.Cmd) *proto.Reply {
	m.status.UpdateRe("data", "Handling", cmd)
	defer m.status.Update("data", "Running")

	m.logger.Info("Handle", cmd)

	switch cmd.Cmd {
	case "GetConfig":
		config, errs := m.GetConfig()
		return cmd.Reply(config, errs...)
	case "SetConfig":
		newConfig, errs := m.handleSetConfig(cmd)
		return cmd.Reply(newConfig, errs...)
	default:
		return cmd.Reply(nil, pct.UnknownCmdError{Cmd: cmd.Cmd})
	}
}

func (m *Manager) Status() map[string]string {
	return m.status.Merge(m.client.Status(), m.spooler.Status(), m.sender.Status())
}

func (m *Manager) GetConfig() ([]proto.AgentConfig, []error) {
	m.logger.Debug("GetConfig:call")
	defer m.logger.Debug("GetConfig:return")
	m.mux.Lock()
	defer m.mux.Unlock()
	bytes, err := json.Marshal(m.config)
	if err != nil {
		return nil, []error{err}
	}
	// Configs are always returned as array of AgentConfig resources.
	config := proto.AgentConfig{
		Service: "data",
		Set:     m.setConfig,
		Running: string(bytes),
	}
	return []proto.AgentConfig{config}, nil
}

func (m *Manager) GetDefaults() map[string]interface{} {
	return map[string]interface{}{
		"DataEncoding": DEFAULT_DATA_ENCODING,
		"SendInterval": DEFAULT_DATA_SEND_INTERVAL,
		"MaxAge":       DEFAULT_DATA_MAX_AGE,
		"MaxSize":      DEFAULT_DATA_MAX_SIZE,
		"MaxFiles":     DEFAULT_DATA_MAX_FILES,
	}
}

func (m *Manager) Spooler() Spooler {
	return m.spooler
}

func (m *Manager) Sender() *Sender {
	return m.sender
}

func (m *Manager) validateConfig(config *pc.Data) error {
	if config.Encoding == "" {
		config.Encoding = DEFAULT_DATA_ENCODING
	} else if config.Encoding != "none" && config.Encoding != "gzip" {
		return fmt.Errorf("Invalid Encoding: '%s', must be 'none' or 'gzip'", config.Encoding)
	}
	if config.SendInterval < 0 {
		return errors.New("SendInterval must be > 0")
	} else if config.SendInterval > 3600 {
		// Don't want to let the spool grow too large.  This doesn't affect
		// how much spool can hold within the hour, only that we should try
		// to send data and reduce spool at least once an hour.
		return errors.New("SendInterval must be <= 3600 (1 hour)")
	} else if config.SendInterval == 0 {
		config.SendInterval = DEFAULT_DATA_SEND_INTERVAL
	}

	if config.Limits.MaxAge == 0 {
		config.Limits.MaxAge = DEFAULT_DATA_MAX_AGE
	}
	if config.Limits.MaxSize == 0 {
		config.Limits.MaxSize = DEFAULT_DATA_MAX_SIZE
	}
	if config.Limits.MaxFiles == 0 {
		config.Limits.MaxFiles = DEFAULT_DATA_MAX_FILES
	}

	return nil
}

func (m *Manager) handleSetConfig(cmd *proto.Cmd) (interface{}, []error) {
	newConfig := &pc.Data{}
	if err := json.Unmarshal(cmd.Data, newConfig); err != nil {
		return nil, []error{err}
	}

	if err := m.validateConfig(newConfig); err != nil {
		return nil, []error{err}
	}

	m.mux.Lock()
	defer m.mux.Unlock()
	finalConfig := *m.config // copy current config

	errs := []error{}

	/**
	 * Data sender
	 */

	if newConfig.SendInterval != finalConfig.SendInterval {
		m.sender.Stop()
		err := m.sender.Start(
			m.spooler,
			time.Tick(time.Duration(newConfig.SendInterval)*time.Second),
			newConfig.SendInterval,
			pct.ToBool(newConfig.Blackhole),
		)
		if err != nil {
			errs = append(errs, err)
		} else {
			finalConfig.SendInterval = newConfig.SendInterval
		}
	}

	/**
	 * Data spooler
	 */

	if newConfig.Encoding != finalConfig.Encoding {
		sz, err := makeSerializer(newConfig.Encoding)
		if err != nil {
			errs = append(errs, err)
		} else {
			m.spooler.Stop()
			if err := m.spooler.Start(sz); err != nil {
				errs = append(errs, err)
			} else {
				finalConfig.Encoding = newConfig.Encoding
			}
		}
	}

	// Write the new, updated config.  If this fails, agent will use old config if restarted.
	if err := pct.Basedir.WriteConfig("data", finalConfig); err != nil {
		errs = append(errs, errors.New("data.WriteConfig:"+err.Error()))
	}

	m.setConfig = string(cmd.Data)
	m.config = &finalConfig

	return m.config, errs
}

func makeSerializer(encoding string) (proto.Serializer, error) {
	switch encoding {
	case "none":
		return proto.NewJsonSerializer(), nil
	case "gzip":
		return proto.NewJsonGzipSerializer(), nil
	default:
		return nil, errors.New("Unknown encoding: " + encoding)
	}
}
