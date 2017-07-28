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

package qan

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/percona/pmm/proto"
	"github.com/percona/qan-agent/instance"
	"github.com/percona/qan-agent/mrms"
	"github.com/percona/qan-agent/mysql"
	"github.com/percona/qan-agent/pct"
	"github.com/percona/qan-agent/ticker"
)

var (
	ErrNotRunning     = errors.New("not running")
	ErrAlreadyRunning = errors.New("already running")
)

// An AnalyzerInstnace is an Analyzer ran by a Manager, one per MySQL instance
// as configured.
type AnalyzerInstance struct {
	setConfig   map[string]string
	mysqlConn   mysql.Connector
	restartChan chan proto.Instance
	tickChan    chan time.Time
	analyzer    Analyzer
}

// A Manager runs AnalyzerInstances, one per MySQL instance as configured.
type Manager struct {
	logger          *pct.Logger
	clock           ticker.Manager
	instanceRepo    *instance.Repo
	mrm             mrms.Monitor
	mysqlFactory    mysql.ConnectionFactory
	analyzerFactory AnalyzerFactory
	// --
	mux       *sync.RWMutex
	running   bool
	analyzers map[string]AnalyzerInstance
	status    *pct.Status
}

func NewManager(
	logger *pct.Logger,
	clock ticker.Manager,
	instanceRepo *instance.Repo,
	mrm mrms.Monitor,
	mysqlFactory mysql.ConnectionFactory,
	analyzerFactory AnalyzerFactory,
) *Manager {
	m := &Manager{
		logger:          logger,
		clock:           clock,
		instanceRepo:    instanceRepo,
		mrm:             mrm,
		mysqlFactory:    mysqlFactory,
		analyzerFactory: analyzerFactory,
		// --
		mux:       &sync.RWMutex{},
		analyzers: make(map[string]AnalyzerInstance),
		status:    pct.NewStatus([]string{"qan"}),
	}
	return m
}

/////////////////////////////////////////////////////////////////////////////
// Interface
/////////////////////////////////////////////////////////////////////////////

func (m *Manager) Start() error {
	m.logger.Debug("Start:call")
	defer m.logger.Debug("Start:return")

	m.mux.Lock()
	defer m.mux.Unlock()

	// Manager ("qan" in status) runs independent from qan-parser.
	m.status.Update("qan", "Starting")
	defer func() {
		m.logger.Info("Started")
		m.status.Update("qan", "Running")
	}()

	files, err := filepath.Glob(pct.Basedir.Dir("config") + "/qan-*" + pct.CONFIG_FILE_SUFFIX)
	if err != nil {
		return err
	}
	for _, file := range files {
		data, err := ioutil.ReadFile(file)
		if err != nil && !os.IsNotExist(err) {
			m.logger.Warn(fmt.Sprintf("Cannot read %s: %s", file, err))
			continue
		}

		if len(data) == 0 {
			m.logger.Warn(fmt.Sprintf("%s is empty, removing", file))
			pct.RemoveFile(file)
			continue
		}

		setConfig := map[string]string{}
		if err := json.Unmarshal(data, &setConfig); err != nil {
			m.logger.Warn(fmt.Sprintf("Cannot decode %s: %s", file, err))
			continue
		}

		// Start the slow log or perf schema analyzer. If it fails that's ok for
		// the qan manager itself (i.e. don't fail this func) because user can fix
		// or reconfigure this analyzer instance later and have qan manager try
		// again to start it.
		// todo: this fails if agent starts before MySQL is running because MRMS
		//       fails to connect to MySQL in mrms/monitor/instance.NewMysqlInstance();
		//       it should succeed and retry until MySQL is online.
		if err := m.startAnalyzer(setConfig); err != nil {
			errMsg := fmt.Sprintf("Cannot start Query Analytics on MySQL %s: %s", setConfig["UUID"], err)
			m.logger.Error(errMsg)
			continue
		}
	}

	return nil // success
}

func (m *Manager) Stop() error {
	m.logger.Debug("Stop:call")
	defer m.logger.Debug("Stop:return")

	m.mux.Lock()
	defer m.mux.Unlock()

	for uuid := range m.analyzers {
		if err := m.stopAnalyzer(uuid); err != nil {
			m.logger.Error(err)
		}
	}

	m.logger.Info("Stopped")
	m.status.Update("qan", "Stopped")
	return nil
}

func (m *Manager) Status() map[string]string {
	m.mux.RLock()
	defer m.mux.RUnlock()
	status := m.status.All()
	for _, a := range m.analyzers {
		for k, v := range a.analyzer.Status() {
			status[k] = v
		}
	}
	return status
}

func (m *Manager) Handle(cmd *proto.Cmd) *proto.Reply {
	m.logger.Debug("Handle:call")
	defer m.logger.Debug("Handle:return")

	m.status.UpdateRe("qan", "Handling", cmd)
	defer m.status.Update("qan", "Running")

	m.mux.Lock()
	defer m.mux.Unlock()

	switch cmd.Cmd {
	case "StartTool":
		setConfig := map[string]string{}
		if err := json.Unmarshal(cmd.Data, &setConfig); err != nil {
			return cmd.Reply(nil, err)
		}
		uuid := setConfig["UUID"]
		if err := m.startAnalyzer(setConfig); err != nil {
			switch err {
			case ErrAlreadyRunning:
				// App reports this error message to user.
				err = fmt.Errorf("Query Analytics is already running on MySQL %s."+
					"To reconfigure or restart Query Analytics, stop then start it again.",
					uuid)
				return cmd.Reply(nil, err)
			default:
				return cmd.Reply(nil, err)
			}
		}

		// Write instance qan config to disk so agent runs qan on restart.
		if err := pct.Basedir.WriteConfig("qan-"+uuid, setConfig); err != nil {
			return cmd.Reply(nil, err)
		}

		a := m.analyzers[uuid]
		runningConfig := a.analyzer.Config()

		return cmd.Reply(runningConfig) // success
	case "StopTool":
		errs := []error{}
		uuid := string(cmd.Data)

		if err := m.stopAnalyzer(uuid); err != nil {
			switch err {
			case ErrNotRunning:
				// StopTool is idempotent so this isn't an error, but log it
				// in case user isn't expecting this.
				m.logger.Info("Not running Query Analytics on MySQL", uuid)
			default:
				errs = append(errs, err)
			}
		}

		// Remove qan-<uuid>.conf from disk so agent doesn't run qan on restart.
		if err := pct.Basedir.RemoveConfig("qan-" + uuid); err != nil {
			errs = append(errs, err)
		}

		// Remove local, cached instance info so if tool is started again we will
		// fetch the latest instance info.
		m.instanceRepo.Remove(uuid)

		return cmd.Reply(nil, errs...)
	case "GetConfig":
		config, errs := m.GetConfig()
		return cmd.Reply(config, errs...)
	default:
		return cmd.Reply(nil, pct.UnknownCmdError{Cmd: cmd.Cmd})
	}
}

func (m *Manager) GetConfig() ([]proto.AgentConfig, []error) {
	m.logger.Debug("GetConfig:call")
	defer m.logger.Debug("GetConfig:return")

	m.mux.RLock()
	defer m.mux.RUnlock()

	// Configs are always returned as array of AgentConfig resources.
	configs := []proto.AgentConfig{}
	for uuid, a := range m.analyzers {
		setConfigBytes, _ := json.Marshal(a.setConfig)
		runConfigBytes, err := json.Marshal(a.analyzer.Config())
		if err != nil {
			m.logger.Warn(err)
		}
		configs = append(configs, proto.AgentConfig{
			Service: "qan",
			UUID:    uuid,
			Set:     string(setConfigBytes),
			Running: string(runConfigBytes),
		})
	}
	return configs, nil
}

func (m *Manager) GetDefaults() map[string]interface{} {
	return map[string]interface{}{
		"CollectFrom":             DEFAULT_COLLECT_FROM,
		"Interval":                DEFAULT_INTERVAL,
		"LongQueryTime":           DEFAULT_LONG_QUERY_TIME,
		"MaxSlowLogSize":          DEFAULT_MAX_SLOW_LOG_SIZE,
		"RemoveOldSlowLogs":       DEFAULT_REMOVE_OLD_SLOW_LOGS,
		"ExampleQueries":          DEFAULT_EXAMPLE_QUERIES,
		"SlowLogVerbosity":        DEFAULT_SLOW_LOG_VERBOSITY,
		"RateLimit":               DEFAULT_RATE_LIMIT,
		"LogSlowAdminStatements":  DEFAULT_LOG_SLOW_ADMIN_STATEMENTS,
		"LogSlowSlaveStatemtents": DEFAULT_LOG_SLOW_SLAVE_STATEMENTS,
		"WorkerRunTime":           DEFAULT_WORKER_RUNTIME,
		"ReportLimit":             DEFAULT_REPORT_LIMIT,
	}
}

/////////////////////////////////////////////////////////////////////////////
// Implementation
/////////////////////////////////////////////////////////////////////////////

func (m *Manager) startAnalyzer(setConfig map[string]string) error {
	/*
		XXX Assume caller has locked m.mux.
	*/

	m.logger.Debug("startAnalyzer:call")
	defer m.logger.Debug("startAnalyzer:return")

	// Validate and transform the set config and into a running config.
	config, err := ValidateConfig(setConfig)
	if err != nil {
		return fmt.Errorf("invalid QAN config: %s", err)
	}

	// Check if an analyzer for this MySQL instance already exists.
	if _, ok := m.analyzers[config.UUID]; ok {
		return ErrAlreadyRunning
	}

	// Get the MySQL instance from repo.
	mysqlInstance, err := m.instanceRepo.Get(config.UUID, true) // true = cache (write to disk)
	if err != nil {
		return fmt.Errorf("cannot get MySQL instance %s: %s", config.UUID, err)
	}

	// Create a MySQL connection.
	mysqlConn := m.mysqlFactory.Make(mysqlInstance.DSN)

	// Add the MySQL DSN to the MySQL restart monitor. If MySQL restarts,
	// the analyzer will stop its worker and re-configure MySQL.
	restartChan := m.mrm.Add(mysqlInstance)

	// Make a chan on which the clock will tick at even intervals:
	// clock -> tickChan -> iter -> analyzer -> worker
	tickChan := make(chan time.Time, 1)
	m.clock.Add(tickChan, config.Interval, true)

	// Create and start a new analyzer. This should return immediately.
	// The analyzer will configure MySQL, start its iter, then run it worker
	// for each interval.
	analyzer := m.analyzerFactory.Make(
		config,
		"qan-analyzer-"+mysqlInstance.UUID[0:8],
		mysqlConn,
		restartChan,
		tickChan,
	)
	if err := analyzer.Start(); err != nil {
		return fmt.Errorf("Cannot start analyzer: %s", err)
	}

	// Save the new analyzer and its associated parts.
	m.analyzers[config.UUID] = AnalyzerInstance{
		setConfig:   setConfig,
		mysqlConn:   mysqlConn,
		restartChan: restartChan,
		tickChan:    tickChan,
		analyzer:    analyzer,
	}

	return nil // success
}

func (m *Manager) stopAnalyzer(uuid string) error {
	/*
		XXX Assume caller has locked m.mux.
	*/

	m.logger.Debug("stopAnalyzer:call")
	defer m.logger.Debug("stopAnalyzer:return")

	a, ok := m.analyzers[uuid]
	if !ok {
		m.logger.Debug("stopAnalyzer:not-running", uuid)
		return ErrNotRunning
	}

	m.status.Update("qan", fmt.Sprintf("Stopping %s", a.analyzer))
	m.logger.Info(fmt.Sprintf("Stopping %s", a.analyzer))

	// Stop ticking on this tickChan. Other services receiving ticks at the same
	// interval are not affected.
	m.clock.Remove(a.tickChan)

	// Stop watching this MySQL instance. Other services watching this MySQL
	// instance are not affected.
	m.mrm.Remove(uuid, a.restartChan)

	// Stop the analyzer. It stops its iter and worker and un-configures MySQL.
	if err := a.analyzer.Stop(); err != nil {
		return err
	}

	// Stop managing this analyzer.
	delete(m.analyzers, uuid)

	return nil // success
}
