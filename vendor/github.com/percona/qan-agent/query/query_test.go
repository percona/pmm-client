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

package query_test

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/percona/pmm/proto"
	"github.com/percona/qan-agent/instance"
	"github.com/percona/qan-agent/mysql"
	"github.com/percona/qan-agent/pct"
	"github.com/percona/qan-agent/query"
	"github.com/percona/qan-agent/test/mock"
	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

/////////////////////////////////////////////////////////////////////////////
// Manager test suite
/////////////////////////////////////////////////////////////////////////////

type ManagerTestSuite struct {
	logChan       chan *proto.LogEntry
	logger        *pct.Logger
	configDir     string
	tmpDir        string
	dsn           string
	repo          *instance.Repo
	mysqlInstance proto.Instance
	api           *mock.API
}

var _ = Suite(&ManagerTestSuite{})

func (s *ManagerTestSuite) SetUpSuite(t *C) {
	s.dsn = os.Getenv("PCT_TEST_MYSQL_DSN")
	if s.dsn == "" {
		t.Fatal("PCT_TEST_MYSQL_DSN is not set")
	}

	s.logChan = make(chan *proto.LogEntry, 10)
	s.logger = pct.NewLogger(s.logChan, query.SERVICE_NAME+"-manager-test")

	var err error
	s.tmpDir, err = ioutil.TempDir("/tmp", "agent-test")
	t.Assert(err, IsNil)

	if err := pct.Basedir.Init(s.tmpDir); err != nil {
		t.Fatal(err)
	}
	s.configDir = pct.Basedir.Dir("config")

	// Real instance repo
	s.repo = instance.NewRepo(pct.NewLogger(s.logChan, "im-test"), s.configDir, s.api)
	s.mysqlInstance = proto.Instance{
		Subsystem: "mysql",
		UUID:      "313",
		Name:      "db1",
		DSN:       s.dsn,
	}
	s.repo.Add(s.mysqlInstance, false)

	links := map[string]string{
		"agent":     "http://localhost/agent",
		"instances": "http://localhost/instances",
	}
	s.api = mock.NewAPI("http://localhost", "http://localhost", "abc-123-def", links)
}

func (s *ManagerTestSuite) SetUpTest(t *C) {
	glob := filepath.Join(pct.Basedir.Dir("config"), "*")
	files, err := filepath.Glob(glob)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if err := os.Remove(file); err != nil {
			t.Fatal(err)
		}
	}
}

func (s *ManagerTestSuite) TearDownSuite(t *C) {
	if err := os.RemoveAll(s.tmpDir); err != nil {
		t.Error(err)
	}
}

// --------------------------------------------------------------------------

func (s *ManagerTestSuite) TestStartStop(t *C) {
	var err error

	// Create query manager
	m := query.NewManager(s.logger, s.repo, &mysql.RealConnectionFactory{})
	t.Assert(m, NotNil)

	// The agent calls mm.Start().
	err = m.Start()
	t.Assert(err, IsNil)

	// Its status should be "Running".
	status := m.Status()
	t.Check(status[query.SERVICE_NAME], Equals, "Idle")

	// Can't start manager twice.
	err = m.Start()
	t.Check(err, FitsTypeOf, pct.ServiceIsRunningError{})

	// Stop the service.
	err = m.Stop()
	t.Check(err, IsNil)
	status = m.Status()
	t.Check(status[query.SERVICE_NAME], Equals, "Stopped")

	// Stop is idempotent.
	err = m.Stop()
	t.Check(err, IsNil)
	status = m.Status()
	t.Check(status[query.SERVICE_NAME], Equals, "Stopped")
}

func (s *ManagerTestSuite) TestHandleExplain(t *C) {
	var err error

	// Create query manager
	m := query.NewManager(s.logger, s.repo, &mysql.RealConnectionFactory{})
	t.Assert(m, NotNil)

	// The agent calls mm.Start().
	err = m.Start()
	t.Assert(err, IsNil)

	// Test known cmd
	query := proto.ExplainQuery{
		UUID:  "313",
		Query: "SELECT 1",
	}
	data, err := json.Marshal(query)
	t.Assert(err, IsNil)
	cmd := &proto.Cmd{
		Service: "query",
		Cmd:     "Explain",
		Data:    data,
	}
	gotReply := m.Handle(cmd)
	t.Assert(gotReply, NotNil)
	t.Check(gotReply.Error, Equals, "")
	t.Check(gotReply.Data, Not(HasLen), 0)

	// Test unknown cmd
	cmd = &proto.Cmd{
		Service: "query",
		Cmd:     "Unknown",
		Data:    data,
	}
	gotReply = m.Handle(cmd)
	t.Assert(gotReply, NotNil)
	t.Assert(gotReply.Error, Equals, fmt.Sprintf("Unknown command: %s", cmd.Cmd))
}
