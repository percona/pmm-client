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
	"database/sql"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"time"
	"strconv"

	"github.com/percona/go-mysql/dsn"
)

// MySQLFlags MySQL specific flags.
type MySQLFlags struct {
	DefaultsFile string
	User         string
	Password     string
	Host         string
	Port         string
	Socket       string

	QuerySource string

	CreateUser         bool
	CreateUserPassword string
	MaxUserConn        uint16
	Force              bool

	DisableTableStats      bool
	DisableTableStatsLimit uint16
	DisableUserStats       bool
	DisableBinlogStats     bool
	DisableProcesslist     bool
	DisableQueryExamples   bool
}

// DetectMySQL detect MySQL, create user if needed, return DSN and MySQL info strings.
func (a *Admin) DetectMySQL(mf MySQLFlags) (map[string]string, error) {
	// Check for invalid mix of flags.
	if mf.Socket != "" && mf.Host != "" {
		return nil, errors.New("Flags --socket and --host are mutually exclusive.")
	}
	if mf.Socket != "" && mf.Port != "" {
		return nil, errors.New("Flags --socket and --port are mutually exclusive.")
	}
	if !mf.CreateUser && mf.CreateUserPassword != "" {
		return nil, errors.New("Flag --create-user-password should be used along with --create-user.")
	}

	userDSN := dsn.DSN{
		DefaultsFile: mf.DefaultsFile,
		Username:     mf.User,
		Password:     mf.Password,
		Hostname:     mf.Host,
		Port:         mf.Port,
		Socket:       mf.Socket,
		Params:       []string{dsn.ParseTimeParam, dsn.TimezoneParam, dsn.LocationParam},
	}
	// Populate defaults to DSN for missing options.
	userDSN, err := userDSN.AutoDetect()
	if err != nil && err != dsn.ErrNoSocket {
		err = fmt.Errorf("Problem with MySQL auto-detection: %s", err)
		return nil, err
	}

	db, err := sql.Open("mysql", userDSN.String())
	if err != nil {
		return nil, err
	}
	defer db.Close()

	// Test access using detected credentials and stored password.
	accessOK := false
	if a.Config.MySQLPassword != "" {
		pmmDSN := userDSN
		pmmDSN.Username = "pmm"
		pmmDSN.Password = a.Config.MySQLPassword
		if err := testConnection(pmmDSN.String()); err == nil {
			//fmt.Println("Using stored credentials, DSN is", pmmDSN.String())
			accessOK = true
			userDSN = pmmDSN
			// Not setting this into db connection as it will never have GRANT
			// in case we want to create a new user below.
		}
	}

	// If the above fails, test MySQL access simply using detected credentials.
	if !accessOK {
		if err := testConnection(userDSN.String()); err != nil {
			err = fmt.Errorf("Cannot connect to MySQL: %s\n\n%s\n%s", err,
				"Verify that MySQL user exists and has the correct privileges.",
				"Use additional flags --user, --password, --host, --port, --socket if needed.")
			return nil, err
		}
	}

	// At this point, we verified the MySQL access, so no need to handle SQL errors below
	// if our queries are predictably good.

	// Get MySQL variables.
	info := getMysqlInfo(db, mf.DisableTableStats)

	if mf.QuerySource == "auto" {
		// MySQL is local if the server hostname == MySQL hostname.
		osHostname, _ := os.Hostname()
		if osHostname == info["hostname"] {
			mf.QuerySource = "slowlog"
		} else {
			mf.QuerySource = "perfschema"
		}
	}

	// Create a new MySQL user.
	if mf.CreateUser {
		userDSN, err = createMySQLUser(db, userDSN, mf)
		if err != nil {
			return nil, err
		}

		// Store generated password.
		a.Config.MySQLPassword = userDSN.Password
		a.writeConfig()
	}

	info["query_source"] = mf.QuerySource
	info["query_examples"] = strconv.FormatBool(!mf.DisableQueryExamples)
	info["dsn"] = userDSN.String()
	info["safe_dsn"] = SanitizeDSN(userDSN.String())

	return info, nil
}

func createMySQLUser(db *sql.DB, userDSN dsn.DSN, mf MySQLFlags) (dsn.DSN, error) {
	// New DSN has same host:port or socket, but different user and pass.
	userDSN.Username = "pmm"
	if mf.CreateUserPassword != "" {
		userDSN.Password = mf.CreateUserPassword
	} else {
		userDSN.Password = generatePassword(20)
	}

	hosts := []string{"%"}
	if userDSN.Socket != "" || userDSN.Hostname == "localhost" {
		hosts = []string{"localhost", "127.0.0.1"}
	} else if userDSN.Hostname == "127.0.0.1" {
		hosts = []string{"127.0.0.1"}
	}

	if !mf.Force {
		if err := mysqlCheck(db, hosts); err != nil {
			return dsn.DSN{}, err
		}
	}

	// Create a new MySQL user with the necessary privs.
	grants := makeGrants(userDSN, hosts, mf.MaxUserConn)
	for _, grant := range grants {
		if _, err := db.Exec(grant); err != nil {
			err = fmt.Errorf("Problem creating a new MySQL user. Failed to execute %s: %s\n\n%s",
				grant, err, "Verify that connecting MySQL user has GRANT privilege.")
			return dsn.DSN{}, err
		}
	}

	// Verify new MySQL user works. If this fails, the new DSN or grant statements are wrong.
	if err := testConnection(userDSN.String()); err != nil {
		err = fmt.Errorf("Problem creating a new MySQL user. Insufficient privileges: %s", err)
		return dsn.DSN{}, err
	}

	return userDSN, nil
}

func mysqlCheck(db *sql.DB, hosts []string) error {
	var (
		errMsg []string
		varVal string
	)

	// Check for read_only.
	if db.QueryRow("SELECT @@read_only").Scan(&varVal); varVal == "1" {
		errMsg = append(errMsg, "* You are trying to write on read-only MySQL host.")
	}

	// Check for slave.
	if slaveStatusRows, err := db.Query("SHOW SLAVE STATUS"); err == nil {
		if slaveStatusRows.Next() {
			errMsg = append(errMsg, "* You are trying to write on MySQL replication slave.")
		}
	}

	// Check if user exists.
	for _, host := range hosts {
		if rows, err := db.Query(fmt.Sprintf("SHOW GRANTS FOR 'pmm'@'%s'", host)); err == nil {
			// MariaDB requires to check .Next() because err is always nil even user doesn't exist %)
			if !rows.Next() {
				continue
			}
			if host == "%" {
				host = "%%"
			}
			errMsg = append(errMsg, fmt.Sprintf("* MySQL user pmm@%s already exists. %s", host,
				"Try without --create-user flag using the default credentials or specify the existing `pmm` user ones."))
			break
		}
	}

	if len(errMsg) > 0 {
		errMsg = append([]string{"Problem creating a new MySQL user:", ""}, errMsg...)
		errMsg = append(errMsg, "", "If you think the above is okay to proceed, you can use --force flag.")
		return errors.New(strings.Join(errMsg, "\n"))
	}

	return nil
}

func makeGrants(dsn dsn.DSN, hosts []string, conn uint16) []string {
	var grants []string
	for _, host := range hosts {
		// Privileges:
		// PROCESS - for mysqld_exporter to get all processes from `SHOW PROCESSLIST`
		// REPLICATION CLIENT - for mysqld_exporter to run `SHOW BINARY LOGS`
		// RELOAD - for qan-agent to run `FLUSH SLOW LOGS`
		// SUPER - for qan-agent to set global variables (not clear it is still required)
		// Grants for performance_schema - for qan-agent to manage query digest tables.
		grants = append(grants,
			fmt.Sprintf("GRANT SELECT, PROCESS, REPLICATION CLIENT, RELOAD, SUPER ON *.* TO '%s'@'%s' IDENTIFIED BY '%s' WITH MAX_USER_CONNECTIONS %d",
				dsn.Username, host, dsn.Password, conn),
			fmt.Sprintf("GRANT UPDATE, DELETE, DROP ON `performance_schema`.* TO '%s'@'%s'",
				dsn.Username, host))
	}

	return grants
}

func testConnection(dsn string) error {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return err
	}
	defer db.Close()

	if err = db.Ping(); err != nil {
		return err
	}

	return nil
}

func getMysqlInfo(db *sql.DB, disableTableStats bool) map[string]string {
	var hostname, port, distro, version, tableCount string
	db.QueryRow("SELECT @@hostname, @@port, @@version_comment, @@version").Scan(&hostname, &port, &distro, &version)
	// Do not count number of tables if we explicitly disable table stats.
	if !disableTableStats {
		db.QueryRow("SELECT COUNT(*) FROM information_schema.tables").Scan(&tableCount)
	}

	return map[string]string{
		"hostname":    hostname,
		"port":        port,
		"distro":      distro,
		"version":     version,
		"table_count": tableCount,
	}
}

// generatePassword generate password to satisfy MySQL 5.7 default password policy.
func generatePassword(size int) string {
	rand.Seed(time.Now().UnixNano())
	required := []string{
		"abcdefghijklmnopqrstuvwxyz", "ABCDEFGHIJKLMNOPQRSTUVWXYZ", "0123456789", "_,;-",
	}
	var b []rune

	for _, source := range required {
		rsource := []rune(source)
		for i := 0; i < int(size/len(required))+1; i++ {
			b = append(b, rsource[rand.Intn(len(rsource))])
		}
	}
	// Scramble.
	for range b {
		pos1 := rand.Intn(len(b))
		pos2 := rand.Intn(len(b))
		a := b[pos1]
		b[pos1] = b[pos2]
		b[pos2] = a
	}
	return string(b)[:size]
}
