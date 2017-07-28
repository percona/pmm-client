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
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/percona/go-mysql/dsn"
	"github.com/percona/qan-agent/pct"
)

var ErrNotConnected = errors.New("not connected")

type Query struct {
	Set    string // SET GLOBAL long_query_time=0
	Verify string // SELECT @@long_query_time
	Expect string // 0
}

type Connector interface {
	DB() *sql.DB
	DSN() string
	Connect() error
	Close()
	Set([]Query) error
	Exec([]string) error
	GetGlobalVarString(varName string) string
	GetGlobalVarNumber(varName string) float64
	Uptime() (uptime int64, err error)
	AtLeastVersion(string) (bool, error)
	UTCOffset() (time.Duration, time.Duration, error)
}

type Connection struct {
	dsn       string
	conn      *sql.DB
	connected bool
	*sync.Mutex
}

func NewConnection(dsn string) *Connection {
	c := &Connection{
		dsn:   dsn,
		Mutex: &sync.Mutex{},
	}
	return c
}

func (c *Connection) DB() *sql.DB {
	return c.conn
}

func (c *Connection) DSN() string {
	return c.dsn
}

func (c *Connection) Connect() error {
	c.Lock()
	defer c.Unlock()
	if c.connected {
		return nil
	}
	var err error
	var db *sql.DB

	// Make logical sql.DB connection, not an actual MySQL connection...
	db, err = sql.Open("mysql", c.dsn)
	if err != nil {
		return fmt.Errorf("Cannot connect to MySQL %s: %s", dsn.HidePassword(c.dsn), FormatError(err))
	}

	// Must call sql.DB.Ping to test actual MySQL connection.
	if err = db.Ping(); err != nil {
		db.Close()
		return fmt.Errorf("Cannot connect to MySQL %s: %s", dsn.HidePassword(c.dsn), FormatError(err))
	}

	c.conn = db
	c.connected = true
	return nil
}

func (c *Connection) Close() {
	c.Lock()
	defer c.Unlock()
	if !c.connected {
		return
	}
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
	c.connected = false
}

func (c *Connection) Set(queries []Query) error {
	if !c.connected {
		return ErrNotConnected
	}
	for _, query := range queries {
		if query.Set != "" {
			if _, err := c.conn.Exec(query.Set); err != nil {
				return err
			}
		}
		if query.Verify != "" {
			got := c.GetGlobalVarString(query.Verify)
			if got != query.Expect {
				return fmt.Errorf(
					"Global variable '%s' is set to '%s' but needs to be '%s'. "+
						"Consult the MySQL manual, or contact Percona Support, "+
						"for help configuring this variable, then try again.",
					query.Verify, got, query.Expect)
			}
		}
	}
	return nil
}

func (c *Connection) Exec(queries []string) error {
	if !c.connected {
		return ErrNotConnected
	}
	for _, query := range queries {
		if _, err := c.conn.Exec(query); err != nil {
			return err
		}
	}
	return nil
}

func (c *Connection) GetGlobalVarString(varName string) string {
	if !c.connected {
		return ""
	}
	var varValue string
	c.conn.QueryRow("SELECT @@GLOBAL." + varName).Scan(&varValue)
	return varValue
}

func (c *Connection) GetGlobalVarNumber(varName string) float64 {
	if !c.connected {
		return 0
	}
	var varValue float64
	c.conn.QueryRow("SELECT @@GLOBAL." + varName).Scan(&varValue)
	return varValue
}

func (c *Connection) Uptime() (uptime int64, err error) {
	if !c.connected {
		return 0, ErrNotConnected
	}
	// Result from SHOW STATUS includes two columns,
	// Variable_name and Value, we ignore the first one as we need only Value
	var varName string
	c.conn.QueryRow("SHOW STATUS LIKE 'Uptime'").Scan(&varName, &uptime)
	return uptime, nil
}

// Check if version v2 is equal or higher than v1 (v2 >= v1)
// v2 can be in form m.n.o-ubuntu
func (c *Connection) AtLeastVersion(minVersion string) (bool, error) {
	return pct.AtLeastVersion(c.GetGlobalVarString("version"), minVersion)
}

func (c *Connection) UTCOffset() (time.Duration, time.Duration, error) {
	var curHours int64
	var sysHours int64
	var err error

	if !c.connected {
		err = c.Connect()
		if err != nil {
			return 0, 0, err
		}
		defer c.Close()
	}

	// Current time zone (@@session.time_zone)
	err = c.conn.QueryRow("SELECT TIMESTAMPDIFF(HOUR, NOW(), UTC_TIMESTAMP())").Scan(&curHours)
	if err != nil {
		return 0, 0, err
	}

	// System time zone (@@global.system_time_zone)
	_, err = c.conn.Exec("SET time_zone='SYSTEM'")
	if err != nil {
		return 0, 0, err
	}
	err = c.conn.QueryRow("SELECT TIMESTAMPDIFF(HOUR, NOW(), UTC_TIMESTAMP())").Scan(&sysHours)
	if err != nil {
		return 0, 0, err
	}
	return time.Duration(curHours) * time.Hour, time.Duration(sysHours) * time.Hour, nil
}

var rePerconaServer = regexp.MustCompile("(?i)Percona Server")
var reMariaDB = regexp.MustCompile("(?i)MariaDB")

func Distro(distro string) string {
	if rePerconaServer.Match([]byte(distro)) {
		return "Percona Server"
	} else if reMariaDB.Match([]byte(distro)) {
		return "MariaDB"
	} else {
		return "MySQL"
	}
}
