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

package instance_test

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/percona/pmm/proto"
	"github.com/percona/qan-agent/instance"
	"github.com/percona/qan-agent/mysql"
	"github.com/percona/qan-agent/pct"
	"github.com/percona/qan-agent/test"
	"github.com/percona/qan-agent/test/mock"
	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type ManagerTestSuite struct {
	tmpDir      string
	logChan     chan *proto.LogEntry
	logger      *pct.Logger
	configDir   string
	instanceDir string
	api         *mock.API
}

var _ = Suite(&ManagerTestSuite{})

func (s *ManagerTestSuite) SetUpSuite(t *C) {
	var err error
	s.tmpDir, err = ioutil.TempDir("/tmp", "agent-test")
	t.Assert(err, IsNil)

	err = pct.Basedir.Init(s.tmpDir)
	t.Assert(err, IsNil)

	s.configDir = pct.Basedir.Dir("config")
	s.instanceDir = pct.Basedir.Dir("instance")
	s.logChan = make(chan *proto.LogEntry, 10)
	s.logger = pct.NewLogger(s.logChan, "pct-it-test")

	links := map[string]string{
		"instances": "http://localhost/instances",
	}
	s.api = mock.NewAPI("http://localhost", "http://localhost", "abc-123-def", links)
}

func (s *ManagerTestSuite) SetUpTest(t *C) {
	test.ClearDir(s.configDir)
}

func (s *ManagerTestSuite) TearDownSuite(t *C) {
	err := os.RemoveAll(s.tmpDir)
	t.Check(err, IsNil)
}

var dsn = os.Getenv("PCT_TEST_MYSQL_DSN")

// --------------------------------------------------------------------------

func (s *ManagerTestSuite) TestHandleGetInfoMySQL(t *C) {
	if dsn == "" {
		t.Fatal("PCT_TEST_MYSQL_DSN is not set")
	}

	// First get MySQL info manually.  This is what GetInfo should do, too.
	conn := mysql.NewConnection(dsn)
	if err := conn.Connect(); err != nil {
		t.Fatal(err)
	}
	var hostname, distro, version string
	sql := "SELECT" +
		" CONCAT_WS('.', @@hostname, IF(@@port='3306',NULL,@@port)) AS Hostname," +
		" @@version_comment AS Distro," +
		" @@version AS Version"
	if err := conn.DB().QueryRow(sql).Scan(&hostname, &distro, &version); err != nil {
		t.Fatal(err)
	}

	s.api.PutResp = []mock.APIResponse{
		{Code: 204, Data: nil, Error: nil},
	}

	// Create an instance manager.
	mrm := mock.NewMrmsMonitor()
	m := instance.NewManager(s.logger, s.instanceDir, s.api, mrm)
	err := m.Start()
	t.Assert(err, IsNil)
	defer m.Stop()

	mysqlIt := &proto.Instance{
		Subsystem: "mysql",
		UUID:      "313",
		Name:      "mysql-bm-cloud-0001",
		DSN:       dsn,
		Distro:    "", // not set yet
		Version:   "", // not set yet
	}
	mysqlData, err := json.Marshal(mysqlIt)
	t.Assert(err, IsNil)

	cmd := &proto.Cmd{
		Cmd:     "GetInfo",
		Service: "instance",
		Data:    mysqlData,
	}
	reply := m.Handle(cmd)

	var got proto.Instance
	err = json.Unmarshal(reply.Data, &got)
	t.Assert(err, IsNil)

	t.Check(got.Name, Equals, mysqlIt.Name) // not changed
	t.Check(got.DSN, Equals, mysqlIt.DSN)   // not changed
	t.Check(got.Distro, Equals, distro)     // set
	t.Check(got.Version, Equals, version)   // set
}

func (s *ManagerTestSuite) TestStartAndUpdate(t *C) {
	if dsn == "" {
		t.Fatal("PCT_TEST_MYSQL_DSN is not set")
	}

	mysqlInstanceFile := pct.Basedir.InstanceFile("BBB")

	var err error
	err = test.CopyFile(filepath.Join(test.RootDir, "instances/001/os-AAA.json"), pct.Basedir.InstanceFile("AAA"))
	t.Assert(err, IsNil)
	err = test.CopyFile(filepath.Join(test.RootDir, "instances/001/mysql-BBB.json"), mysqlInstanceFile)
	t.Assert(err, IsNil)

	bytes, err := ioutil.ReadFile(mysqlInstanceFile)
	t.Assert(err, IsNil)
	var in proto.Instance
	err = json.Unmarshal(bytes, &in)
	t.Assert(err, IsNil)
	in.DSN = dsn         // use a real DSN
	in.Distro = "wrong"  // should be updated
	in.Version = "wrong" // should be updated
	bytes, err = json.Marshal(in)
	t.Assert(err, IsNil)
	err = ioutil.WriteFile(mysqlInstanceFile, bytes, 0664)
	t.Assert(err, IsNil)

	// Create an instance manager.
	mrm := mock.NewMrmsMonitor()
	m := instance.NewManager(s.logger, s.instanceDir, s.api, mrm)
	err = m.Start()
	t.Assert(err, IsNil)
	defer m.Stop()

	s.api.GetResp = []mock.APIResponse{
		{Code: 404, Data: nil, Error: nil},
	}
	in, err = m.Repo().Get("notfound", false)
	t.Check(err, Equals, instance.ErrInstanceNotFound)

	in, err = m.Repo().Get("BBB", false)
	t.Check(err, IsNil)
	t.Check(in.UUID, Equals, "BBB")
	t.Check(in.ParentUUID, Equals, "AAA")
	t.Check(in.DSN, Equals, dsn)
	t.Check(in.Distro, Not(Equals), "")
	t.Check(in.Version, Not(Equals), "")

	cmd := &proto.Cmd{
		Cmd:  "RemoveInstance",
		Data: []byte("BBB"),
	}
	reply := m.Handle(cmd)
	t.Check(reply.Error, Equals, "")
	t.Check(pct.FileExists(mysqlInstanceFile), Equals, false)
}
