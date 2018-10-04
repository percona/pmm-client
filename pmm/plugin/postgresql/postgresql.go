package postgresql

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os/exec"
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

	var errs errs

	// Test access using detected credentials and stored password.
	accessOK := false
	if pmmUserPassword != "" {
		pmmDSN := userDSN
		pmmDSN.User = "pmm"
		pmmDSN.Password = pmmUserPassword
		if err := testConnection(ctx, pmmDSN.String()); err != nil {
			errs = append(errs, err)
		} else {
			userDSN = pmmDSN
			accessOK = true
		}
	}

	// If the above fails, test PostgreSQL access simply using detected credentials.
	if !accessOK {
		if err := testConnection(ctx, userDSN.String()); err != nil {
			errs = append(errs, err)
		} else {
			accessOK = true
		}
	}

	// If the above fails, try to create `pmm` user with `sudo -u postgres psql`.
	if !accessOK {
		// If PostgreSQL server is local and --create-user flag is specified
		// then try to create user using `sudo -u postgres psql` and use that connection.
		if userDSN.Host == "" && flags.CreateUser {
			pmmDSN, err := createUserUsingSudoPSQL(ctx, userDSN, flags)
			if err != nil {
				errs = append(errs, fmt.Errorf("Cannot create user: %s", err))
			} else {
				errs = nil
				if err := testConnection(ctx, userDSN.String()); err != nil {
					errs = append(errs, err)
				} else {
					userDSN = pmmDSN
					accessOK = true
				}
			}
		}
	}

	// At this point access is required.
	if !accessOK {
		err := fmt.Errorf("Cannot connect to PostgreSQL:%s\n\n%s\n%s", errs,
			"Verify that PostgreSQL user exists and has the correct privileges.",
			"Use additional flags --user, --password, --host, --port if needed.")
		return nil, err
	}

	// Get PostgreSQL connection.
	db, err := sql.Open("postgres", userDSN.String())
	if err != nil {
		return nil, err
	}
	defer db.Close()

	// Get PostgreSQL variables.
	info, err := getInfo(ctx, db)
	if err != nil {
		return nil, err
	}

	// Create a new PostgreSQL user.
	if userDSN.User != "pmm" && flags.CreateUser {
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

func createUserUsingSudoPSQL(ctx context.Context, userDSN DSN, flags Flags) (DSN, error) {
	// New DSN has same host:port or socket, but different user and pass.
	userDSN.User = "pmm"

	// Check if user exists.
	userExists, err := userExistsCheckUsingSudoPSQL(ctx, userDSN.User)
	if err != nil {
		return DSN{}, err
	}
	if userExists && !flags.Force {
		var errMsg []string
		errMsg = append(errMsg, fmt.Sprintf("* PostgreSQL user %s already exists. %s", userDSN.User,
			"Try without --create-user flag using the default credentials or specify the existing `pmm` user ones."))
		errMsg = append([]string{"Problem creating a new PostgreSQL user:", ""}, errMsg...)
		errMsg = append(errMsg, "", "If you think the above is okay to proceed, you can use --force flag.")
		return DSN{}, errors.New(strings.Join(errMsg, "\n"))
	}

	// Check if schema exists.
	schemaExists, err := schemaExistsCheckUsingSudoPSQL(ctx, userDSN.User)
	if err != nil {
		return DSN{}, err
	}

	// Check for existing password or generate new one.
	if flags.CreateUserPassword != "" {
		userDSN.Password = flags.CreateUserPassword
	} else {
		userDSN.Password = utils.GeneratePassword(20)
	}

	grants := makeGrants(userDSN, userExists, schemaExists)
	for _, grant := range grants {
		cmd := exec.CommandContext(
			ctx,
			"sudo",
			"-u", "postgres", "psql", "postgres", "-tAc", grant,
		)

		b, err := cmd.CombinedOutput()
		if err != nil {
			return DSN{}, fmt.Errorf("cannot create user: %s: %s", err, string(b))
		}
	}

	// Verify new PostgreSQL user works. If this fails, the new DSN or grant statements are wrong.
	if err := testConnection(ctx, userDSN.String()); err != nil {
		return DSN{}, fmt.Errorf("Problem creating a new PostgreSQL user. Insufficient privileges: %s", err)
	}

	return userDSN, nil
}

func createUser(ctx context.Context, db *sql.DB, userDSN DSN, flags Flags) (DSN, error) {
	// New DSN has same host:port or socket, but different user and pass.
	userDSN.User = "pmm"
	if flags.CreateUserPassword != "" {
		userDSN.Password = flags.CreateUserPassword
	} else {
		userDSN.Password = utils.GeneratePassword(20)
	}

	// Check if user exists.
	userExists, err := userExists(ctx, db, userDSN.User)
	if err != nil {
		return DSN{}, err
	}
	if userExists && !flags.Force {
		var errMsg []string
		errMsg = append(errMsg, fmt.Sprintf("* PostgreSQL user %s already exists. %s", userDSN.User,
			"Try without --create-user flag using the default credentials or specify the existing `pmm` user ones."))
		errMsg = append([]string{"Problem creating a new PostgreSQL user:", ""}, errMsg...)
		errMsg = append(errMsg, "", "If you think the above is okay to proceed, you can use --force flag.")
		return DSN{}, errors.New(strings.Join(errMsg, "\n"))
	}

	// Check if schema exists.
	schemaExists, err := schemaExists(ctx, db, userDSN.User)
	if err != nil {
		return DSN{}, err
	}

	// Create a new PostgreSQL user with the necessary privileges.
	grants := makeGrants(userDSN, userExists, schemaExists)
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

// makeGrants generates queries that will allow to scrape metrics as non-root user.
func makeGrants(dsn DSN, userExists bool, schemaExists bool) []string {
	var grants []string
	quotedUser := pq.QuoteIdentifier(dsn.User)

	query := ""
	if userExists {
		query = fmt.Sprintf("ALTER USER %s WITH PASSWORD '%s'", quotedUser, dsn.Password)
	} else {
		query = fmt.Sprintf("CREATE USER %s WITH PASSWORD '%s'", quotedUser, dsn.Password)
	}
	grants = append(grants, query)

	if !schemaExists {
		query := fmt.Sprintf("CREATE SCHEMA %s AUTHORIZATION %s", quotedUser, quotedUser)
		grants = append(grants, query)
	}

	grants = append(grants,
		fmt.Sprintf("ALTER USER %s SET SEARCH_PATH TO %s,pg_catalog", quotedUser, quotedUser),
		fmt.Sprintf("CREATE OR REPLACE VIEW %s.pg_stat_activity AS SELECT * from pg_catalog.pg_stat_activity", quotedUser),
		fmt.Sprintf("GRANT SELECT ON %s.pg_stat_activity TO %s", quotedUser, quotedUser),
		fmt.Sprintf("CREATE OR REPLACE VIEW %s.pg_stat_replication AS SELECT * from pg_catalog.pg_stat_replication", quotedUser),
		fmt.Sprintf("GRANT SELECT ON %s.pg_stat_replication TO %s", quotedUser, quotedUser),
	)
	return grants
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

func schemaExists(ctx context.Context, db *sql.DB, user string) (bool, error) {
	count := 0
	err := db.QueryRowContext(ctx, "SELECT 1 FROM pg_namespace WHERE nspname = $1", user).Scan(&count)
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

func userExistsCheckUsingSudoPSQL(ctx context.Context, user string) (bool, error) {
	cmd := exec.CommandContext(
		ctx,
		"sudo",
		"-u", "postgres",
		"psql", "postgres", "-tAc", fmt.Sprintf("SELECT 1 FROM pg_roles WHERE rolname = '%s'", user),
	)
	b, err := cmd.Output()
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			b = append(b, exitError.Stderr...)
		}
		return false, fmt.Errorf("cannot check if user exists: %s: %s", err, string(b))
	}
	if bytes.HasPrefix(b, []byte("1")) {
		return true, nil
	}
	return false, nil
}

func schemaExistsCheckUsingSudoPSQL(ctx context.Context, schema string) (bool, error) {
	cmd := exec.CommandContext(
		ctx,
		"sudo",
		"-u", "postgres",
		"psql", "postgres", "-tAc", fmt.Sprintf("SELECT 1 FROM pg_namespace WHERE nspname = '%s'", schema),
	)
	b, err := cmd.Output()
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			b = append(b, exitError.Stderr...)
		}
		return false, fmt.Errorf("cannot check if schema exists: %s: %s", err, string(b))
	}
	if bytes.HasPrefix(b, []byte("1")) {
		return true, nil
	}
	return false, nil
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

type errs []error

func (errs errs) Error() string {
	if len(errs) == 0 {
		return ""
	}
	buf := &bytes.Buffer{}
	for _, err := range errs {
		fmt.Fprintf(buf, "\n* %s", err)
	}
	return buf.String()
}
