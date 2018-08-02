package queries

import (
	"context"

	"github.com/percona/pmm-client/pmm/plugin"
	"github.com/percona/pmm-client/pmm/plugin/mongodb"
	pc "github.com/percona/pmm/proto/config"
)

var _ plugin.Queries = (*Queries)(nil)

// New returns *Queries.
func New(queriesFlags plugin.QueriesFlags, dsn string, args []string, pmmBaseDir string) *Queries {
	return &Queries{
		queriesFlags: queriesFlags,
		dsn:          dsn,
		args:         args,
		pmmBaseDir:   pmmBaseDir,
	}
}

// Queries implements plugin.Queries.
type Queries struct {
	queriesFlags plugin.QueriesFlags
	dsn          string
	args         []string
	pmmBaseDir   string
}

// Init initializes plugin.
func (m *Queries) Init(ctx context.Context, pmmUserPassword string) (*plugin.Info, error) {
	info, err := mongodb.Init(ctx, m.dsn, m.args, m.pmmBaseDir)
	if err != nil {
		return nil, err
	}
	m.dsn = info.DSN
	return info, nil
}

// Name of the service.
func (m Queries) Name() string {
	return "mongodb"
}

// InstanceTypeName of the service.
// Deprecated: QAN API should use `mongodb` not `mongo`.
func (m Queries) InstanceTypeName() string {
	return "mongo"
}

// Config returns pc.QAN.
func (m Queries) Config() pc.QAN {
	exampleQueries := !m.queriesFlags.DisableQueryExamples
	return pc.QAN{
		ExampleQueries: &exampleQueries,
	}
}
