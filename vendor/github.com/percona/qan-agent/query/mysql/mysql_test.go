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

package mysql_test

import (
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/percona/qan-agent/mysql"
	mysqlExec "github.com/percona/qan-agent/query/mysql"
	"github.com/percona/pmm/proto"
	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type TestSuite struct {
	dsn  string
	conn *mysql.Connection
	e    *mysqlExec.QueryExecutor
}

var _ = Suite(&TestSuite{})

func (s *TestSuite) SetUpSuite(t *C) {
	s.dsn = os.Getenv("PCT_TEST_MYSQL_DSN")
	if s.dsn == "" {
		t.Fatal("PCT_TEST_MYSQL_DSN is not set")
	}

	s.conn = mysql.NewConnection(s.dsn)
	if err := s.conn.Connect(); err != nil {
		t.Fatal(err)
	}
}

func (s *TestSuite) SetUpTest(t *C) {
	s.e = mysqlExec.NewQueryExecutor(s.conn)
}

func (s *TestSuite) TearDownSuite(t *C) {
	s.conn.Close()
}

// --------------------------------------------------------------------------

func (s *TestSuite) TestExplainWithoutDb(t *C) {
	db := ""
	query := "SELECT 1"

	expectedExplainResult := &proto.ExplainResult{
		Classic: []*proto.ExplainRow{
			&proto.ExplainRow{
				Id: proto.NullInt64{
					NullInt64: sql.NullInt64{
						Int64: 1,
						Valid: true,
					},
				},
				SelectType: proto.NullString{
					NullString: sql.NullString{
						String: "SIMPLE",
						Valid:  true,
					},
				},
				Table: proto.NullString{
					NullString: sql.NullString{
						String: "",
						Valid:  false,
					},
				},
				Type: proto.NullString{
					NullString: sql.NullString{
						String: "",
						Valid:  false,
					},
				},
				PossibleKeys: proto.NullString{
					NullString: sql.NullString{
						String: "",
						Valid:  false,
					},
				},
				Key: proto.NullString{
					NullString: sql.NullString{
						String: "",
						Valid:  false,
					},
				},
				KeyLen: proto.NullString{
					NullString: sql.NullString{
						String: "",
						Valid:  false,
					},
				},
				Ref: proto.NullString{
					NullString: sql.NullString{
						String: "",
						Valid:  false,
					},
				},
				Rows: proto.NullInt64{
					NullInt64: sql.NullInt64{
						Int64: 0,
						Valid: false,
					},
				},
				Extra: proto.NullString{
					NullString: sql.NullString{
						String: "No tables used",
						Valid:  true,
					},
				},
			},
		},
		JSON: "{\n  \"query_block\": {\n    \"select_id\": 1,\n    \"table\": {\n      \"message\": \"No tables used\"\n    }\n  }\n}",
	}

	gotExplainResult, err := s.e.Explain(db, query, true)
	t.Check(err, IsNil)
	t.Check(gotExplainResult, DeepEquals, expectedExplainResult)
}

func (s *TestSuite) TestExplainWithDb(t *C) {
	db := "information_schema"
	query := "SELECT table_name FROM tables WHERE table_name='tables'"

	expectedExplainResult := &proto.ExplainResult{
		Classic: []*proto.ExplainRow{
			&proto.ExplainRow{
				Id: proto.NullInt64{
					NullInt64: sql.NullInt64{
						Int64: 1,
						Valid: true,
					},
				},
				SelectType: proto.NullString{
					NullString: sql.NullString{
						String: "SIMPLE",
						Valid:  true,
					},
				},
				Table: proto.NullString{
					NullString: sql.NullString{
						String: "tables",
						Valid:  true,
					},
				},
				Type: proto.NullString{
					NullString: sql.NullString{
						String: "ALL",
						Valid:  true,
					},
				},
				PossibleKeys: proto.NullString{
					NullString: sql.NullString{
						String: "",
						Valid:  false,
					},
				},
				Key: proto.NullString{
					NullString: sql.NullString{
						String: "TABLE_NAME",
						Valid:  true,
					},
				},
				KeyLen: proto.NullString{
					NullString: sql.NullString{
						String: "",
						Valid:  false,
					},
				},
				Ref: proto.NullString{
					NullString: sql.NullString{
						String: "",
						Valid:  false,
					},
				},
				Rows: proto.NullInt64{
					NullInt64: sql.NullInt64{
						Int64: 0,
						Valid: false,
					},
				},
				Extra: proto.NullString{
					NullString: sql.NullString{
						String: "Using where; Skip_open_table; Scanned 1 database",
						Valid:  true,
					},
				},
			},
		},
		JSON: "{\n  \"query_block\": {\n    \"select_id\": 1,\n    \"table\": {\n      \"table_name\": \"tables\",\n      \"access_type\": \"ALL\",\n      \"key\": \"TABLE_NAME\",\n      \"skip_open_table\": true,\n      \"scanned_databases\": \"1\",\n      \"attached_condition\": \"(`information_schema`.`tables`.`TABLE_NAME` = 'tables')\"\n    }\n  }\n}",
	}

	gotExplainResult, err := s.e.Explain(db, query, true)
	t.Check(err, IsNil)
	t.Check(gotExplainResult, DeepEquals, expectedExplainResult)
}

func (s *TestSuite) TestDMLToSelect(t *C) {
	q := mysqlExec.DMLToSelect(`update ignore tabla set nombre = "carlos" where id = 0 limit 2`)
	t.Check(q, Equals, `SELECT nombre = "carlos" FROM tabla WHERE id = 0`)

	q = mysqlExec.DMLToSelect(`update ignore tabla set nombre = "carlos" where id = 0`)
	t.Check(q, Equals, `SELECT nombre = "carlos" FROM tabla WHERE id = 0`)

	q = mysqlExec.DMLToSelect(`update ignore tabla set nombre = "carlos" limit 1`)
	t.Check(q, Equals, `SELECT nombre = "carlos" FROM tabla`)

	q = mysqlExec.DMLToSelect(`update tabla set nombre = "carlos" where id = 0 limit 2`)
	t.Check(q, Equals, `SELECT nombre = "carlos" FROM tabla WHERE id = 0`)

	q = mysqlExec.DMLToSelect(`update tabla set nombre = "carlos" where id = 0`)
	t.Check(q, Equals, `SELECT nombre = "carlos" FROM tabla WHERE id = 0`)

	q = mysqlExec.DMLToSelect(`update tabla set nombre = "carlos" limit 1`)
	t.Check(q, Equals, `SELECT nombre = "carlos" FROM tabla`)

	q = mysqlExec.DMLToSelect(`delete from tabla`)
	t.Check(q, Equals, `SELECT * FROM tabla`)

	q = mysqlExec.DMLToSelect(`delete from tabla join tabla2 on tabla.id = tabla2.tabla2_id`)
	t.Check(q, Equals, `SELECT 1 FROM tabla join tabla2 on tabla.id = tabla2.tabla2_id`)

	q = mysqlExec.DMLToSelect(`insert into tabla (f1, f2, f3) values (1,2,3)`)
	t.Check(q, Equals, `SELECT * FROM tabla  WHERE f1=1 and f2=2 and f3=3`)

	q = mysqlExec.DMLToSelect(`insert into tabla (f1, f2, f3) values (1,2)`)
	t.Check(q, Equals, `SELECT * FROM tabla  LIMIT 1`)

	q = mysqlExec.DMLToSelect(`insert into tabla set f1="A1", f2="A2"`)
	t.Check(q, Equals, `SELECT * FROM tabla WHERE f1="A1" AND  f2="A2"`)

	q = mysqlExec.DMLToSelect(`replace into tabla set f1="A1", f2="A2"`)
	t.Check(q, Equals, `SELECT * FROM tabla WHERE f1="A1" AND  f2="A2"`)
}

func (s *TestSuite) TestFullTableInfo(t *C) {
	db := "mysql"
	table := "user"
	tables := &proto.TableInfoQuery{
		Create: []proto.Table{proto.Table{db, table}},
		Index:  []proto.Table{proto.Table{db, table}},
		Status: []proto.Table{proto.Table{db, table}},
	}

	got, err := s.e.TableInfo(tables)
	t.Assert(err, IsNil)

	tableInfo, ok := got[db+"."+table]
	t.Assert(ok, Equals, true)

	t.Logf("%+v\n", tableInfo)

	t.Assert(len(tableInfo.Errors), Equals, 0)

	t.Check(strings.HasPrefix(tableInfo.Create, "CREATE TABLE `user` ("), Equals, true)

	t.Assert(tableInfo.Status, NotNil)
	t.Check(tableInfo.Status.Name, Equals, table)

	// Indexes are grouped by name (KeyName), so all the index parts of the
	// PRIMARY key should be together.
	t.Assert(tableInfo.Index, Not(HasLen), 0)
	index, ok := tableInfo.Index["PRIMARY"]
	t.Assert(ok, Equals, true)
	t.Check(index, HasLen, 2)
	t.Check(index[0].ColumnName, Equals, "Host")
	t.Check(index[1].ColumnName, Equals, "User")
}

func (s *TestSuite) TestStatusTimes(t *C) {
	err := s.conn.DB().QueryRow("SELECT 1 FROM mysql.slow_log").Scan()
	if err != nil && err != sql.ErrNoRows {
		t.Log(err)
		t.Skip("mysql.slow_log table does not exist")
	}

	db := "mysql"
	table := "slow_log"
	tables := &proto.TableInfoQuery{
		Status: []proto.Table{proto.Table{db, table}},
	}

	got, err := s.e.TableInfo(tables)
	t.Assert(err, IsNil)

	tableInfo, ok := got[db+"."+table]
	t.Assert(ok, Equals, true)

	t.Logf("%+v\n", tableInfo)

	t.Assert(len(tableInfo.Errors), Equals, 0)

	t.Assert(tableInfo.Status, NotNil)
	t.Check(tableInfo.Status.Name, Equals, table)

	var zeroTime time.Time
	t.Check(tableInfo.Status.CreateTime.Time, Equals, zeroTime)
	t.Check(tableInfo.Status.UpdateTime.Time, Equals, zeroTime)
	t.Check(tableInfo.Status.CheckTime.Time, Equals, zeroTime)
}
