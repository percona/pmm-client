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

package mock

import (
	"database/sql"
	"time"

	"github.com/percona/qan-agent/mysql"
)

type SlowMySQL struct {
	realConnection *mysql.Connection
	globalDelay    time.Duration
}

func NewSlowMySQL(dsn string) *SlowMySQL {
	n := &SlowMySQL{
		realConnection: mysql.NewConnection(dsn),
		globalDelay:    0,
	}
	return n
}

func (s *SlowMySQL) DB() *sql.DB {
	time.Sleep(s.globalDelay)
	return s.realConnection.DB()
}

func (s *SlowMySQL) DSN() string {
	return s.realConnection.DSN()
}

func (s *SlowMySQL) Connect() error {
	return s.realConnection.Connect()
}

func (s *SlowMySQL) Close() {
	s.realConnection.Close()
}

func (s *SlowMySQL) Set(queries []mysql.Query) error {
	return s.realConnection.Set(queries)
}

func (s *SlowMySQL) GetGlobalVarString(varName string) string {
	return s.realConnection.GetGlobalVarString(varName)
}

func (s *SlowMySQL) GetGlobalVarNumber(varName string) float64 {
	return s.realConnection.GetGlobalVarNumber(varName)
}

func (s *SlowMySQL) Uptime() (int64, error) {
	return s.realConnection.Uptime()
}

func (s *SlowMySQL) SetGlobalDelay(delay time.Duration) {
	s.globalDelay = delay
}

func (s *SlowMySQL) AtLeastVersion(v string) (bool, error) {
	return s.realConnection.AtLeastVersion(v)
}
