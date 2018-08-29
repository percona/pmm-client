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

package postgresql

import (
	"context"
	"testing"
	"time"

	"github.com/percona/pmm-client/pmm/plugin"
	"github.com/stretchr/testify/assert"
	"gopkg.in/DATA-DOG/go-sqlmock.v1"
)

func TestMakeGrants(t *testing.T) {
	type sample struct {
		dsn    DSN
		grants []string
	}
	samples := []sample{
		{
			dsn: DSN{User: "pmm", Password: "abc123"},
			grants: []string{
				"CREATE USER \"pmm\" WITH PASSWORD 'abc123'",
				"CREATE SCHEMA \"pmm\" AUTHORIZATION \"pmm\"",
				"ALTER USER \"pmm\" SET SEARCH_PATH TO \"pmm\",pg_catalog",
				"CREATE OR REPLACE VIEW \"pmm\".pg_stat_activity AS SELECT * from pg_catalog.pg_stat_activity",
				"GRANT SELECT ON \"pmm\".pg_stat_activity TO \"pmm\"",
				"CREATE OR REPLACE VIEW \"pmm\".pg_stat_replication AS SELECT * from pg_catalog.pg_stat_replication",
				"GRANT SELECT ON \"pmm\".pg_stat_replication TO \"pmm\"",
			},
		},
		{
			dsn: DSN{User: "admin", Password: "23;,_-asd"},
			grants: []string{
				"CREATE USER \"admin\" WITH PASSWORD '23;,_-asd'",
				"CREATE SCHEMA \"admin\" AUTHORIZATION \"admin\"",
				"ALTER USER \"admin\" SET SEARCH_PATH TO \"admin\",pg_catalog",
				"CREATE OR REPLACE VIEW \"admin\".pg_stat_activity AS SELECT * from pg_catalog.pg_stat_activity",
				"GRANT SELECT ON \"admin\".pg_stat_activity TO \"admin\"",
				"CREATE OR REPLACE VIEW \"admin\".pg_stat_replication AS SELECT * from pg_catalog.pg_stat_replication",
				"GRANT SELECT ON \"admin\".pg_stat_replication TO \"admin\"",
			},
		},
	}
	for _, s := range samples {
		grants := makeGrants(s.dsn, false, false)
		assert.Equal(t, s.grants, grants)
	}
}

func TestGetInfo(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("error opening a stub database connection: %s", err)
	}
	defer db.Close()

	columns := []string{"@@hostname", "@@port", "@@version"}
	rows := sqlmock.NewRows(columns).AddRow("db01", "3306", "1.2.3")
	mock.ExpectQuery(`SELECT inet_server_addr\(\), inet_server_port\(\), version\(\)`).WillReturnRows(rows)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	info, err := getInfo(ctx, db)
	assert.NoError(t, err)
	expected := plugin.Info{
		Hostname: "db01",
		Port:     "3306",
		Distro:   "PostgreSQL",
		Version:  "1.2.3",
	}
	assert.Equal(t, expected, *info)

	// Ensure all SQL queries were executed
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("there were unfulfilled expectations: %s", err)
	}
}
