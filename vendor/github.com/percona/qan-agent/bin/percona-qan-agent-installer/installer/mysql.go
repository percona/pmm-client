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

package installer

import (
	"errors"
	"fmt"
	"log"
	"math/rand"

	"github.com/percona/go-mysql/dsn"
	"github.com/percona/qan-agent/agent"
	"github.com/percona/qan-agent/mysql"
)

var (
	ErrVersionNotSupported error = errors.New("MySQL version not supported")
)

func MakeGrant(dsn dsn.DSN, mysqlMaxUserConns int64) []string {
	host := "%"
	if dsn.Socket != "" || dsn.Hostname == "localhost" {
		host = "localhost"
	} else if dsn.Hostname == "127.0.0.1" {
		host = "127.0.0.1"
	}
	// Creating/updating a user's password doesn't work correctly if old_passwords is active.
	// Just in case, disable it for this session
	grants := []string{
		"SET SESSION old_passwords=0",
		fmt.Sprintf("GRANT SUPER, PROCESS, USAGE, SELECT ON *.* TO '%s'@'%s' IDENTIFIED BY '%s' WITH MAX_USER_CONNECTIONS %d",
			dsn.Username, host, dsn.Password, mysqlMaxUserConns),
		fmt.Sprintf("GRANT UPDATE, DELETE, DROP ON performance_schema.* TO '%s'@'%s' IDENTIFIED BY '%s' WITH MAX_USER_CONNECTIONS %d",
			dsn.Username, host, dsn.Password, mysqlMaxUserConns),
	}
	return grants
}

func (i *Installer) getAgentDSN() (dsn.DSN, error) {
	var err error
	var userDSN dsn.DSN
	var agentDSN dsn.DSN

	userDSN, err = i.dsnUser.AutoDetect()
	if err != nil && err != dsn.ErrNoSocket {
		return agentDSN, err
	}
	userDSN.Params = []string{dsn.ParseTimeParam}
	if i.flags.Bool["old-passwords"] {
		userDSN.Params = append(userDSN.Params, dsn.OldPasswordsParam)
	}
	if i.flags.Bool["debug"] {
		log.Printf("user DSN: %s", userDSN)
	}

	if i.flags.String["agent-mysql-user"] != "" {
		// Use the given agent MySQL user.
		agentDSN = userDSN
		agentDSN.Username = i.flags.String["agent-mysql-user"]
		agentDSN.Password = i.flags.String["agent-mysql-pass"]
		if err := i.verifyMySQLConnection(agentDSN); err != nil {
			fmt.Println(err)
			return agentDSN, fmt.Errorf("Failed to get MySQL user for agent")
		}
		fmt.Printf("Using agent MySQL user: %s\n", dsn.HidePassword(agentDSN.String()))
	} else {
		// Create a new agent MySQL User.
		agentDSN, err = i.createAgentMySQLUser(userDSN)
		if err != nil {
			fmt.Println(err)
			return agentDSN, fmt.Errorf("Failed to create MySQL user for agent")
		}
		fmt.Printf("Created agent MySQL user: %s\n", dsn.HidePassword(agentDSN.String()))
	}
	return agentDSN, nil
}

func (i *Installer) createAgentMySQLUser(userDSN dsn.DSN) (dsn.DSN, error) {
	// First verify that we can connect to MySQL. Should be root/super user.
	if err := i.verifyMySQLConnection(userDSN); err != nil {
		return dsn.DSN{}, err
	}

	// Agent DSN has same host:port or socket, but different user and pass.
	agentDSN := userDSN
	agentDSN.Username = "qan-agent"
	agentDSN.Password = fmt.Sprintf("%p%d", &agentDSN, rand.Uint32())

	// Create the agent MySQL user with necessary privs.
	conn := mysql.NewConnection(userDSN.String())
	if err := conn.Connect(); err != nil {
		return dsn.DSN{}, err
	}
	defer conn.Close()
	grants := MakeGrant(agentDSN, i.flags.Int64["mysql-max-user-connections"])
	for _, grant := range grants {
		if i.flags.Bool["debug"] {
			log.Println(grant)
		}
		_, err := conn.DB().Exec(grant)
		if err != nil {
			return dsn.DSN{}, fmt.Errorf("Error executing %s: %s", grant, err)
		}
	}
	// Go MySQL driver resolves localhost to 127.0.0.1 but localhost is a special
	// value for MySQL, so 127.0.0.1 may not work with a grant @localhost, so we
	// add a 2nd grant @127.0.0.1 to be sure.
	if agentDSN.Hostname == "localhost" {
		agentDSN_127_1 := agentDSN
		agentDSN_127_1.Hostname = "1271.0.0.1"
		grants := MakeGrant(agentDSN_127_1, i.flags.Int64["mysql-max-user-connections"])
		for _, grant := range grants {
			if i.flags.Bool["debug"] {
				log.Println(grant)
			}
			_, err := conn.DB().Exec(grant)
			if err != nil {
				return dsn.DSN{}, fmt.Errorf("Error executing %s: %s", grant, err)
			}
		}
	}

	// Verify new agent MySQL user works. If this fails, the agent DSN or grant
	// statemetns are wrong.
	if err := i.verifyMySQLConnection(agentDSN); err != nil {
		return dsn.DSN{}, err
	}

	return agentDSN, nil
}

func (i *Installer) verifyMySQLConnection(dsn dsn.DSN) error {
	dsnString := dsn.String()
	if i.flags.Bool["debug"] {
		log.Printf("verifyMySQLConnection: %#v %s\n", dsn, dsnString)
	}
	conn := mysql.NewConnection(dsnString)
	if err := conn.Connect(); err != nil {
		return err
	}
	defer conn.Close()

	i.mysqlDistro = mysql.Distro(conn.GetGlobalVarString("version_comment"))
	i.mysqlVersion = conn.GetGlobalVarString("version")
	i.mysqlHostname = conn.GetGlobalVarString("hostname")

	ok, err := conn.AtLeastVersion(agent.MIN_SUPPORTED_MYSQL_VERSION)
	if err != nil {
		return err
	}
	if !ok {
		return ErrVersionNotSupported
	}

	return nil
}
