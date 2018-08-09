package postgresql

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/lib/pq"

	"github.com/percona/pmm-client/pmm/plugin"
	"github.com/percona/pmm-client/pmm/utils"
)

// Flags are PostgreSQL specific flags.
type Flags struct {
	DSN
	CreateUser         bool
	CreateUserPassword string
	Force              bool
}

// DSN represents PostgreSQL data source name.
type DSN struct {
	User     string
	Password string
	Host     string
	Port     string
	SSLMode  string
}

// String converts DSN struct to DSN string.
func (d DSN) String() string {
	var buf bytes.Buffer

	buf.WriteString("postgresql://")

	// [username]
	if len(d.User) > 0 {
		buf.WriteString(d.User)
	}

	// [:password]
	if len(d.Password) > 0 {
		buf.WriteByte(':')
		buf.WriteString(d.Password)
	}

	// @ is required if User or Password is set.
	if len(d.User) > 0 || len(d.Password) > 0 {
		buf.WriteByte('@')
	}

	// [host]
	if len(d.Host) > 0 {
		buf.WriteString(d.Host)
	}

	// [:port]
	if len(d.Port) > 0 {
		buf.WriteByte(':')
		buf.WriteString(d.Port)
	}

	buf.WriteString("/postgres")
	buf.WriteString("?sslmode=")
	if d.SSLMode == "" {
		d.SSLMode = "disable"
	}
	buf.WriteString(d.SSLMode)

	return buf.String()
}

// Init verifies PostgreSQL connection and creates PMM user if requested.
func Init(ctx context.Context, flags Flags, pmmUserPassword string) (*plugin.Info, error) {
	// Check for invalid mix of flags.
	if flags.CreateUser && flags.CreateUserPassword != "" {
		return nil, errors.New("flag --create-user-password should be used along with --create-user")
	}

	userDSN := flags.DSN
	db, err := sql.Open("postgres", userDSN.String())
	if err != nil {
		return nil, err
	}
	defer db.Close()

	// Test access using detected credentials and stored password.
	accessOK := false
	if pmmUserPassword != "" {
		pmmDSN := userDSN
		pmmDSN.User = "pmm"
		pmmDSN.Password = pmmUserPassword
		if err := testConnection(ctx, pmmDSN.String()); err == nil {
			//fmt.Println("Using stored credentials, DSN is", pmmDSN.String())
			accessOK = true
			userDSN = pmmDSN
			// Not setting this into db connection as it will never have GRANT
			// in case we want to create a new user below.
		}
	}

	// If the above fails, test PostgreSQL access simply using detected credentials.
	if !accessOK {
		if err := testConnection(ctx, userDSN.String()); err != nil {
			err = fmt.Errorf("Cannot connect to PostgreSQL: %s\n\n%s\n%s", err,
				"Verify that PostgreSQL user exists and has the correct privileges.",
				"Use additional flags --user, --password, --host, --port if needed.")
			return nil, err
		}
	}

	// Get PostgreSQL variables.
	info, err := getInfo(ctx, db)
	if err != nil {
		return nil, err
	}

	// Create a new PostgreSQL user.
	if flags.CreateUser {
		userDSN, err = createUser(ctx, db, userDSN, flags)
		if err != nil {
			return nil, err
		}

		// Store generated password.
		info.PMMUserPassword = userDSN.Password
	}

	info.DSN = userDSN.String()

	return info, nil
}

func createUser(ctx context.Context, db *sql.DB, userDSN DSN, flags Flags) (DSN, error) {
	// New DSN has same host:port or socket, but different user and pass.
	userDSN.User = "pmm"
	if flags.CreateUserPassword != "" {
		userDSN.Password = flags.CreateUserPassword
	} else {
		userDSN.Password = utils.GeneratePassword(20)
	}

	if !flags.Force {
		if err := check(ctx, db, userDSN.User); err != nil {
			return DSN{}, err
		}
	}

	// Create a new PostgreSQL user with the necessary privs.
	grants, err := makeGrants(ctx, db, userDSN)
	if err != nil {
		return DSN{}, err
	}
	for _, grant := range grants {
		if _, err := db.Exec(grant); err != nil {
			return DSN{}, fmt.Errorf("Problem creating a new PostgreSQL user. Failed to execute %s: %s", grant, err)
		}
	}

	// Verify new PostgreSQL user works. If this fails, the new DSN or grant statements are wrong.
	if err := testConnection(ctx, userDSN.String()); err != nil {
		return DSN{}, fmt.Errorf("Problem creating a new PostgreSQL user. Insufficient privileges: %s", err)
	}

	return userDSN, nil
}

func check(ctx context.Context, db *sql.DB, username string) error {
	var (
		errMsg []string
	)

	// Check if user exists.
	exists, err := userExists(ctx, db, username)
	if err != nil {
		return err
	}
	if exists {
		errMsg = append(errMsg, fmt.Sprintf("* PostgreSQL user %s already exists. %s", username,
			"Try without --create-user flag using the default credentials or specify the existing `pmm` user ones."))
	}

	if len(errMsg) > 0 {
		errMsg = append([]string{"Problem creating a new PostgreSQL user:", ""}, errMsg...)
		errMsg = append(errMsg, "", "If you think the above is okay to proceed, you can use --force flag.")
		return errors.New(strings.Join(errMsg, "\n"))
	}

	return nil
}

func makeGrants(ctx context.Context, db *sql.DB, dsn DSN) ([]string, error) {
	var grants []string
	quotedUser := pq.QuoteIdentifier(dsn.User)

	// Verify if user exists, if so then just update password.
	exists, err := userExists(ctx, db, dsn.User)
	if err != nil {
		return nil, err
	}
	query := ""
	if exists {
		query = fmt.Sprintf("ALTER USER %s WITH PASSWORD '%s'", quotedUser, dsn.Password)
	} else {
		query = fmt.Sprintf("CREATE USER %s WITH PASSWORD '%s'", quotedUser, dsn.Password)
	}
	grants = append(grants, query)

	// Allow to scrape metrics as non-root user.
	grants = append(grants,
		fmt.Sprintf("ALTER USER %s SET SEARCH_PATH TO %s,pg_catalog", quotedUser, quotedUser),
		fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s AUTHORIZATION %s", quotedUser, quotedUser),
		fmt.Sprintf("CREATE OR REPLACE VIEW %s.pg_stat_activity AS SELECT * from pg_catalog.pg_stat_activity", quotedUser),
		fmt.Sprintf("GRANT SELECT ON %s.pg_stat_activity TO %s", quotedUser, quotedUser),
		fmt.Sprintf("CREATE OR REPLACE VIEW %s.pg_stat_replication AS SELECT * from pg_catalog.pg_stat_replication", quotedUser),
		fmt.Sprintf("GRANT SELECT ON %s.pg_stat_replication TO %s", quotedUser, quotedUser),
	)
	return grants, nil
}

func userExists(ctx context.Context, db *sql.DB, user string) (bool, error) {
	count := 0
	err := db.QueryRowContext(ctx, "SELECT 1 FROM pg_roles WHERE rolname = $1", user).Scan(&count)
	switch {
	case err == sql.ErrNoRows:
		return false, nil
	case err != nil:
		return false, err
	case count == 0:
		// Shouldn't happen but just in case, if we get row and 0 value then user doesn't exists.
		return false, nil
	}
	return true, nil
}

func testConnection(ctx context.Context, dsn string) error {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return err
	}
	defer db.Close()

	if err = db.PingContext(ctx); err != nil {
		return err
	}

	return nil
}

func getInfo(ctx context.Context, db *sql.DB) (*plugin.Info, error) {
	info := &plugin.Info{}
	err := db.QueryRowContext(ctx, "SELECT inet_server_addr(), inet_server_port(), version()").Scan(&info.Hostname, &info.Port, &info.Version)
	if err != nil {
		return nil, err
	}
	info.Distro = "PostgreSQL"
	return info, nil
}