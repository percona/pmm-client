package metrics

import (
	"context"
	"fmt"

	"github.com/percona/pmm-client/pmm/plugin"
	"github.com/percona/pmm-client/pmm/plugin/mongodb"
	"github.com/percona/pmm-client/pmm/utils"
)

var _ plugin.Metrics = (*Metrics)(nil)

// New returns *Metrics.
func New(dsn string, args []string, cluster string, pmmBaseDir string) *Metrics {
	return &Metrics{
		dsn:        dsn,
		args:       args,
		cluster:    cluster,
		pmmBaseDir: pmmBaseDir,
	}
}

// Metrics implements plugin.Metrics.
type Metrics struct {
	dsn        string
	args       []string
	cluster    string
	pmmBaseDir string
}

// Init initializes plugin.
func (m *Metrics) Init(ctx context.Context, pmmUserPassword string) (*plugin.Info, error) {
	info, err := mongodb.Init(ctx, m.dsn, m.args, m.pmmBaseDir)
	if err != nil {
		return nil, err
	}
	m.dsn = info.DSN
	return info, nil
}

// Name of the exporter.
func (Metrics) Name() string {
	return "mongodb"
}

// DefaultPort returns default port.
func (Metrics) DefaultPort() int {
	return 42003
}

// Args is a list of additional arguments passed to exporter executable.
func (Metrics) Args() []string {
	return nil
}

// Environment is a list of additional environment variables passed to exporter executable.
func (m Metrics) Environment() []string {
	return []string{fmt.Sprintf("MONGODB_URI=%s", m.dsn)}
}

// Executable is a name of exporter executable under PMMBaseDir.
func (Metrics) Executable() string {
	return "mongodb_exporter"
}

// KV is a list of additional Key-Value data stored in consul.
func (m Metrics) KV() map[string][]byte {
	return map[string][]byte{
		"dsn": []byte(utils.SanitizeDSN(m.dsn)),
	}
}

// Cluster defines cluster name for the target.
func (m Metrics) Cluster() string {
	return m.cluster
}

// Multiple returns true if exporter can be added multiple times.
func (Metrics) Multiple() bool {
	return true
}
