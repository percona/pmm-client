package plugin

import (
	"context"
)

// Metrics is a common interface for all exporters.
type Metrics interface {
	// Init initializes plugin and returns Info about database.
	Init(ctx context.Context, pmmUserPassword string) (*Info, error)
	// Name of the exporter.
	// As the time of writing this is limited to linux, mysql, mongodb, proxysql and postgresql.
	Name() string
	// Args is a list of additional arguments passed to exporter executable.
	Args() []string
	// Environment is a list of additional environment variables passed to exporter executable.
	Environment() []string
	// Executable is a name of exporter executable under PMMBaseDir.
	Executable() string
	// KV is a list of additional Key-Value data stored in consul.
	KV() map[string][]byte
	// Cluster defines cluster name for the target.
	Cluster() string
	// Multiple returns true if exporter can be added multiple times.
	Multiple() bool
	// DefaultPort returns default port.
	DefaultPort() int
}
