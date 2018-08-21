package queries

import (
	"context"
	"os"

	"github.com/percona/pmm-client/pmm/plugin"
	"github.com/percona/pmm-client/pmm/plugin/mysql"
	pc "github.com/percona/pmm/proto/config"
)

var _ plugin.Queries = (*Queries)(nil)

// Flags are MySQL Queries specific flags.
type Flags struct {
	QuerySource string
	// slowlog specific options.
	RetainSlowLogs  int
	SlowLogRotation bool
}

// New returns *Queries.
func New(queriesFlags plugin.QueriesFlags, flags Flags, mysqlFlags mysql.Flags) *Queries {
	return &Queries{
		queriesFlags: queriesFlags,
		flags:        flags,
		mysqlFlags:   mysqlFlags,
	}
}

// Queries implements plugin.Queries.
type Queries struct {
	queriesFlags plugin.QueriesFlags
	flags        Flags
	mysqlFlags   mysql.Flags
}

// Init initializes plugin.
func (m *Queries) Init(ctx context.Context, pmmUserPassword string) (*plugin.Info, error) {
	info, err := mysql.Init(ctx, m.mysqlFlags, pmmUserPassword)
	if err != nil {
		return nil, err
	}

	if m.flags.QuerySource == "auto" {
		// MySQL is local if the server hostname == MySQL hostname.
		osHostname, _ := os.Hostname()
		if osHostname == info.Hostname {
			m.flags.QuerySource = "slowlog"
		} else {
			m.flags.QuerySource = "perfschema"
		}
	}

	info.QuerySource = m.flags.QuerySource
	return info, nil
}

// Name of the service.
func (m Queries) Name() string {
	return "mysql"
}

// InstanceTypeName of the service.
// Deprecated: QAN API should use the same value as Name().
func (m Queries) InstanceTypeName() string {
	return m.Name()
}

// Config returns pc.QAN.
func (m Queries) Config() pc.QAN {
	exampleQueries := !m.queriesFlags.DisableQueryExamples
	return pc.QAN{
		CollectFrom:    m.flags.QuerySource,
		Interval:       60,
		ExampleQueries: &exampleQueries,
		// "slowlog" specific options.
		SlowLogRotation: &m.flags.SlowLogRotation,
		RetainSlowLogs:  &m.flags.RetainSlowLogs,
	}
}
