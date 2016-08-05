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
	"fmt"
	"math/rand"
	"os"
	"time"

	"github.com/percona/go-mysql/dsn"
)

// MySQL specific options.
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
	MaxUserConn        uint
	Force              bool

	DisableTableStats  bool
	DisableUserStats   bool
	DisableBinlogStats bool
	DisableProcesslist bool
}

// DetectMySQL detect MySQL, create user if needed, return DSN and MySQL info strings.
func DetectMySQL(mf MySQLFlags) (map[string]string, error) {
	// Check for invalid mix of flags.
	if mf.Socket != "" && mf.Host != "" {
		return nil, fmt.Errorf("Flags --socket and --host are mutually exclusive.")
	}
	if mf.Socket != "" && mf.Port != "" {
		return nil, fmt.Errorf("Flags --socket and --port are mutually exclusive.")
	}
	if !mf.CreateUser && mf.CreateUserPassword != "" {
		return nil, fmt.Errorf("Flag --create-user-password should be used along with --create-user.")
	}

	userDSN := dsn.DSN{
		DefaultsFile: mf.DefaultsFile,
		Username:     mf.User,
		Password:     mf.Password,
		Hostname:     mf.Host,
		Port:         mf.Port,
		Socket:       mf.Socket,
		Params:       []string{dsn.ParseTimeParam},
	}
	// Populate defaults to DSN for missed options.
	userDSN, err := userDSN.AutoDetect()
	if err != nil && err != dsn.ErrNoSocket {
		err = fmt.Errorf("Problem with MySQL auto-detection: %s", err)
		return nil, err
	}

	// Get MySQL variables and test connection.
	db, err := sql.Open("mysql", userDSN.String())
	if err != nil {
		return nil, err
	}
	defer db.Close()
	helpTxt := "Use additional MySQL flags --user, --password, --host, --port, --socket if needed."
	info, err := getMysqlInfo(db)
	if err != nil {
		err = fmt.Errorf("Cannot connect to MySQL: %s\n\n%s\n%s", err,
			"Verify that MySQL user exists and has the correct privileges.", helpTxt)
		return nil, err
	}

	if mf.QuerySource == "auto" {
		// MySQL is local if the server hostname == MySQL hostname.
		os_hostname, _ := os.Hostname()
		if os_hostname == info["hostname"] {
			mf.QuerySource = "slowlog"
		} else {
			mf.QuerySource = "perfschema"
		}
	}

	// Create a new MySQL user.
	if mf.CreateUser {
		userDSN, err = createMySQLUser(userDSN, mf)
		if err != nil {
			err = fmt.Errorf("Cannot create MySQL user: %s\n\n%s\n%s", err,
				"Verify that connecting MySQL user exists and has GRANT privilege.", helpTxt)
			return nil, err
		}
	}

	info["query_source"] = mf.QuerySource
	info["dsn"] = userDSN.String()
	info["safe_dsn"] = SanitizeDSN(userDSN.String())

	return info, nil
}

// --------------------------------------------------------------------------

func createMySQLUser(userDSN dsn.DSN, mf MySQLFlags) (dsn.DSN, error) {
	db, err := sql.Open("mysql", userDSN.String())
	if err != nil {
		return dsn.DSN{}, err
	}
	defer db.Close()

	// New DSN has same host:port or socket, but different user and pass.
	newDSN := userDSN
	newDSN.Username = "pmm"
	newDSN.Password = generatePassword(20)

	// Create a new MySQL user with necessary privs.
	grants := makeGrant(newDSN, mf)
	for _, grant := range grants {
		if _, err := db.Exec(grant); err != nil {
			return dsn.DSN{}, fmt.Errorf("failed to execute %s: %s", grant, err)
		}
	}

	// Go MySQL driver resolves localhost to 127.0.0.1 but localhost is a special value for MySQL,
	// so 127.0.0.1 may not work with a grant @localhost, so we add a 2nd grant @127.0.0.1 to be sure.
	if newDSN.Hostname == "localhost" {
		newDSN_127 := newDSN
		newDSN_127.Hostname = "127.0.0.1"
		grants := makeGrant(newDSN_127, mf)
		for _, grant := range grants {
			if _, err := db.Exec(grant); err != nil {
				return dsn.DSN{}, fmt.Errorf("failed to execute %s: %s", grant, err)
			}
		}
	}

	// Verify new MySQL user works. If this fails, the new DSN or grant statements are wrong.
	if err := testConnection(newDSN); err != nil {
		return dsn.DSN{}, fmt.Errorf("problem with privileges: %s", err)
	}

	return newDSN, nil
}

func makeGrant(dsn dsn.DSN, mf MySQLFlags) []string {
	host := "%"
	if dsn.Socket != "" || dsn.Hostname == "localhost" {
		host = "localhost"
	} else if dsn.Hostname == "127.0.0.1" {
		host = "127.0.0.1"
	}

	grants := []string{
		fmt.Sprintf("GRANT SELECT, PROCESS, REPLICATION CLIENT, SUPER ON *.* TO '%s'@'%s' IDENTIFIED BY '%s' WITH MAX_USER_CONNECTIONS %d",
			dsn.Username, host, dsn.Password, mf.MaxUserConn),
		fmt.Sprintf("GRANT UPDATE, DELETE, DROP ON `performance_schema`.* TO '%s'@'%s'",
			dsn.Username, host),
	}

	return grants
}

func testConnection(userDSN dsn.DSN) error {
	db, err := sql.Open("mysql", userDSN.String())
	if err != nil {
		return err
	}
	defer db.Close()

	// Must call sql.DB.Ping to test actual MySQL connection.
	if err = db.Ping(); err != nil {
		return err
	}

	return nil
}

func getMysqlInfo(db *sql.DB) (map[string]string, error) {
	var (
		hostname string
		port     string
		distro   string
		version  string
	)
	if err := db.QueryRow("SELECT @@hostname, @@port, @@version_comment, @@version").Scan(
		&hostname, &port, &distro, &version); err != nil {
		return nil, err
	}
	info := map[string]string{
		"hostname": hostname,
		"port":     port,
		"distro":   distro,
		"version":  version,
	}
	return info, nil
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
	for _ = range b {
		pos1 := rand.Intn(len(b))
		pos2 := rand.Intn(len(b))
		a := b[pos1]
		b[pos1] = b[pos2]
		b[pos2] = a
	}
	return string(b)[:size]
}
