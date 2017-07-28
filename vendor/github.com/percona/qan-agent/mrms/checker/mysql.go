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

package checker

import (
	"fmt"
	"github.com/percona/qan-agent/mysql"
	"github.com/percona/qan-agent/pct"
	"time"
)

type MySQL struct {
	logger    *pct.Logger
	mysqlConn mysql.Connector
	// --
	lastUptime      int64
	lastUptimeCheck time.Time
}

func NewMySQL(logger *pct.Logger, mysqlConn mysql.Connector) *MySQL {
	m := &MySQL{
		logger:    logger,
		mysqlConn: mysqlConn,
	}
	return m
}

func (m *MySQL) Check() (bool, error) {
	if err := m.mysqlConn.Connect(); err != nil {
		return false, err
	}
	defer m.mysqlConn.Close()

	if m.lastUptime == 0 {
		// First check, just init and return.
		var err error
		m.lastUptime, err = m.mysqlConn.Uptime()
		if err != nil {
			return false, err // shouldn't happen because we just opened the connection
		}
		m.lastUptimeCheck = time.Now()
		m.logger.Debug(fmt.Sprintf("init uptime=%d", m.lastUptime))
		return false, nil
	}

	lastUptime := m.lastUptime
	lastUptimeCheck := m.lastUptimeCheck
	currentUptime, err := m.mysqlConn.Uptime()
	if err != nil {
		return false, err
	}

	m.logger.Debug(fmt.Sprintf("lastUptime=%d lastUptimeCheck=%s currentUptime=%d",
		lastUptime, lastUptimeCheck.UTC(), currentUptime))

	// Calculate expected uptime
	//   This protects against situation where after restarting MySQL
	//   we are unable to connect to it for period longer than last registered uptime
	//
	// Steps to reproduce:
	// * currentUptime=60 lastUptime=0
	// * Restart MySQL
	// * QAN connection problem for 120s
	// * currentUptime=120 lastUptime=60 (new uptime (120s) is higher than last registered (60s))
	// * elapsedTime=120s (time elapsed since last check)
	// * expectedUptime= 60s + 120s = 180s
	// * 120s < 180s (currentUptime < expectedUptime) => server was restarted
	elapsedTime := time.Now().Unix() - lastUptimeCheck.Unix()
	expectedUptime := lastUptime + elapsedTime
	m.logger.Debug(fmt.Sprintf("elapsedTime=%d expectedUptime=%d", elapsedTime, expectedUptime))

	// Save uptime from last check
	m.lastUptime = currentUptime
	m.lastUptimeCheck = time.Now()

	// If current server uptime is lower than last registered uptime
	// then we can assume that server was restarted
	if currentUptime < expectedUptime {
		return true, nil
	}

	return false, nil
}

func (m *MySQL) DSN() string {
	return m.mysqlConn.DSN()
}
