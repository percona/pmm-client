package plugin

import (
	"context"

	pc "github.com/percona/pmm/proto/config"
)

// QueriesFlags Queries specific flags.
type QueriesFlags struct {
	DisableQueryExamples bool
}

// Queries is a common interface for all Query Analytics plugins.
type Queries interface {
	// Init initializes plugin and returns Info about database.
	Init(ctx context.Context, pmmUserPassword string) (*Info, error)
	// Name of the queries.
	// As the time of writing this is limited to mysql and mongodb.
	Name() string
	// InstanceTypeName returns name of instance type used by QAN API.
	// Deprecated: QAN API should be modified and use same value as Name().
	InstanceTypeName() string
	// Config returns pc.QAN, this allows for additional configuration of QAN.
	Config() pc.QAN
}
