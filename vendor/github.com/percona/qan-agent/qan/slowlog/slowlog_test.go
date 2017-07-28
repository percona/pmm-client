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

package slowlog_test

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	. "github.com/go-test/test"
	"github.com/percona/go-mysql/event"
	"github.com/percona/go-mysql/log"
	"github.com/percona/pmm/proto"
	pc "github.com/percona/pmm/proto/config"
	"github.com/percona/qan-agent/mysql"
	"github.com/percona/qan-agent/pct"
	"github.com/percona/qan-agent/qan"
	"github.com/percona/qan-agent/qan/slowlog"
	"github.com/percona/qan-agent/test"
	"github.com/percona/qan-agent/test/mock"
	. "gopkg.in/check.v1"
)

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) { TestingT(t) }

var inputDir = RootDir() + "/agent/test/slow-logs/"
var outputDir = RootDir() + "/agent/test/qan/"

type ByQueryId []*event.Class

func (a ByQueryId) Len() int      { return len(a) }
func (a ByQueryId) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a ByQueryId) Less(i, j int) bool {
	return a[i].Id > a[j].Id
}

type WorkerTestSuite struct {
	logChan       chan *proto.LogEntry
	logger        *pct.Logger
	now           time.Time
	mysqlInstance proto.Instance
	config        pc.QAN
	mysqlConn     mysql.Connector
	worker        *slowlog.Worker
	nullmysql     *mock.NullMySQL
	dsn           string
}

var _ = Suite(&WorkerTestSuite{})

func (s *WorkerTestSuite) SetUpSuite(t *C) {
	s.dsn = os.Getenv("PCT_TEST_MYSQL_DSN")
	s.logChan = make(chan *proto.LogEntry, 100)
	s.logger = pct.NewLogger(s.logChan, "qan-worker")
	s.now = time.Now()
	s.mysqlInstance = proto.Instance{UUID: "1", Name: "mysql1"}
	s.config = pc.QAN{
		UUID: s.mysqlInstance.UUID,
		Start: []string{
			"SET GLOBAL slow_query_log=OFF",
			"SET GLOBAL long_query_time=0.123",
			"SET GLOBAL slow_query_log=ON",
		},
		Stop: []string{
			"SET GLOBAL slow_query_log=OFF",
			"SET GLOBAL long_query_time=10",
		},
		Interval:          60,         // 1 min
		MaxSlowLogSize:    1073741824, // 1 GiB
		RemoveOldSlowLogs: "true",
		ExampleQueries:    "true",
		WorkerRunTime:     60, // 1 min
		CollectFrom:       "slowlog",
	}
	s.nullmysql = mock.NewNullMySQL()
}

func (s *WorkerTestSuite) SetUpTest(t *C) {
	s.nullmysql.Reset()
}

func (s *WorkerTestSuite) RunWorker(config pc.QAN, mysqlConn mysql.Connector, i *qan.Interval) (*qan.Result, error) {
	w := slowlog.NewWorker(s.logger, config, mysqlConn)
	w.ZeroRunTime = true
	w.Setup(i)
	err, res := w.Run()
	w.Cleanup()
	return err, res
}

// -------------------------------------------------------------------------

func (s *WorkerTestSuite) TestWorkerWithAnotherTZ(t *C) {
	// Use results for TestWorkerSlow001 but simulate tz = UTC+1
	mysqlConn := mock.NewNullMySQL()
	mysqlConn.SystemTzOffsetHours = 1
	defer func() { mysqlConn.SystemTzOffsetHours = 0 }()

	i := &qan.Interval{
		Number:      1,
		StartTime:   s.now,
		StopTime:    s.now.Add(1 * time.Minute),
		Filename:    inputDir + "slow001.log",
		StartOffset: 0,
		EndOffset:   524,
	}
	got, err := s.RunWorker(s.config, mysqlConn, i)
	t.Check(err, IsNil)
	expect := &qan.Result{}
	test.LoadReport(outputDir+"slow001.json", expect)

	expect.Class[0].Example.Ts = "2007-10-15 22:45:10"
	expect.Class[1].Example.Ts = "2007-10-15 22:43:52"

	sort.Sort(ByQueryId(got.Class))
	sort.Sort(ByQueryId(expect.Class))
	if ok, diff := IsDeeply(got, expect); !ok {
		Dump(got)
		t.Error(diff)
	}
}

func (s *WorkerTestSuite) TestWorkerSlow001(t *C) {
	i := &qan.Interval{
		Number:      1,
		StartTime:   s.now,
		StopTime:    s.now.Add(1 * time.Minute),
		Filename:    inputDir + "slow001.log",
		StartOffset: 0,
		EndOffset:   524,
	}
	got, err := s.RunWorker(s.config, mock.NewNullMySQL(), i)
	t.Check(err, IsNil)
	expect := &qan.Result{}
	test.LoadReport(outputDir+"slow001.json", expect)
	sort.Sort(ByQueryId(got.Class))
	sort.Sort(ByQueryId(expect.Class))
	if ok, diff := IsDeeply(got, expect); !ok {
		Dump(got)
		t.Error(diff)
	}
}

func (s *WorkerTestSuite) TestWorkerSlow001NoExamples(t *C) {
	i := &qan.Interval{
		Number:      99,
		StartTime:   s.now,
		StopTime:    s.now.Add(1 * time.Minute),
		Filename:    inputDir + "slow001.log",
		StartOffset: 0,
		EndOffset:   524,
	}
	config := s.config
	config.ExampleQueries = "false"
	got, err := s.RunWorker(config, mock.NewNullMySQL(), i)
	t.Check(err, IsNil)
	expect := &qan.Result{}
	if err := test.LoadReport(outputDir+"slow001-no-examples.json", expect); err != nil {
		t.Fatal(err)
	}
	sort.Sort(ByQueryId(got.Class))
	sort.Sort(ByQueryId(expect.Class))
	if same, diff := IsDeeply(got, expect); !same {
		Dump(got)
		t.Error(diff)
	}
}

func (s *WorkerTestSuite) TestWorkerSlow001Half(t *C) {
	// This tests that the worker will stop processing events before
	// the end of the slow log file.  358 is the last byte of the first
	// (of 2) events.
	i := &qan.Interval{
		Number:      1,
		StartTime:   s.now,
		StopTime:    s.now.Add(1 * time.Minute),
		Filename:    inputDir + "slow001.log",
		StartOffset: 0,
		EndOffset:   358,
	}
	got, err := s.RunWorker(s.config, mock.NewNullMySQL(), i)
	t.Check(err, IsNil)
	expect := &qan.Result{}
	if err := test.LoadReport(outputDir+"slow001-half.json", expect); err != nil {
		t.Fatal(err)
	}
	sort.Sort(ByQueryId(got.Class))
	sort.Sort(ByQueryId(expect.Class))
	if ok, diff := IsDeeply(got, expect); !ok {
		Dump(got)
		t.Error(diff)
	}
}

func (s *WorkerTestSuite) TestWorkerSlow001Resume(t *C) {
	// This tests that the worker will resume processing events from
	// somewhere in the slow log file.  359 is the first byte of the
	// second (of 2) events.
	i := &qan.Interval{
		Number:      2,
		StartTime:   s.now,
		StopTime:    s.now.Add(1 * time.Minute),
		Filename:    inputDir + "slow001.log",
		StartOffset: 359,
		EndOffset:   524,
	}
	got, err := s.RunWorker(s.config, mock.NewNullMySQL(), i)
	t.Check(err, IsNil)
	expect := &qan.Result{}
	test.LoadReport(outputDir+"slow001-resume.json", expect)
	sort.Sort(ByQueryId(got.Class))
	sort.Sort(ByQueryId(expect.Class))
	if ok, diff := IsDeeply(got, expect); !ok {
		Dump(got)
		t.Error(diff)
	}
}

func (s *WorkerTestSuite) TestWorkerSlow011(t *C) {
	// Percona Server rate limit
	i := &qan.Interval{
		Number:      1,
		StartTime:   s.now,
		StopTime:    s.now.Add(1 * time.Minute),
		Filename:    inputDir + "slow011.log",
		StartOffset: 0,
		EndOffset:   3000,
	}
	got, err := s.RunWorker(s.config, mock.NewNullMySQL(), i)
	t.Check(err, IsNil)
	expect := &qan.Result{}
	if err := test.LoadReport(outputDir+"slow011.json", expect); err != nil {
		t.Fatal(err)
	}
	sort.Sort(ByQueryId(got.Class))
	sort.Sort(ByQueryId(expect.Class))
	if same, diff := IsDeeply(got, expect); !same {
		Dump(got)
		t.Error(diff)
	}
}

func (s *WorkerTestSuite) TestRotateAndRemoveSlowLog(t *C) {
	// Clean up files that may interfere with test.
	slowlogFile := "slow006.log"
	files, _ := filepath.Glob("/tmp/" + slowlogFile + "-[0-9]*")
	for _, file := range files {
		os.Remove(file)
	}

	/**
	 * slow006.log is 2200 bytes large.  Rotation happens when the worker
	 * sees interval.EndOffset >= MaxSlowLogSize.  So we'll use these
	 * intervals:
	 *      0 -  736
	 *    736 - 1833
	 *   1833 - 2200
	 * and set MaxSlowLogSize=1000 which should make the worker rotate the log
	 * after the 2nd interval.  When the worker rotates log, it 1) renames log
	 * to NAME-TS where NAME is the original name and TS is the current Unix
	 * timestamp (UTC); and 2) it sets interval.StopOff = file size of NAME-TS
	 * to finish parsing the log. Therefore, results for 2nd interval should
	 * include our 3rd interval. -- The worker also calls Start and Stop so the
	 * nullmysql conn should record the queries being set.
	 */

	// See TestStartService() for description of these startup tasks.
	config := pc.QAN{
		UUID:              s.mysqlInstance.UUID,
		Interval:          300,
		MaxSlowLogSize:    1000,   // <-- HERE
		RemoveOldSlowLogs: "true", // <-- HERE too
		ExampleQueries:    "false",
		WorkerRunTime:     600,
		Start: []string{
			"-- start",
		},
		Stop: []string{
			"-- stop",
		},
		CollectFrom: "slowlog",
	}
	w := slowlog.NewWorker(s.logger, config, s.nullmysql)

	// Make copy of slow log because test will mv/rename it.
	cp := exec.Command("cp", inputDir+slowlogFile, "/tmp/"+slowlogFile)
	cp.Run()

	// First interval: 0 - 736
	now := time.Now()
	i1 := &qan.Interval{
		Filename:    "/tmp/" + slowlogFile,
		StartOffset: 0,
		EndOffset:   736,
		StartTime:   now,
		StopTime:    now,
	}
	// Rotation happens in Setup(), but the log isn't rotated yet.
	w.Setup(i1)
	gotSet := s.nullmysql.GetExec()
	t.Check(gotSet, HasLen, 0)

	res, err := w.Run()
	t.Assert(err, IsNil)

	w.Cleanup()
	t.Check(res.Global.TotalQueries, Equals, uint(2))
	t.Check(res.Global.UniqueQueries, Equals, uint(1))

	// Second interval: 736 - 1833, but will actually go to end: 2200.
	i2 := &qan.Interval{
		Filename:    "/tmp/" + slowlogFile,
		StartOffset: 736,
		EndOffset:   1833,
		StartTime:   now,
		StopTime:    now,
	}
	w.Setup(i2)
	gotSet = s.nullmysql.GetExec()
	expectSet := append(config.Stop, config.Start...)
	if same, diff := IsDeeply(gotSet, expectSet); !same {
		Dump(gotSet)
		t.Error(diff)
	}

	// When rotated, the interval end offset is extended to end of file.
	t.Check(i2.EndOffset, Equals, int64(2200))

	res, err = w.Run()
	t.Assert(err, IsNil)

	// The old slow log is removed in Cleanup(), so it should still exist.
	files, _ = filepath.Glob("/tmp/" + slowlogFile + "-[0-9]*")
	t.Check(files, HasLen, 1)

	w.Cleanup()
	t.Check(res.Global.TotalQueries, Equals, uint(4))
	t.Check(res.Global.UniqueQueries, Equals, uint(2))

	// Original slow log should no longer exist; it was rotated away.
	if _, err := os.Stat("/tmp/" + slowlogFile); !os.IsNotExist(err) {
		t.Error("/tmp/" + slowlogFile + " no longer exists")
	}

	// The original slow log should have been renamed to slow006-TS, parsed, and removed.
	files, _ = filepath.Glob("/tmp/" + slowlogFile + "-[0-9]*")
	if len(files) != 0 {
		t.Errorf("Old slow log removed, got %+v", files)
	}
	defer func() {
		for _, file := range files {
			os.Remove(file)
		}
	}()

	// https://jira.percona.com/browse/PCT-466
	// Old slow log removed but space not freed in filesystem
	pid := fmt.Sprintf("%d", os.Getpid())
	out, err := exec.Command("lsof", "-p", pid).Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "/tmp/"+slowlogFile+"-") {
		t.Logf("%s\n", string(out))
		t.Error("Old slow log removed but not freed in filesystem (PCT-466)")
	}
}

func (s *WorkerTestSuite) TestRotateSlowLog(t *C) {
	// Same as TestRotateAndRemoveSlowLog but pc.QAN.RemoveOldSlowLogs=false
	// so the old slow log file is not removed.

	slowlogFile := "slow006.log"
	files, _ := filepath.Glob("/tmp/" + slowlogFile + "-[0-9]*")
	for _, file := range files {
		os.Remove(file)
	}

	// See TestStartService() for description of these startup tasks.
	config := pc.QAN{
		UUID:              s.mysqlInstance.UUID,
		Interval:          300,
		MaxSlowLogSize:    1000,
		RemoveOldSlowLogs: "false", // <-- HERE
		ExampleQueries:    "false",
		WorkerRunTime:     600,
		Start: []string{
			"-- start",
		},
		Stop: []string{
			"-- stop",
		},
		CollectFrom: "slowlog",
	}
	w := slowlog.NewWorker(s.logger, config, s.nullmysql)

	// Make copy of slow log because test will mv/rename it.
	cp := exec.Command("cp", inputDir+slowlogFile, "/tmp/"+slowlogFile)
	cp.Run()

	// First interval: 0 - 736
	now := time.Now()
	i1 := &qan.Interval{
		Filename:    "/tmp/" + slowlogFile,
		StartOffset: 0,
		EndOffset:   736,
		StartTime:   now,
		StopTime:    now,
	}
	// Rotation happens in Setup(), but the log isn't rotated yet.
	w.Setup(i1)
	gotSet := s.nullmysql.GetExec()
	t.Check(gotSet, HasLen, 0)

	res, err := w.Run()
	t.Assert(err, IsNil)

	w.Cleanup()
	t.Check(res.Global.TotalQueries, Equals, uint(2))
	t.Check(res.Global.UniqueQueries, Equals, uint(1))

	// Second interval: 736 - 1833, but will actually go to end: 2200.
	i2 := &qan.Interval{
		Filename:    "/tmp/" + slowlogFile,
		StartOffset: 736,
		EndOffset:   1833,
		StartTime:   now,
		StopTime:    now,
	}
	w.Setup(i2)
	gotSet = s.nullmysql.GetExec()
	expectSet := append(config.Stop, config.Start...)
	if same, diff := IsDeeply(gotSet, expectSet); !same {
		Dump(gotSet)
		t.Error(diff)
	}

	// When rotated, the interval end offset is extended to end of file.
	t.Check(i2.EndOffset, Equals, int64(2200))

	res, err = w.Run()
	t.Assert(err, IsNil)

	// The old slow log is removed in Cleanup(), so it should still exist.
	files, _ = filepath.Glob("/tmp/" + slowlogFile + "-[0-9]*")
	t.Check(files, HasLen, 1)

	w.Cleanup()
	t.Check(res.Global.TotalQueries, Equals, uint(4))
	t.Check(res.Global.UniqueQueries, Equals, uint(2))

	// Original slow log should no longer exist; it was rotated away.
	if _, err := os.Stat("/tmp/" + slowlogFile); !os.IsNotExist(err) {
		t.Error("/tmp/" + slowlogFile + " no longer exists")
	}

	// The original slow log should NOT have been removed.
	files, _ = filepath.Glob("/tmp/" + slowlogFile + "-[0-9]*")
	t.Check(files, HasLen, 1)
	defer func() {
		for _, file := range files {
			os.Remove(file)
		}
	}()

	// https://jira.percona.com/browse/PCT-466
	// Old slow log removed but space not freed in filesystem
	pid := fmt.Sprintf("%d", os.Getpid())
	out, err := exec.Command("lsof", "-p", pid).Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "/tmp/"+slowlogFile+"-") {
		t.Logf("%s\n", string(out))
		t.Error("Old slow log removed but not freed in filesystem (PCT-466)")
	}
}

func (s *WorkerTestSuite) TestStop(t *C) {
	config := pc.QAN{
		UUID:              s.mysqlInstance.UUID,
		Interval:          300,
		MaxSlowLogSize:    1024 * 1024 * 1024,
		RemoveOldSlowLogs: "true",
		WorkerRunTime:     60,
		Start:             []string{},
		Stop:              []string{},
		CollectFrom:       "slowlog",
	}
	w := slowlog.NewWorker(s.logger, config, s.nullmysql)

	// Make and set a mock log.LogParser. The worker will use this once when
	// Start() is called instead of making a real slow log parser.
	p := mock.NewLogParser()
	w.SetLogParser(p)

	now := time.Now()
	i := &qan.Interval{
		Number:      1,
		StartTime:   now,
		StopTime:    now.Add(1 * time.Minute),
		Filename:    inputDir + "slow006.log",
		StartOffset: 0,
		EndOffset:   100000,
	}
	w.Setup(i)

	// Run the worker. It calls p.Start() and p.Stop() when done.
	doneChan := make(chan bool, 1)
	var res *qan.Result
	var err error
	go func() {
		res, err = w.Run() // calls p.Start()
		doneChan <- true
	}()

	// Send first event. This is aggregated.
	e := &log.Event{
		Offset: 0,
		Ts:     "071015 21:45:10",
		Query:  "select 1 from t",
		Db:     "db1",
		TimeMetrics: map[string]float64{
			"Query_time": 1.111,
		},
	}
	p.Send(e) // blocks until received

	// This will block until we send a 2nd event...
	stopChan := make(chan bool, 1)
	go func() {
		w.Stop()
		stopChan <- true
	}()

	// Give Stop() time to send its signal. This isn't ideal, but it's necessary.
	time.Sleep(500 * time.Millisecond)

	// Send 2nd event which is not aggregated because a stop ^ is pending.
	e = &log.Event{
		Offset: 100,
		Ts:     "071015 21:50:10",
		Query:  "select 2 from u",
		Db:     "db2",
		TimeMetrics: map[string]float64{
			"Query_time": 2.222,
		},
	}
	p.Send(e) // blocks until received

	// Side test: Status()
	status := w.Status()
	t.Check(strings.HasPrefix(status["qan-worker"], "Parsing "+i.Filename), Equals, true)

	if !test.WaitState(stopChan) {
		t.Fatal("Timeout waiting for <-stopChan")
	}
	if !test.WaitState(doneChan) {
		t.Fatal("Timeout waiting for <-doneChan")
	}

	t.Check(res.Global.TotalQueries, Equals, uint(1))
	t.Check(res.Class, HasLen, 1)
}

/////////////////////////////////////////////////////////////////////////////
// IntervalIter test suite
/////////////////////////////////////////////////////////////////////////////

type IterTestSuite struct {
	logChan chan *proto.LogEntry
	logger  *pct.Logger
}

var _ = Suite(&IterTestSuite{})

func (s *IterTestSuite) SetUpSuite(t *C) {
	s.logChan = make(chan *proto.LogEntry, 100)
	s.logger = pct.NewLogger(s.logChan, "qan-worker")
}

var fileName string

func getFilename() (string, error) {
	return fileName, nil
}

func (s *IterTestSuite) TestIterFile(t *C) {
	tickChan := make(chan time.Time)

	// This is the file we iterate.  It's 3 bytes large to start,
	// so that should be the StartOffset.
	tmpFile, _ := ioutil.TempFile("/tmp", "interval_test.")
	tmpFile.Close()
	fileName = tmpFile.Name()
	_ = ioutil.WriteFile(tmpFile.Name(), []byte("123"), 0777)
	defer func() { os.Remove(tmpFile.Name()) }()

	// Start interating the file, waiting for ticks.
	i := slowlog.NewIter(s.logger, getFilename, tickChan)
	i.Start()

	// Send a tick to start the interval
	t1 := time.Now()
	tickChan <- t1

	// Write more data to the file, pretend time passes...
	_ = ioutil.WriteFile(tmpFile.Name(), []byte("123456"), 0777)

	// Send a 2nd tick to finish the interval
	t2 := time.Now()
	tickChan <- t2

	// Get the interval
	got := <-i.IntervalChan()
	expect := &qan.Interval{
		Number:      1,
		Filename:    fileName,
		StartTime:   t1,
		StopTime:    t2,
		StartOffset: 3,
		EndOffset:   6,
	}
	t.Check(got, DeepEquals, expect)

	/**
	 * Rename the file, then re-create it.  The file change should be detected.
	 */

	oldFileName := tmpFile.Name() + "-old"
	os.Rename(tmpFile.Name(), oldFileName)
	defer os.Remove(oldFileName)

	// Re-create original file and write new data.  We expect StartOffset=0
	// because the file is new, and EndOffset=10 because that's the len of
	// the new data.  The old ^ file/data had start/stop offset 3/6, so those
	// should not appear in next interval; if they do, then iter failed to
	// detect file change and is still reading old file.
	tmpFile, _ = os.Create(fileName)
	tmpFile.Close()
	_ = ioutil.WriteFile(fileName, []byte("123456789A"), 0777)

	t3 := time.Now()
	tickChan <- t3

	got = <-i.IntervalChan()
	expect = &qan.Interval{
		Number:      2,
		Filename:    fileName,
		StartTime:   t2,
		StopTime:    t3,
		StartOffset: 0,
		EndOffset:   10,
	}
	t.Check(got, DeepEquals, expect)

	// Iter should no longer detect file change.
	_ = ioutil.WriteFile(fileName, []byte("123456789ABCDEF"), 0777)
	//                                               ^^^^^ new data
	t4 := time.Now()
	tickChan <- t4

	got = <-i.IntervalChan()
	expect = &qan.Interval{
		Number:      3,
		Filename:    fileName,
		StartTime:   t3,
		StopTime:    t4,
		StartOffset: 10,
		EndOffset:   15,
	}
	t.Check(got, DeepEquals, expect)

	i.Stop()
}
