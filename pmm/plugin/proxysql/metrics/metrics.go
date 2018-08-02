package metrics

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/go-sql-driver/mysql"
	"github.com/percona/pmm-client/pmm/plugin"
	"github.com/percona/pmm-client/pmm/utils"
)

var _ plugin.Metrics = (*Metrics)(nil)

// New returns *Metrics.
func New(dsn string) *Metrics {
	return &Metrics{
		dsn: dsn,
	}
}

// Metrics implements plugin.Metrics.
type Metrics struct {
	dsn string
}

// Init initializes plugin.
func (m *Metrics) Init(ctx context.Context, pmmUserPassword string) (*plugin.Info, error) {
	dsn, err := mysql.ParseDSN(m.dsn)
	if err != nil {
		return nil, fmt.Errorf("Bad dsn %s: %s", m.dsn, err)
	}

	if err := testConnection(ctx, dsn.FormatDSN()); err != nil {
		return nil, fmt.Errorf("Cannot connect to ProxySQL using DSN %s: %s", m.dsn, err)
	}

	info := &plugin.Info{
		DSN: m.dsn,
	}
	return info, nil
}

// Name of the exporter.
func (Metrics) Name() string {
	return "proxysql"
}

// DefaultPort returns default port.
func (Metrics) DefaultPort() int {
	return 42004
}

// Args is a list of additional arguments passed to exporter executable.
func (Metrics) Args() []string {
	return nil
}

// Environment is a list of additional environment variables passed to exporter executable.
func (m Metrics) Environment() []string {
	return []string{
		fmt.Sprintf("DATA_SOURCE_NAME=%s", m.dsn),
	}
}

// Executable is a name of exporter executable under PMMBaseDir.
func (Metrics) Executable() string {
	return "proxysql_exporter"
}

// KV is a list of additional Key-Value data stored in consul.
func (m Metrics) KV() map[string][]byte {
	return map[string][]byte{
		"dsn": []byte(utils.SanitizeDSN(m.dsn)),
	}
}

// Cluster defines cluster name for the target.
func (Metrics) Cluster() string {
	return ""
}

// Multiple returns true if exporter can be added multiple times.
func (Metrics) Multiple() bool {
	return true
}

func testConnection(ctx context.Context, dsn string) error {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return err
	}
	defer db.Close()

	if err = db.PingContext(ctx); err != nil {
		return err
	}

	return nil
}
