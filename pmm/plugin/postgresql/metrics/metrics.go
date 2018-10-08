package metrics

import (
	"context"
	"fmt"

	"github.com/percona/pmm-client/pmm/plugin"
	"github.com/percona/pmm-client/pmm/plugin/postgresql"
	"github.com/percona/pmm-client/pmm/utils"
)

var _ plugin.Metrics = (*Metrics)(nil)

// New returns *Metrics.
func New(flags postgresql.Flags) *Metrics {
	return &Metrics{
		postgresqlFlags: flags,
	}
}

// Metrics implements plugin.Metrics.
type Metrics struct {
	postgresqlFlags postgresql.Flags

	dsn string
}

// Init initializes plugin.
func (m *Metrics) Init(ctx context.Context, pmmUserPassword string) (*plugin.Info, error) {
	info, err := postgresql.Init(ctx, m.postgresqlFlags, pmmUserPassword)
	if err != nil {
		err = fmt.Errorf("%s\n\n"+
			"It looks like we were unable to connect to your PostgreSQL server.\n"+
			"Please see the PMM FAQ for additional troubleshooting steps: https://www.percona.com/doc/percona-monitoring-and-management/faq.html", err)
		return nil, err
	}
	m.dsn = info.DSN
	return info, nil
}

// Name of the exporter.
func (m Metrics) Name() string {
	return "postgresql"
}

// DefaultPort returns default port.
func (m Metrics) DefaultPort() int {
	return 42005
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
func (m Metrics) Executable() string {
	return "postgres_exporter"
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
