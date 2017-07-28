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

package qan_test

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"time"

	. "github.com/go-test/test"
	"github.com/percona/pmm/proto"
	pc "github.com/percona/pmm/proto/config"
	"github.com/percona/qan-agent/instance"
	"github.com/percona/qan-agent/pct"
	"github.com/percona/qan-agent/qan"
	"github.com/percona/qan-agent/test"
	"github.com/percona/qan-agent/test/mock"
	. "gopkg.in/check.v1"
)

type ManagerTestSuite struct {
	nullmysql    *mock.NullMySQL
	mrmsMonitor  *mock.MrmsMonitor
	logChan      chan proto.LogEntry
	logger       *pct.Logger
	intervalChan chan *qan.Interval
	dataChan     chan interface{}
	spool        *mock.Spooler
	//workerFactory qan.WorkerFactory
	clock         *mock.Clock
	tmpDir        string
	configDir     string
	im            *instance.Repo
	api           *mock.API
	mysqlUUID     string
	mysqlInstance proto.Instance
}

var _ = Suite(&ManagerTestSuite{})

func (s *ManagerTestSuite) SetUpSuite(t *C) {

	s.nullmysql = mock.NewNullMySQL()
	s.mrmsMonitor = mock.NewMrmsMonitor()

	s.logChan = make(chan proto.LogEntry, 1000)
	s.logger = pct.NewLogger(s.logChan, "qan-test")

	s.dataChan = make(chan interface{}, 2)
	s.spool = mock.NewSpooler(s.dataChan)

	var err error
	s.tmpDir, err = ioutil.TempDir("/tmp", "agent-test")
	t.Assert(err, IsNil)

	links := map[string]string{
		"agents":    "/agents",
		"instances": "/instances",
	}
	s.api = mock.NewAPI("localhost", "http://localhost", "212", links)

	if err := pct.Basedir.Init(s.tmpDir); err != nil {
		t.Fatal(err)
	}
	s.configDir = pct.Basedir.Dir("config")
	s.im = instance.NewRepo(pct.NewLogger(s.logChan, "manager-test"), s.configDir, s.api)

	s.mysqlUUID = "3130000000000000"
	s.mysqlInstance = proto.Instance{
		Subsystem: "mysql",
		UUID:      s.mysqlUUID,
		Name:      "db01",
		DSN:       "user:pass@tcp(localhost)/",
	}

	err = s.im.Init()
	t.Assert(err, IsNil)
}

func (s *ManagerTestSuite) SetUpTest(t *C) {
	s.nullmysql.Reset()
	s.clock = mock.NewClock()
	if err := test.ClearDir(pct.Basedir.Dir("config"), "*"); err != nil {
		t.Fatal(err)
	}
	for _, in := range s.im.List("mysql") {
		s.im.Remove(in.UUID)
	}
	err := s.im.Add(s.mysqlInstance, true)
	t.Assert(err, IsNil)
}

func (s *ManagerTestSuite) TearDownSuite(t *C) {
	if err := os.RemoveAll(s.tmpDir); err != nil {
		t.Error(err)
	}
}

// --------------------------------------------------------------------------

func (s *ManagerTestSuite) TestStarNoConfig(t *C) {
	// Make a qan.Manager with mock factories.
	mockConnFactory := &mock.ConnectionFactory{Conn: s.nullmysql}
	a := mock.NewQanAnalyzer("qan-analizer-1")
	f := mock.NewQanAnalyzerFactory(a)
	m := qan.NewManager(s.logger, s.clock, s.im, s.mrmsMonitor, mockConnFactory, f)
	t.Assert(m, NotNil)

	// qan.Manager should be able to start without a qan.conf, i.e. no analyzer.
	err := m.Start()
	t.Check(err, IsNil)

	// Wait for qan.Manager.Start() to finish.
	test.WaitStatus(1, m, "qan", "Running")

	// No analyzer is configured, so the mock analyzer should not be started.
	select {
	case <-a.StartChan:
		t.Error("Analyzer.Start() called")
	default:
	}

	// And the mock analyzer's status should not be reported.
	status := m.Status()
	t.Check(status["qan"], Equals, "Running")

	// Stop the manager.
	err = m.Stop()
	t.Assert(err, IsNil)

	// No analyzer is configured, so the mock analyzer should not be stop.
	select {
	case <-a.StartChan:
		t.Error("Analyzer.Stop() called")
	default:
	}
}

func (s *ManagerTestSuite) TestStartWithConfig(t *C) {
	mi2 := s.mysqlInstance
	mi2.UUID = "2220000000000000"
	mi2.Name = "db02"
	err := s.im.Add(mi2, false)
	t.Assert(err, IsNil)
	defer s.im.Remove(mi2.UUID)

	// Get MYySQL instances
	mysqlInstances := s.im.List("mysql")
	t.Assert(len(mysqlInstances), Equals, 2)

	// Make a qan.Manager with mock factories.
	a1 := mock.NewQanAnalyzer(fmt.Sprintf("qan-analyzer-%s", mysqlInstances[0].Name))
	a2 := mock.NewQanAnalyzer(fmt.Sprintf("qan-analyzer-%s", mysqlInstances[1].Name))
	f := mock.NewQanAnalyzerFactory(a1, a2)
	mockConnFactory := &mock.ConnectionFactory{Conn: s.nullmysql}
	m := qan.NewManager(s.logger, s.clock, s.im, s.mrmsMonitor, mockConnFactory, f)
	t.Assert(m, NotNil)
	configs := make([]pc.QAN, 0)
	for i, analyzerType := range []string{"slowlog", "perfschema"} {
		// We have two analyzerTypes and two MySQL instances in fixture, lets re-use the index
		// as we only need one of each analizer type and they need to be different instances.
		mysqlInstance := mysqlInstances[i]
		// Write a realistic qan.conf config to disk.
		config := pc.QAN{
			UUID:              mysqlInstance.UUID,
			CollectFrom:       analyzerType,
			Interval:          300,
			WorkerRunTime:     600,
			MaxSlowLogSize:    100,   // specify optional args
			RemoveOldSlowLogs: "yes", // specify optional args
			ExampleQueries:    "yes", // specify optional args
			ReportLimit:       200,   // specify optional args
			Start: []string{
				"SET GLOBAL slow_query_log=OFF",
				"SET GLOBAL long_query_time=0.456",
				"SET GLOBAL slow_query_log=ON",
			},
			Stop: []string{
				"SET GLOBAL slow_query_log=OFF",
				"SET GLOBAL long_query_time=10",
			},
		}
		err := pct.Basedir.WriteConfig("qan-"+mysqlInstance.UUID, &config)
		t.Assert(err, IsNil)
		configs = append(configs, config)
	}
	// qan.Start() reads qan configs from disk and starts an analyzer for each one.
	err = m.Start()
	t.Check(err, IsNil)

	// Wait until qan.Start() calls analyzer.Start().
	if !test.WaitState(a1.StartChan) {
		t.Fatal("Timeout waiting for <-a1.StartChan")
	}
	if !test.WaitState(a2.StartChan) {
		t.Fatal("Timeout waiting for <-a2.StartChan")
	}

	// After starting, the manager's status should be Running and the analyzer's
	// status should be reported too.
	status := m.Status()
	t.Check(status["qan"], Equals, "Running")
	t.Check(status["qan-analyzer"], Equals, "ok")

	// Check the args passed by the manager to the analyzer factory.
	if len(f.Args) == 0 {
		t.Error("len(f.Args) == 0, expected 2")
	} else {
		t.Check(f.Args, HasLen, 2)

		argConfigs := []pc.QAN{f.Args[0].Config, f.Args[1].Config}
		t.Check(argConfigs, DeepEquals, configs)
		t.Check(f.Args[0].Name, Equals, "qan-analyzer-22200000")
		t.Check(f.Args[1].Name, Equals, "qan-analyzer-31300000")
	}

	// qan.Stop() stops the analyzer and leaves qan.conf on disk.
	err = m.Stop()
	t.Assert(err, IsNil)

	// Wait until qan.Stop() calls analyzer.Stop().
	if !test.WaitState(a1.StopChan) {
		t.Fatal("Timeout waiting for <-a.StopChan")
	}

	// Wait until qan.Stop() calls analyzer.Stop().
	if !test.WaitState(a2.StopChan) {
		t.Fatal("Timeout waiting for <-a.StopChan")
	}

	// qan.conf still exists after qan.Stop().
	for _, mysqlInstance := range s.im.List("mysql") {
		t.Check(test.FileExists(pct.Basedir.ConfigFile("qan-"+mysqlInstance.UUID)), Equals, true)
	}

	// The analyzer is no longer reported in the status because it was stopped
	// and removed when the manager was stopped.
	status = m.Status()
	t.Check(status["qan"], Equals, "Stopped")
}

func (s *ManagerTestSuite) TestStart2RemoteQAN(t *C) {
	mi2 := s.mysqlInstance
	mi2.UUID = "2220000000000000"
	mi2.Name = "db02"
	err := s.im.Add(mi2, false)
	t.Assert(err, IsNil)
	defer s.im.Remove(mi2.UUID)

	// Get MYySQL instances
	mysqlInstances := s.im.List("mysql")
	t.Assert(len(mysqlInstances), Equals, 2)

	// Make a qan.Manager with mock factories.
	a1 := mock.NewQanAnalyzer(fmt.Sprintf("qan-analyzer-%s", mysqlInstances[0].Name))
	a2 := mock.NewQanAnalyzer(fmt.Sprintf("qan-analyzer-%s", mysqlInstances[1].Name))
	f := mock.NewQanAnalyzerFactory(a1, a2)
	mockConnFactory := &mock.ConnectionFactory{Conn: s.nullmysql}
	m := qan.NewManager(s.logger, s.clock, s.im, s.mrmsMonitor, mockConnFactory, f)
	t.Assert(m, NotNil)
	configs := make([]pc.QAN, 0)
	for _, mysqlInstance := range mysqlInstances {
		// Write a realistic qan.conf config to disk.
		config := pc.QAN{
			UUID:              mysqlInstance.UUID,
			CollectFrom:       "perfschema",
			Interval:          300,
			WorkerRunTime:     600,
			MaxSlowLogSize:    100,   // specify optional args
			RemoveOldSlowLogs: "yes", // specify optional args
			ExampleQueries:    "yes", // specify optional args
			ReportLimit:       200,   // specify optional args
			Start: []string{
				"SET GLOBAL slow_query_log=OFF",
				"SET GLOBAL long_query_time=0.456",
				"SET GLOBAL slow_query_log=ON",
			},
			Stop: []string{
				"SET GLOBAL slow_query_log=OFF",
				"SET GLOBAL long_query_time=10",
			},
		}
		err := pct.Basedir.WriteConfig("qan-"+mysqlInstance.UUID, &config)
		t.Assert(err, IsNil)
		configs = append(configs, config)
	}
	// qan.Start() reads qan configs from disk and starts an analyzer for each one.
	err = m.Start()
	t.Check(err, IsNil)

	// Wait until qan.Start() calls analyzer.Start().
	if !test.WaitState(a1.StartChan) {
		t.Fatal("Timeout waiting for <-a1.StartChan")
	}
	if !test.WaitState(a2.StartChan) {
		t.Fatal("Timeout waiting for <-a2.StartChan")
	}

	// After starting, the manager's status should be Running and the analyzer's
	// status should be reported too.
	status := m.Status()
	t.Check(status["qan"], Equals, "Running")
	t.Check(status["qan-analyzer"], Equals, "ok")

	// Check the args passed by the manager to the analyzer factory.
	if len(f.Args) == 0 {
		t.Error("len(f.Args) == 0, expected 2")
	} else {
		t.Check(f.Args, HasLen, 2)

		argConfigs := []pc.QAN{f.Args[0].Config, f.Args[1].Config}
		t.Check(argConfigs, DeepEquals, configs)
		t.Check(f.Args[0].Name, Equals, "qan-analyzer-22200000")
		t.Check(f.Args[1].Name, Equals, "qan-analyzer-31300000")
	}

	// qan.Stop() stops the analyzer and leaves qan.conf on disk.
	err = m.Stop()
	t.Assert(err, IsNil)

	// Wait until qan.Stop() calls analyzer.Stop().
	if !test.WaitState(a1.StopChan) {
		t.Fatal("Timeout waiting for <-a.StopChan")
	}

	// Wait until qan.Stop() calls analyzer.Stop().
	if !test.WaitState(a2.StopChan) {
		t.Fatal("Timeout waiting for <-a.StopChan")
	}

	// qan.conf still exists after qan.Stop().
	for _, mysqlInstance := range s.im.List("mysql") {
		t.Check(test.FileExists(pct.Basedir.ConfigFile("qan-"+mysqlInstance.UUID)), Equals, true)
	}

	// The analyzer is no longer reported in the status because it was stopped
	// and removed when the manager was stopped.
	status = m.Status()
	t.Check(status["qan"], Equals, "Stopped")

}

func (s *ManagerTestSuite) TestGetConfig(t *C) {
	t.Skip("fixme")

	// Make a qan.Manager with mock factories.
	mockConnFactory := &mock.ConnectionFactory{Conn: s.nullmysql}
	a := mock.NewQanAnalyzer("qan-analizer-1")
	f := mock.NewQanAnalyzerFactory(a)
	m := qan.NewManager(s.logger, s.clock, s.im, s.mrmsMonitor, mockConnFactory, f)
	t.Assert(m, NotNil)

	mysqlInstances := s.im.List("mysql")
	t.Assert(len(mysqlInstances), Equals, 1)
	mysqlUUID := mysqlInstances[0].UUID

	// Write a realistic qan.conf config to disk.
	config := pc.QAN{
		UUID:          mysqlUUID,
		CollectFrom:   "slowlog",
		Interval:      300,
		WorkerRunTime: 600,
		Start: []string{
			"SET GLOBAL slow_query_log=OFF",
			"SET GLOBAL long_query_time=0.456",
			"SET GLOBAL slow_query_log=ON",
		},
		Stop: []string{
			"SET GLOBAL slow_query_log=OFF",
			"SET GLOBAL long_query_time=10",
		},
	}
	err := pct.Basedir.WriteConfig("qan-"+mysqlUUID, &config)
	t.Assert(err, IsNil)

	configWithDefaults := config
	configWithDefaults.MaxSlowLogSize = qan.DEFAULT_MAX_SLOW_LOG_SIZE
	configWithDefaults.RemoveOldSlowLogs = qan.DEFAULT_REMOVE_OLD_SLOW_LOGS
	configWithDefaults.ExampleQueries = qan.DEFAULT_EXAMPLE_QUERIES
	configWithDefaults.ReportLimit = qan.DEFAULT_REPORT_LIMIT
	qanConfig, err := json.Marshal(configWithDefaults)
	t.Assert(err, IsNil)

	// Start the manager and analyzer.
	err = m.Start()
	t.Check(err, IsNil)
	test.WaitStatus(1, m, "qan", "Running")

	// Get the manager config which should be just the analyzer config.
	got, errs := m.GetConfig()
	t.Assert(errs, HasLen, 0)
	t.Assert(got, HasLen, 1)
	expect := []proto.AgentConfig{
		{
			Service: "qan",
			UUID:    mysqlUUID,
			Set:     string(qanConfig),
		},
	}
	if same, diff := IsDeeply(got, expect); !same {
		Dump(got)
		t.Error(diff)
	}

	// Stop the manager.
	err = m.Stop()
	t.Assert(err, IsNil)
}

func (s *ManagerTestSuite) TestValidateConfig(t *C) {
	mysqlInstances := s.im.List("mysql")
	t.Assert(len(mysqlInstances), Equals, 1)
	mysqlUUID := mysqlInstances[0].UUID

	config := pc.QAN{
		UUID: mysqlUUID,
		Start: []string{
			"SET GLOBAL slow_query_log=OFF",
			"SET GLOBAL long_query_time=0.123",
			"SET GLOBAL slow_query_log=ON",
		},
		Stop: []string{
			"SET GLOBAL slow_query_log=OFF",
			"SET GLOBAL long_query_time=10",
		},
		Interval:          300,        // 5 min
		MaxSlowLogSize:    1073741824, // 1 GiB
		RemoveOldSlowLogs: "true",
		ExampleQueries:    "true",
		WorkerRunTime:     600, // 10 min
		CollectFrom:       "slowlog",
	}
	err := qan.ValidateConfig(&config)
	t.Check(err, IsNil)
}

func (s *ManagerTestSuite) TestStartTool(t *C) {
	// Make and start a qan.Manager with mock factories, no analyzer yet.
	mockConnFactory := &mock.ConnectionFactory{Conn: s.nullmysql}
	a := mock.NewQanAnalyzer("qan-analizer-1")
	f := mock.NewQanAnalyzerFactory(a)
	m := qan.NewManager(s.logger, s.clock, s.im, s.mrmsMonitor, mockConnFactory, f)
	t.Assert(m, NotNil)
	err := m.Start()
	t.Check(err, IsNil)
	test.WaitStatus(1, m, "qan", "Running")

	mysqlInstances := s.im.List("mysql")
	t.Assert(len(mysqlInstances), Equals, 1)
	mysqlUUID := mysqlInstances[0].UUID

	// Create the qan config.
	config := &pc.QAN{
		UUID: mysqlUUID,
		Start: []string{
			"SET GLOBAL slow_query_log=OFF",
			"SET GLOBAL long_query_time=0.123",
			"SET GLOBAL slow_query_log=ON",
		},
		Stop: []string{
			"SET GLOBAL slow_query_log=OFF",
			"SET GLOBAL long_query_time=10",
		},
		Interval:          300,        // 5 min
		MaxSlowLogSize:    1073741824, // 1 GiB
		RemoveOldSlowLogs: "true",
		ExampleQueries:    "true",
		WorkerRunTime:     600, // 10 min
		CollectFrom:       "slowlog",
	}

	// Send a StartTool cmd with the qan config to start an analyzer.
	now := time.Now()
	qanConfig, _ := json.Marshal(config)
	cmd := &proto.Cmd{
		User:      "daniel",
		Ts:        now,
		AgentUUID: "123",
		Service:   "qan",
		Cmd:       "StartTool",
		Data:      qanConfig,
	}
	reply := m.Handle(cmd)
	t.Assert(reply.Error, Equals, "")

	// The manager writes the qan config to disk.
	data, err := ioutil.ReadFile(pct.Basedir.ConfigFile("qan-" + mysqlUUID))
	t.Check(err, IsNil)
	gotConfig := &pc.QAN{}
	err = json.Unmarshal(data, gotConfig)
	t.Check(err, IsNil)
	if same, diff := IsDeeply(gotConfig, config); !same {
		Dump(gotConfig)
		t.Error(diff)
	}

	// Now the manager and analyzer should be running.
	status := m.Status()
	t.Check(status["qan"], Equals, "Running")
	t.Check(status["qan-analyzer"], Equals, "ok")

	// Try to start the same analyzer again. It results in an error because
	// double tooling is not allowed.
	reply = m.Handle(cmd)
	t.Check(reply.Error, Equals, a.String()+" service is running")

	// Send a StopTool cmd to stop the analyzer.
	now = time.Now()
	cmd = &proto.Cmd{
		User:      "daniel",
		Ts:        now,
		AgentUUID: "123",
		Service:   "qan",
		Cmd:       "StopTool",
		Data:      []byte(mysqlUUID),
	}
	reply = m.Handle(cmd)
	t.Assert(reply.Error, Equals, "")

	// Now the manager is still running, but the analyzer is not.
	status = m.Status()
	t.Check(status["qan"], Equals, "Running")

	// And the manager has removed the qan config from disk so next time
	// the agent starts the analyzer is not started.
	t.Check(test.FileExists(pct.Basedir.ConfigFile("qan-"+mysqlUUID)), Equals, false)

	// StopTool should be idempotent, so send it again and expect no error.
	reply = m.Handle(cmd)
	t.Assert(reply.Error, Equals, "")

	// Stop the manager.
	err = m.Stop()
	t.Assert(err, IsNil)
}

func (s *ManagerTestSuite) TestBadCmd(t *C) {
	mockConnFactory := &mock.ConnectionFactory{Conn: s.nullmysql}
	a := mock.NewQanAnalyzer("qan-analizer-1")
	f := mock.NewQanAnalyzerFactory(a)
	m := qan.NewManager(s.logger, s.clock, s.im, s.mrmsMonitor, mockConnFactory, f)
	t.Assert(m, NotNil)
	err := m.Start()
	t.Check(err, IsNil)
	defer m.Stop()
	test.WaitStatus(1, m, "qan", "Running")
	cmd := &proto.Cmd{
		User:      "daniel",
		Ts:        time.Now(),
		AgentUUID: "123",
		Service:   "qan",
		Cmd:       "foo", // bad cmd
	}
	reply := m.Handle(cmd)
	t.Assert(reply.Error, Equals, "Unknown command: foo")
}
