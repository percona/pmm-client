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

package mrms_test

import (
	"testing"
	"time"

	"github.com/percona/pmm/proto"
	"github.com/percona/qan-agent/mrms"
	"github.com/percona/qan-agent/mysql"
	"github.com/percona/qan-agent/pct"
	"github.com/percona/qan-agent/test/mock"
	. "gopkg.in/check.v1"
	"os"
)

func Test(t *testing.T) { TestingT(t) }

var dsn = os.Getenv("PCT_TEST_MYSQL_DSN")

type TestSuite struct {
	nullmysql     *mock.NullMySQL
	logChan       chan *proto.LogEntry
	logger        *pct.Logger
	instance      proto.Instance
	emptyInstance proto.Instance
}

var _ = Suite(&TestSuite{})

func (s *TestSuite) SetUpSuite(t *C) {
	s.nullmysql = mock.NewNullMySQL()
	s.logChan = make(chan *proto.LogEntry, 1000)
	s.logger = pct.NewLogger(s.logChan, "mrms-monitor-test")
	s.instance = proto.Instance{
		Subsystem: "mysql",
		UUID:      "313",
		DSN:       "",
	}
}

// --------------------------------------------------------------------------

func (s *TestSuite) TestStartStop(t *C) {
	mockConn := mock.NewNullMySQL()
	mockConnFactory := &mock.ConnectionFactory{
		Conn: mockConn,
	}
	m := mrms.NewRealMonitor(s.logger, mockConnFactory)
	s.instance.DSN = "fake:dsn@tcp(127.0.0.1:3306)/?parseTime=true"

	// Add the instance to monitor, get back a restart chan.
	restartChan := m.Add(s.instance)

	// Set initial uptime before starting.
	mockConn.SetUptime(10)
	t.Assert(mockConn.GetUptimeCount(), Equals, uint(0))

	m.Check() // do 1st check so that ^ is set

	// Start the monitor. It checks uptime once before sleeping.
	err := m.Start(1 * time.Second)
	t.Assert(err, IsNil)

	// Imitate MySQL restart by setting uptime to 5s (previously 10s)
	mockConn.SetUptime(5)

	// After max 1 second it should notify listener about MySQL restart
	var gotInstance proto.Instance
	select {
	case gotInstance = <-restartChan:
	case <-time.After(1 * time.Second):
	}
	t.Check(gotInstance, DeepEquals, s.instance)

	// Stop the monitor.
	err = m.Stop()
	t.Assert(err, IsNil)

	// Check status
	status := m.Status()
	t.Check(status, DeepEquals, map[string]string{
		mrms.MONITOR_NAME: "Stopped",
	})

	// Imitate MySQL restart by setting uptime to 1s (previously 5s)
	mockConn.SetUptime(1)

	// After stopping service it should not notify listeners anymore
	time.Sleep(2 * time.Second)
	select {
	case gotInstance = <-restartChan:
		t.Error("Got restart after stopping monitor")
	default:
	}
}

/*
func (s *TestSuite) TestRestart(t *C) {
	mockConn := mock.NewNullMySQL()
	mockConnFactory := &mock.ConnectionFactory{
		Conn: mockConn,
	}
	m := mrms.NewRealMonitor(s.logger, mockConnFactory)

	s.instance.DSN = "fake:dsn@tcp(127.0.0.1:3306)/?parseTime=true"

	// Set initial uptime
	mockConn.SetUptime(10)
	restartChan := m.Add(s.instance)

	// MRMS should not send notification after first check for given dsn
	var gotInstance proto.Instance
	select {
	case gotInstance = <-restartChan:
	default:
	}
	t.Check(gotInstance, DeepEquals, s.emptyInstance)

	// If MySQL was restarted then MRMS should notify listener
	// Imitate MySQL restart by returning 0s uptime (previously 10s)
	mockConn.SetUptime(0)
	m.Check()
	gotInstance = s.emptyInstance
	select {
	case gotInstance = <-restartChan:
	default:
	}
	t.Check(gotInstance, DeepEquals, s.instance)

	// If MySQL was not restarted then MRMS should not notify listener
	// 2s uptime is higher than previous 0s, this indicates MySQL was not restarted
	mockConn.SetUptime(2)
	m.Check()
	gotInstance = s.emptyInstance
	select {
	case gotInstance = <-restartChan:
	default:
	}
	t.Check(gotInstance, DeepEquals, s.emptyInstance)

	// Now let's imitate MySQL server restart and let's wait 3 seconds before next check.
	// Since MySQL server was restarted and we waited 3s then uptime=3s
	// which is higher than last registered uptime=2s
	// However we expect in this test that this is properly detected as MySQL restart
	// and the MRMS notifies listeners
	waitTime := int64(3)
	time.Sleep(time.Duration(waitTime) * time.Second)
	mockConn.SetUptime(waitTime)
	gotInstance = s.emptyInstance
	m.Check()
	select {
	case gotInstance = <-restartChan:
	default:
	}
	t.Check(gotInstance, DeepEquals, s.instance)

	// After removing listener MRMS should not notify it anymore about MySQL restarts
	// Imitate MySQL restart by returning 0s uptime (previously 3s)
	mockConn.SetUptime(0)
	m.Remove(dsn, restartChan)
	m.Check()
	s.instance = s.emptyInstance
	select {
	case gotInstance = <-restartChan:
	default:
	}
	t.Check(gotInstance, DeepEquals, s.emptyInstance)
}
*/

/*
func (s *TestSuite) TestTwoListeners(t *C) {
	mockConn := mock.NewNullMySQL()
	mockConnFactory := &mock.ConnectionFactory{
		Conn: mockConn,
	}
	m := mrms.NewRealMonitor(s.logger, mockConnFactory)
	in1 := s.instance
	in2 := s.instance

	in1.DSN = "fake:dsn@tcp(127.0.0.1:3306)/?parseTime=true"
	in2.DSN = in1.DSN

	c1 := m.Add(in1)
	c2 := m.Add(in2)

	var gotInstance proto.Instance

	mockConn.SetUptime(1)
	m.Check()
	select {
	case gotInstance = <-c1:
	default:
	}
	t.Check(gotInstance, Equals, false)
	select {
	case gotInstance = <-c2:
	default:
	}
	t.Check(gotInstance, Equals, false)

	mockConn.SetUptime(2)
	m.Check()
	select {
	case gotInstance = <-c1:
	default:
	}
	t.Check(gotInstance, Equals, false)
	select {
	case gotInstance = <-c2:
	default:
	}
	t.Check(gotInstance, Equals, false)
}
*/

func (s *TestSuite) TestRealMySQL(t *C) {
	if dsn == "" {
		t.Skip("PCT_TEST_MYSQL_DSN is not set")
	}
	s.instance.DSN = dsn
	m := mrms.NewRealMonitor(s.logger, &mysql.RealConnectionFactory{})
	c := m.Add(s.instance)
	defer m.Remove(s.instance.UUID, c)
	for i := 0; i < 2; i++ {
		time.Sleep(1 * time.Second)
		m.Check()
		select {
		case <-c:
			t.Logf("False-positive restart reported on check number %d", i)
			t.FailNow()
		default:
		}
	}
}
