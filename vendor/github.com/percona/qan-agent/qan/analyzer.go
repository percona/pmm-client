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
	"fmt"
	"log"
	"runtime/debug"
	"sync"
	"time"

	"github.com/percona/pmm/proto"
	pc "github.com/percona/pmm/proto/config"
	"github.com/percona/qan-agent/data"
	"github.com/percona/qan-agent/mysql"
	"github.com/percona/qan-agent/pct"
	"github.com/percona/qan-agent/ticker"
)

const MIN_SLOWLOG_ROTATION_SIZE = 4096

// A Worker gets queries, aggregates them, and returns a Result. Workers are ran
// by Analyzers. When ran, MySQL is presumed to be configured and ready.
type Worker interface {
	Setup(*Interval) error
	Run() (*Result, error)
	Stop() error
	Cleanup() error
	Status() map[string]string
	SetConfig(pc.QAN)
}

// An Analyzer runs a Worker at each Interval. Analyzers are responsible for
// MySQL: configuring, restarts, etc. The Worker is only ran when MySQL is
// configured and ready. Analyzers are also responsible for making Reports from
// the Results returned by Workers. The Worker determines the type of Analyzer:
// slowlog or perfschema. Analyzers are ran by the QAN Manager.
type Analyzer interface {
	Start() error
	Stop() error
	Status() map[string]string
	String() string
	Config() pc.QAN
	SetConfig(pc.QAN)
}

// An AnalyzerFactory makes an Analyzer, real or mock.
type AnalyzerFactory interface {
	Make(config pc.QAN, name string, mysqlConn mysql.Connector, restartChan chan proto.Instance, tickChan chan time.Time) Analyzer
}

// --------------------------------------------------------------------------

type RealAnalyzer struct {
	logger      *pct.Logger
	config      pc.QAN
	iter        IntervalIter
	mysqlConn   mysql.Connector
	restartChan chan proto.Instance
	worker      Worker
	clock       ticker.Manager
	spool       data.Spooler
	// --
	name                string
	mysqlConfiguredChan chan bool
	workerDoneChan      chan *Interval
	status              *pct.Status
	runSync             *pct.SyncChan
	configureMySQLSync  *pct.SyncChan
	running             bool
	mux                 *sync.RWMutex
	start               []string
	stop                []string
}

func NewRealAnalyzer(logger *pct.Logger, config pc.QAN, iter IntervalIter, mysqlConn mysql.Connector, restartChan chan proto.Instance, worker Worker, clock ticker.Manager, spool data.Spooler) *RealAnalyzer {
	name := logger.Service()
	a := &RealAnalyzer{
		logger:      logger,
		config:      config,
		iter:        iter,
		mysqlConn:   mysqlConn,
		restartChan: restartChan,
		worker:      worker,
		clock:       clock,
		spool:       spool,
		// --
		name:                name,
		mysqlConfiguredChan: make(chan bool, 1),
		workerDoneChan:      make(chan *Interval, 1),
		status:              pct.NewStatus([]string{name, name + "-last-interval", name + "-next-interval"}),
		runSync:             pct.NewSyncChan(),
		configureMySQLSync:  pct.NewSyncChan(),
		mux:                 &sync.RWMutex{},
	}
	return a
}

func (a *RealAnalyzer) String() string {
	return a.name
}

func (a *RealAnalyzer) Start() error {
	a.logger.Debug("Start:call")
	defer a.logger.Debug("Start:return")
	a.mux.Lock()
	defer a.mux.Unlock()
	if a.running {
		return nil
	}
	go a.run()
	a.running = true
	return nil
}

func (a *RealAnalyzer) Stop() error {
	a.logger.Debug("Stop:call")
	defer a.logger.Debug("Stop:return")
	a.mux.Lock()
	defer a.mux.Unlock()
	if !a.running {
		return nil
	}
	a.runSync.Stop()
	a.runSync.Wait()
	a.running = false
	return nil
}

func (a *RealAnalyzer) Status() map[string]string {
	a.mux.RLock()
	defer a.mux.RUnlock()
	if a.running {
		a.status.Update(a.name+"-next-interval", fmt.Sprintf("%.1fs", a.clock.ETA(a.iter.TickChan())))
	} else {
		a.status.Update(a.name+"-next-interval", "")
	}
	return a.status.Merge(a.worker.Status())
}

func (a *RealAnalyzer) Config() pc.QAN {
	return a.config
}

func (a *RealAnalyzer) SetConfig(config pc.QAN) {
	a.config = config
}

// --------------------------------------------------------------------------

// Disable Percona Server slow log rotation and handle internally using the
// max_slowlog_size value. The slow log worker must rotate slow logs by itself
// to ensure full and proper parsing across rotations.
func (a *RealAnalyzer) TakeOverPerconaServerRotation() error {
	a.logger.Debug("TakeOverPerconaServerRotation:call")
	defer a.logger.Debug("TakeOverPerconaServerRotation:return")

	maxSlowLogSize := int64(a.mysqlConn.GetGlobalVarNumber("max_slowlog_size"))
	if maxSlowLogSize == 0 {
		return nil
	}

	// Slow log rotation is only activated if max_slowlog_size >= 4096. PS doc
	// is not very clear, but testing confirmed this.
	// http://www.percona.com/doc/percona-server/5.6/flexibility/slowlog_rotation.html
	if maxSlowLogSize >= MIN_SLOWLOG_ROTATION_SIZE {
		a.logger.Info("Taking over Percona Server slow log rotation, max_slowlog_size:", maxSlowLogSize)
		a.config.MaxSlowLogSize = maxSlowLogSize
		disablePSrotation := []string{
			"SET GLOBAL max_slowlog_size = 0",
		}
		if err := a.mysqlConn.Exec(disablePSrotation); err != nil {
			return err
		}
	}

	return nil
}

func (a *RealAnalyzer) setMySQLConfig() error {
	a.logger.Debug("setMySQLConfig:call")
	defer a.logger.Debug("setMySQLConfig:return")

	// Get the current MySQL distro and version.
	distro := mysql.Distro(a.mysqlConn.GetGlobalVarString("version_comment"))
	version := a.mysqlConn.GetGlobalVarString("version")
	a.logger.Debug(fmt.Sprintf("MySQL distro '%s' version '%s'", distro, version))

	// Based on MySQL distro and version, generate the default/best sequence of
	// start and stop queries to configure MySQL.
	start, stop, err := GetMySQLConfig(a.config, distro, version)
	if err != nil {
		return err
	}
	a.config.Start = start
	a.config.Stop = stop

	return nil
}

func (a *RealAnalyzer) configureMySQL(action string, tryLimit int) {
	a.logger.Debug("configureMySQL:" + action + ":call")
	defer func() {
		if err := recover(); err != nil {
			a.logger.Error(a.name+":configureMySQL "+action+" crashed: ", err)
		}
		a.logger.Debug("configureMySQL:" + action + ":return")
	}()

	var lastErr error
	try := 0
	for (tryLimit == 0) || (try < tryLimit) {
		if lastErr != nil {
			a.logger.Warn(lastErr.Error())
			a.status.Update(a.name, lastErr.Error())
			a.mysqlConn.Close()
			lastErr = nil
		}

		select {
		case <-a.configureMySQLSync.StopChan:
			a.logger.Debug("configureMySQL:" + action + ":stop")
			a.configureMySQLSync.Done()
			return
		default:
		}

		// Wait after first try because something isn't working.
		try++
		if try > 1 {
			time.Sleep(5 * time.Second)
		}

		a.logger.Debug("configureMySQL:" + action + ":connect")

		if err := a.mysqlConn.Connect(); err != nil {
			lastErr = err
			continue
		}

		if err := a.TakeOverPerconaServerRotation(); err != nil {
			lastErr = fmt.Errorf("Cannot takeover slow log rotation: %s", err)
			continue
		}

		if err := a.setMySQLConfig(); err != nil {
			lastErr = fmt.Errorf("Cannot detect how to configure MySQL: %s", err)
			continue
		}
		a.worker.SetConfig(a.config)

		a.logger.Debug("configureMySQL:" + action + ":exec " + action + " queries")

		var queries []string
		switch action {
		case "start":
			queries = a.config.Start
		case "stop":
			queries = a.config.Stop
		default:
			panic("Invalid action in call to qan.Analyzer.configureMySQL: " + action)
		}
		if err := a.mysqlConn.Exec(queries); err != nil {
			lastErr = fmt.Errorf("Error configuring MySQL: %s", err)
			continue
		}

		// Success
		a.logger.Debug("configureMySQL:" + action + ":configured")
		a.mysqlConn.Close()
		break
	}

	select {
	case a.mysqlConfiguredChan <- true:
	case <-a.configureMySQLSync.StopChan:
		a.logger.Debug("configureMySQL:" + action + ":stop")
		a.configureMySQLSync.Done()
	}
}

func (a *RealAnalyzer) run() {
	a.logger.Debug("run:call")
	defer a.logger.Debug("run:return")

	mysqlConfigured := false
	go a.configureMySQL("start", 0) // try forever

	defer func() {
		a.logger.Info("Stopping")

		a.status.Update(a.name, "Stopping worker")
		a.worker.Stop()

		if mysqlConfigured {
			a.status.Update(a.name, "Stopping interval iter")
			a.iter.Stop()

			a.status.Update(a.name, "Stopping QAN on MySQL")
			a.configureMySQL("stop", 1) // try once
		} else {
			a.logger.Debug("run:stop configureMySQL goroutine")
			a.status.Update(a.name, "Stopping MySQL config (can take up to 10 seconds)")
			a.configureMySQLSync.Stop()
			a.configureMySQLSync.Wait()
		}

		if err := recover(); err != nil {
			a.logger.Error("QAN crashed: ", err)
			a.status.Update(a.name, "Crashed")
		} else {
			a.status.Update(a.name, "Stopped")
			a.logger.Info("Stopped")
		}

		a.runSync.Done()
	}()

	workerRunning := false
	lastTs := time.Time{}
	currentInterval := &Interval{}
	for {
		a.logger.Debug("run:idle")
		if mysqlConfigured {
			if workerRunning {
				a.status.Update(a.name, "Running")
			} else {
				a.status.Update(a.name, "Idle")
			}
		} else {
			a.status.Update(a.name, "Configuring MySQL")
		}

		select {
		case interval := <-a.iter.IntervalChan():
			if !mysqlConfigured {
				a.logger.Debug(fmt.Sprintf("run:interval:%d:skip (mysql not configured)", interval.Number))
				continue
			}

			if workerRunning {
				a.logger.Warn(fmt.Sprintf("Skipping interval '%s' because interval '%s' is still being parsed",
					interval, currentInterval))
				continue
			}

			a.status.Update(a.name, fmt.Sprintf("Starting interval '%s'", interval))
			a.logger.Debug(fmt.Sprintf("run:interval:%s", interval))
			currentInterval = interval

			// Run the worker, timing it, make a report from its results, spool
			// the report. When done the interval is returned on workerDoneChan.
			go a.runWorker(interval)
			workerRunning = true
		case interval := <-a.workerDoneChan:
			a.logger.Debug("run:worker:done")
			a.status.Update(a.name, fmt.Sprintf("Cleaning up after interval '%s'", interval))
			workerRunning = false

			if interval.StartTime.After(lastTs) {
				t0 := interval.StartTime.Format("2006-01-02 15:04:05")
				if a.config.CollectFrom == "slowlog" {
					t1 := interval.StopTime.Format("15:04:05 MST")
					a.status.Update(a.name+"-last-interval", fmt.Sprintf("%s to %s", t0, t1))
				} else {
					a.status.Update(a.name+"-last-interval", fmt.Sprintf("%s", t0))
				}
				lastTs = interval.StartTime
			}
		case mysqlConfigured = <-a.mysqlConfiguredChan:
			a.logger.Debug("run:mysql:configured")
			// Start the IntervalIter once MySQL has been configured.
			// This avoids no data or partial data, e.g. slow log verbosity
			// not set yet.
			a.iter.Start()

			// If the next interval is more than 1 minute in the future,
			// simulate a clock tick now to start the iter early. For example,
			// if the interval is 5m and it's currently 01:00, the next interval
			// starts in 4m and stops in 9m, so data won't be reported for about
			// 10m. Instead, tick now so start interval=01:00 and end interval
			// =05:00 and data is reported in about 6m.
			tickChan := a.iter.TickChan()
			t := a.clock.ETA(tickChan)
			if t > 60 {
				began := ticker.Began(a.config.Interval, uint(time.Now().UTC().Unix()))
				a.logger.Info("First interval began at", began)
				tickChan <- began
			} else {
				a.logger.Info(fmt.Sprintf("First interval begins in %.1f seconds", t))
			}
		case <-a.restartChan:
			a.logger.Debug("run:mysql:restart")
			// If MySQL is not configured, then configureMySQL() should already
			// be running, trying to configure it. Else, we need to run
			// configureMySQL again.
			if mysqlConfigured {
				mysqlConfigured = false
				a.iter.Stop()
				go a.configureMySQL("start", 0) // try forever
			}
		case <-a.runSync.StopChan:
			a.logger.Debug("run:stop")
			return
		}
	}
}

func (a *RealAnalyzer) runWorker(interval *Interval) {
	a.logger.Debug(fmt.Sprintf("runWorker:call:%d", interval.Number))
	defer func() {
		if err := recover(); err != nil {
			errMsg := fmt.Sprintf(a.name+"-worker crashed: '%s': %s", interval, err)
			log.Println(errMsg)
			debug.PrintStack()
			a.logger.Error(errMsg)
		}
		a.workerDoneChan <- interval
		a.logger.Debug(fmt.Sprintf("runWorker:return:%d", interval.Number))
	}()

	// Let worker do whatever it needs before it starts processing
	// the interval. This mostly makes testing easier.
	if err := a.worker.Setup(interval); err != nil {
		a.logger.Warn(err)
		return
	}

	// Let worker do whatever it needs after processing the interval.
	// This mostly maske testing easier.
	defer func() {
		if err := a.worker.Cleanup(); err != nil {
			a.logger.Warn(err)
		}
	}()

	// Run the worker to process the interval.
	t0 := time.Now()
	result, err := a.worker.Run()
	t1 := time.Now()
	if err != nil {
		a.logger.Error(err)
		return
	}
	if result == nil {
		if a.config.CollectFrom == "slowlog" {
			// This shouldn't happen. If it does, the slow log worker has a bug
			// because it should have returned an error above.
			a.logger.Error("Nil result", interval)
		}
		return
	}
	result.RunTime = t1.Sub(t0).Seconds()

	// Translate the results into a report and spool.
	// NOTE: "qan" here is correct; do not use a.name.
	report := MakeReport(a.config, interval, result)
	if err := a.spool.Write("qan", report); err != nil {
		a.logger.Warn("Lost report:", err)
	}
}
