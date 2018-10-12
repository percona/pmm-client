package metrics

import (
	"context"

	"github.com/percona/pmm-client/pmm/plugin"
)

var _ plugin.Metrics = (*Metrics)(nil)

// New returns *Metrics.
func New() *Metrics {
	return &Metrics{}
}

// Metrics implements plugin.Metrics.
type Metrics struct{}

// Init initializes plugin.
func (Metrics) Init(ctx context.Context, pmmUserPassword string) (*plugin.Info, error) {
	return &plugin.Info{}, nil
}

// Name of the exporter.
func (Metrics) Name() string {
	return "linux"
}

// DefaultPort returns default port.
func (Metrics) DefaultPort() int {
	return 42000
}

// Args is a list of additional arguments passed to exporter executable.
func (Metrics) Args() []string {
	return []string{
		"-collectors.enabled=diskstats,filefd,filesystem,loadavg,meminfo,netdev,netstat,stat,time,uname,vmstat,meminfo_numa,textfile",
	}
}

// Environment is a list of additional environment variables passed to exporter executable.
func (Metrics) Environment() []string {
	return nil
}

// Executable is a name of exporter executable under PMMBaseDir.
func (Metrics) Executable() string {
	return "node_exporter"
}

// KV is a list of additional Key-Value data stored in consul.
func (Metrics) KV() map[string][]byte {
	return nil
}

// Cluster defines cluster name for the target.
func (Metrics) Cluster() string {
	return ""
}

// Multiple returns true if exporter can be added multiple times.
func (Metrics) Multiple() bool {
	return false
}
