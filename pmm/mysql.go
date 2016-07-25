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

// MySQL and agent specific options.
type MySQLFlags struct {
	DefaultsFile         string
	User                 string
	Password             string
	Host                 string
	Port                 string
	Socket               string
	QuerySource          string
	MaxUserConn          uint
	OldPasswords         bool
	CreateUser           bool
	DisableInfoSchema    bool
	DisablePerTableStats bool
}

// DetectMySQL detect MySQL, create user if needed, return DSN and MySQL info strings.
func DetectMySQL(serviceType string, mf MySQLFlags) (map[string]string, error) {
	// Check for invalid mix of flags.
	if mf.Socket != "" && mf.Host != "" {
		return nil, fmt.Errorf("Flags --socket and --host are mutually exclusive.")
	}
	if mf.Socket != "" && mf.Port != "" {
		return nil, fmt.Errorf("Flags --socket and --port are mutually exclusive.")
	}

	userDSN := dsn.DSN{
		DefaultsFile: mf.DefaultsFile,
		Username:     mf.User,
		Password:     mf.Password,
		Hostname:     mf.Host,
		Port:         mf.Port,
		Socket:       mf.Socket,
		Params:       []string{dsn.ParseTimeParam}, // Probably needed for QAN.
	}
	// Populate defaults to DSN for missed options.
	userDSN, err := userDSN.AutoDetect()
	if err != nil && err != dsn.ErrNoSocket {
		err = fmt.Errorf("Problem with MySQL auto-detection: %s", err)
		return nil, err
	}
	if mf.OldPasswords {
		userDSN.Params = append(userDSN.Params, dsn.OldPasswordsParam)
	}

	if mf.CreateUser {
		// Create a new MySQL user.
		userDSN, err = createMySQLUser(userDSN, serviceType, mf.MaxUserConn)
	} else {
		// Use the given MySQL user to test connection.
		err = testConnection(userDSN)
	}

	if err != nil {
		helpTxt := "Use additional MySQL flags --user, --password, --host, --port, --socket if needed."
		if mf.CreateUser {
			err = fmt.Errorf("Error creating a new MySQL user: %s\n\n%s\n%s", err,
				"Verify that connecting MySQL user exists and has GRANT privilege.", helpTxt)
		} else {
			err = fmt.Errorf("Error: %s\n\n%s\n%s", err,
				"Verify that MySQL user exists and has the correct privileges.", helpTxt)
		}
		return nil, err
	}

	// Get MySQL hostname, port, distro, and version. This shouldn't fail because we just verified the MySQL user.
	info, err := mysqlInfo(userDSN, mf.QuerySource)
	if err != nil {
		err = fmt.Errorf("Failed to get MySQL info: %s", err)
		return nil, err
	}

	return info, nil
}

// --------------------------------------------------------------------------

func createMySQLUser(userDSN dsn.DSN, serviceType string, maxUserConn uint) (dsn.DSN, error) {
	// First verify that we can connect to MySQL. Should be root/super user.
	db, err := sql.Open("mysql", userDSN.String())
	if err != nil {
		return dsn.DSN{}, err
	}
	defer db.Close()

	// New DSN has same host:port or socket, but different user and pass.
	newDSN := userDSN
	newDSN.Username = fmt.Sprintf("pmm-%s", serviceType)
	newDSN.Password = generatePassword(20)

	// Create a new MySQL user with necessary privs.
	grants := makeGrant(newDSN, serviceType, maxUserConn)
	for _, grant := range grants {
		if _, err := db.Exec(grant); err != nil {
			return dsn.DSN{}, fmt.Errorf("cannot execute %s: %s", grant, err)
		}
	}

	// Go MySQL driver resolves localhost to 127.0.0.1 but localhost is a special value for MySQL,
	// so 127.0.0.1 may not work with a grant @localhost, so we add a 2nd grant @127.0.0.1 to be sure.
	if newDSN.Hostname == "localhost" {
		newDSN_127 := newDSN
		newDSN_127.Hostname = "127.0.0.1"
		grants := makeGrant(newDSN_127, serviceType, maxUserConn)
		for _, grant := range grants {
			if _, err := db.Exec(grant); err != nil {
				return dsn.DSN{}, fmt.Errorf("cannot execute %s: %s", grant, err)
			}
		}
	}

	// Verify new MySQL user works. If this fails, the new DSN or grant statements are wrong.
	if err := testConnection(newDSN); err != nil {
		return dsn.DSN{}, err
	}

	return newDSN, nil
}

func makeGrant(dsn dsn.DSN, serviceType string, mysqlMaxUserConns uint) []string {
	host := "%"
	if dsn.Socket != "" || dsn.Hostname == "localhost" {
		host = "localhost"
	} else if dsn.Hostname == "127.0.0.1" {
		host = "127.0.0.1"
	}
	// Creating/updating a user's password doesn't work correctly if old_passwords is active.
	// Just in case, disable it for this session.
	grants := []string{"SET SESSION old_passwords=0"}
	if serviceType == "queries" {
		grants = append(grants,
			fmt.Sprintf("GRANT SELECT, PROCESS, SUPER ON *.* TO '%s'@'%s' IDENTIFIED BY '%s' WITH MAX_USER_CONNECTIONS %d",
				dsn.Username, host, dsn.Password, mysqlMaxUserConns),
			fmt.Sprintf("GRANT SELECT, UPDATE, DELETE, DROP ON performance_schema.* TO '%s'@'%s'",
				dsn.Username, host),
		)
	}
	if serviceType == "mysql" {
		grants = append(grants,
			fmt.Sprintf("GRANT PROCESS, REPLICATION CLIENT ON *.* TO '%s'@'%s' IDENTIFIED BY '%s' WITH MAX_USER_CONNECTIONS %d",
				dsn.Username, host, dsn.Password, mysqlMaxUserConns),
			fmt.Sprintf("GRANT SELECT ON performance_schema.* TO '%s'@'%s'",
				dsn.Username, host),
		)
	}
	return grants
}

func testConnection(userDSN dsn.DSN) error {
	// Make logical sql.DB connection, not an actual MySQL connection...
	db, err := sql.Open("mysql", userDSN.String())
	if err != nil {
		return fmt.Errorf("cannot connect to MySQL %s: %s", SanitizeDSN(userDSN.String()), err)
	}
	defer db.Close()

	// Must call sql.DB.Ping to test actual MySQL connection.
	if err = db.Ping(); err != nil {
		return fmt.Errorf("cannot connect to MySQL %s: %s", SanitizeDSN(userDSN.String()), err)
	}

	return nil
}

func mysqlInfo(userDSN dsn.DSN, source string) (map[string]string, error) {
	db, err := sql.Open("mysql", userDSN.String())
	if err != nil {
		return nil, err
	}
	defer db.Close()
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

	if source == "auto" {
		// MySQL is local if the server hostname == MySQL hostname.
		os_hostname, _ := os.Hostname()
		if os_hostname == hostname {
			source = "slowlog"
		} else {
			source = "perfschema"
		}
	}
	info := map[string]string{
		"hostname":     hostname,
		"port":         port,
		"distro":       distro,
		"version":      version,
		"query_source": source,
		"dsn":          userDSN.String(),
		"safe_dsn":     SanitizeDSN(userDSN.String()),
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
