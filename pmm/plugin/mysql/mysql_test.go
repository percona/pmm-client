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

package mysql

import (
	"context"
	"testing"
	"time"

	"github.com/percona/go-mysql/dsn"
	"github.com/percona/pmm-client/pmm/plugin"
	"github.com/stretchr/testify/assert"
	"gopkg.in/DATA-DOG/go-sqlmock.v1"
)

func TestMySQLCheck1(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("error opening a stub database connection: %s", err)
	}
	defer db.Close()

	rows := sqlmock.NewRows([]string{"col1"}).AddRow("0")
	mock.ExpectQuery("SELECT @@read_only").WillReturnRows(rows)

	rows = sqlmock.NewRows([]string{"col1"})
	mock.ExpectQuery("SHOW SLAVE STATUS").WillReturnRows(rows)

	mock.ExpectQuery("SHOW GRANTS FOR 'pmm'@'localhost'").WillReturnError(err)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	assert.Nil(t, check(ctx, db, []string{"localhost"}))

	// Ensure all SQL queries were executed
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("there were unfulfilled expectations: %s", err)
	}
}

func TestMySQLCheck2(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("error opening a stub database connection: %s", err)
	}
	defer db.Close()

	rows := sqlmock.NewRows([]string{"col1"}).AddRow("1")
	mock.ExpectQuery("SELECT @@read_only").WillReturnRows(rows)

	rows = sqlmock.NewRows([]string{"col1"})
	mock.ExpectQuery("SHOW SLAVE STATUS").WillReturnRows(rows)

	mock.ExpectQuery("SHOW GRANTS FOR 'pmm'@'localhost'").WillReturnError(err)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	assert.NotNil(t, check(ctx, db, []string{"localhost"}))

	// Ensure all SQL queries were executed
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("there were unfulfilled expectations: %s", err)
	}
}

func TestMySQLCheck3(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("error opening a stub database connection: %s", err)
	}
	defer db.Close()

	rows := sqlmock.NewRows([]string{"col1"}).AddRow("0")
	mock.ExpectQuery("SELECT @@read_only").WillReturnRows(rows)

	rows = sqlmock.NewRows([]string{"col1"}).AddRow("1")
	mock.ExpectQuery("SHOW SLAVE STATUS").WillReturnRows(rows)

	mock.ExpectQuery("SHOW GRANTS FOR 'pmm'@'localhost'").WillReturnError(err)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	assert.NotNil(t, check(ctx, db, []string{"localhost"}))

	// Ensure all SQL queries were executed
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("there were unfulfilled expectations: %s", err)
	}
}

func TestMySQLCheck4(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("error opening a stub database connection: %s", err)
	}
	defer db.Close()

	rows := sqlmock.NewRows([]string{"col1"}).AddRow("0")
	mock.ExpectQuery("SELECT @@read_only").WillReturnRows(rows)

	rows = sqlmock.NewRows([]string{"col1"})
	mock.ExpectQuery("SHOW SLAVE STATUS").WillReturnRows(rows)

	rows = sqlmock.NewRows([]string{"col1"}).AddRow("grants...")
	mock.ExpectQuery("SHOW GRANTS FOR 'pmm'@'localhost'").WillReturnRows(rows)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	assert.NotNil(t, check(ctx, db, []string{"localhost"}))

	// Ensure all SQL queries were executed
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("there were unfulfilled expectations: %s", err)
	}
}

func TestMakeGrants(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("error opening a stub database connection: %s", err)
	}
	defer db.Close()

	{
		columns := []string{"version"}
		rows := sqlmock.NewRows(columns).AddRow("8.1.0")
		mock.ExpectQuery("SELECT @@GLOBAL.version").WillReturnRows(rows)
	}

	{
		columns := []string{"exists"}
		rows := sqlmock.NewRows(columns)
		mock.ExpectQuery("SELECT 1 FROM mysql.user WHERE user=?").WithArgs("root", "localhost").WillReturnRows(rows)
	}

	{
		columns := []string{"version"}
		rows := sqlmock.NewRows(columns).AddRow("8.1.0")
		mock.ExpectQuery("SELECT @@GLOBAL.version").WillReturnRows(rows)
	}

	{
		columns := []string{"exists"}
		rows := sqlmock.NewRows(columns)
		mock.ExpectQuery("SELECT 1 FROM mysql.user WHERE user=?").WithArgs("root", "127.0.0.1").WillReturnRows(rows)
	}

	{
		columns := []string{"version"}
		rows := sqlmock.NewRows(columns).AddRow("8.1.0")
		mock.ExpectQuery("SELECT @@GLOBAL.version").WillReturnRows(rows)
	}

	{
		columns := []string{"exists"}
		rows := sqlmock.NewRows(columns)
		mock.ExpectQuery("SELECT 1 FROM mysql.user WHERE user=?").WithArgs("admin", "%").WillReturnRows(rows)
	}

	type sample struct {
		dsn    dsn.DSN
		hosts  []string
		conn   uint16
		grants []string
	}
	samples := []sample{
		{dsn: dsn.DSN{Username: "root", Password: "abc123"},
			hosts: []string{"localhost", "127.0.0.1"},
			conn:  5,
			grants: []string{
				"CREATE USER 'root'@'localhost' IDENTIFIED BY 'abc123' WITH MAX_USER_CONNECTIONS 5",
				"GRANT SELECT, PROCESS, REPLICATION CLIENT, RELOAD, SUPER ON *.* TO 'root'@'localhost'",
				"GRANT UPDATE, DELETE, DROP ON `performance_schema`.* TO 'root'@'localhost'",
				"CREATE USER 'root'@'127.0.0.1' IDENTIFIED BY 'abc123' WITH MAX_USER_CONNECTIONS 5",
				"GRANT SELECT, PROCESS, REPLICATION CLIENT, RELOAD, SUPER ON *.* TO 'root'@'127.0.0.1'",
				"GRANT UPDATE, DELETE, DROP ON `performance_schema`.* TO 'root'@'127.0.0.1'",
			},
		},
		{dsn: dsn.DSN{Username: "admin", Password: "23;,_-asd"},
			hosts: []string{"%"},
			conn:  20,
			grants: []string{
				"CREATE USER 'admin'@'%' IDENTIFIED BY '23;,_-asd' WITH MAX_USER_CONNECTIONS 20",
				"GRANT SELECT, PROCESS, REPLICATION CLIENT, RELOAD, SUPER ON *.* TO 'admin'@'%'",
				"GRANT UPDATE, DELETE, DROP ON `performance_schema`.* TO 'admin'@'%'",
			},
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	for _, s := range samples {
		grants, err := makeGrants(ctx, db, s.dsn, s.hosts, s.conn)
		assert.NoError(t, err)
		assert.Equal(t, s.grants, grants)
	}
}

func TestGetMysqlInfo(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("error opening a stub database connection: %s", err)
	}
	defer db.Close()

	columns := []string{"@@hostname", "@@port", "@@version_comment", "@@version"}
	rows := sqlmock.NewRows(columns).AddRow("db01", "3306", "MySQL", "1.2.3")
	mock.ExpectQuery("SELECT @@hostname, @@port, @@version_comment, @@version").WillReturnRows(rows)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	info, err := getInfo(ctx, db)
	assert.NoError(t, err)
	expected := plugin.Info{
		Hostname: "db01",
		Port:     "3306",
		Distro:   "MySQL",
		Version:  "1.2.3",
	}
	assert.Equal(t, expected, *info)

	// Ensure all SQL queries were executed
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("there were unfulfilled expectations: %s", err)
	}
}
