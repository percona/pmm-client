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

package pmm

import (
	"regexp"
	"strings"
	"testing"

	"github.com/percona/go-mysql/dsn"
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

	assert.Nil(t, mysqlCheck(db, []string{"localhost"}))

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

	assert.NotNil(t, mysqlCheck(db, []string{"localhost"}))

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

	assert.NotNil(t, mysqlCheck(db, []string{"localhost"}))

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

	assert.NotNil(t, mysqlCheck(db, []string{"localhost"}))

	// Ensure all SQL queries were executed
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("there were unfulfilled expectations: %s", err)
	}
}

func TestMakeGrants(t *testing.T) {
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
				"GRANT SELECT, PROCESS, REPLICATION CLIENT, RELOAD, SUPER ON *.* TO 'root'@'localhost' IDENTIFIED BY 'abc123' WITH MAX_USER_CONNECTIONS 5",
				"GRANT UPDATE, DELETE, DROP ON `performance_schema`.* TO 'root'@'localhost'",
				"GRANT SELECT, PROCESS, REPLICATION CLIENT, RELOAD, SUPER ON *.* TO 'root'@'127.0.0.1' IDENTIFIED BY 'abc123' WITH MAX_USER_CONNECTIONS 5",
				"GRANT UPDATE, DELETE, DROP ON `performance_schema`.* TO 'root'@'127.0.0.1'",
			},
		},
		{dsn: dsn.DSN{Username: "admin", Password: "23;,_-asd"},
			hosts: []string{"%"},
			conn:  20,
			grants: []string{
				"GRANT SELECT, PROCESS, REPLICATION CLIENT, RELOAD, SUPER ON *.* TO 'admin'@'%' IDENTIFIED BY '23;,_-asd' WITH MAX_USER_CONNECTIONS 20",
				"GRANT UPDATE, DELETE, DROP ON `performance_schema`.* TO 'admin'@'%'",
			},
		},
	}
	for _, s := range samples {
		assert.Equal(t, s.grants, makeGrants(s.dsn, s.hosts, s.conn))
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

	rows = sqlmock.NewRows([]string{"count"}).AddRow("500")
	mock.ExpectQuery(sanitizeQuery("SELECT COUNT(*) FROM information_schema.tables")).WillReturnRows(rows)

	res := getMysqlInfo(db, false)
	expected := map[string]string{
		"hostname":    "db01",
		"port":        "3306",
		"distro":      "MySQL",
		"version":     "1.2.3",
		"table_count": "500",
	}
	assert.Equal(t, expected, res)

	// Ensure all SQL queries were executed
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("there were unfulfilled expectations: %s", err)
	}
}

func TestGeneratePassword(t *testing.T) {
	r, _ := regexp.Compile("^([[:alnum:]]|[_,;-]){20}$")
	r1, _ := regexp.Compile("[[:lower:]]")
	r2, _ := regexp.Compile("[[:upper:]]")
	r3, _ := regexp.Compile("[[:digit:]]")
	r4, _ := regexp.Compile("[_,;-]")

	assert.Len(t, generatePassword(5), 5)
	assert.Len(t, generatePassword(20), 20)
	assert.NotEqual(t, generatePassword(20), generatePassword(20))
	for i := 0; i < 10; i++ {
		p := generatePassword(20)
		c := r.Match([]byte(p)) && r1.Match([]byte(p)) && r2.Match([]byte(p)) && r3.Match([]byte(p)) && r4.Match([]byte(p))
		assert.True(t, c)
	}
}

func sanitizeQuery(q string) string {
	return strings.NewReplacer(
		"(", "\\(",
		")", "\\)",
		"*", "\\*",
	).Replace(q)
}
