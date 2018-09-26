package metrics

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/percona/pmm-client/pmm/plugin"
	"github.com/percona/pmm-client/pmm/plugin/mysql"
	"github.com/percona/pmm-client/pmm/utils"
)

var _ plugin.Metrics = (*Metrics)(nil)

// Flags are Metrics Metrics specific flags.
type Flags struct {
	DisableTableStats      bool
	DisableTableStatsLimit uint16
	DisableUserStats       bool
	DisableBinlogStats     bool
	DisableProcesslist     bool
}

// New returns *Metrics.
func New(flags Flags, mysqlFlags mysql.Flags) *Metrics {
	return &Metrics{
		flags:      flags,
		mysqlFlags: mysqlFlags,
	}
}

// Metrics implements plugin.Metrics.
type Metrics struct {
	flags      Flags
	mysqlFlags mysql.Flags

	dsn           string
	optsToDisable []string
}

// Init initializes plugin.
func (m *Metrics) Init(ctx context.Context, pmmUserPassword string) (*plugin.Info, error) {
	info, err := mysql.Init(ctx, m.mysqlFlags, pmmUserPassword)
	if err != nil {
		return nil, err
	}
	m.dsn = info.DSN

	m.optsToDisable, err = optsToDisable(ctx, m.dsn, m.flags)
	if err != nil {
		return nil, err
	}

	return info, nil
}

// Name of the exporter.
func (m Metrics) Name() string {
	return "mysql"
}

// DefaultPort returns default port.
func (m Metrics) DefaultPort() int {
	return 42002
}

// Args is a list of additional arguments passed to exporter executable.
func (m Metrics) Args() []string {
	var defaultArgs = []string{
		"-collect.auto_increment.columns=true",
		"-collect.binlog_size=true",
		"-collect.global_status=true",
		"-collect.global_variables=true",
		"-collect.info_schema.innodb_metrics=true",
		"-collect.info_schema.innodb_cmp=true",
		"-collect.info_schema.innodb_cmpmem=true",
		"-collect.info_schema.processlist=true",
		"-collect.info_schema.query_response_time=true",
		"-collect.info_schema.tables=true",
		"-collect.info_schema.tablestats=true",
		"-collect.info_schema.userstats=true",
		"-collect.perf_schema.eventswaits=true",
		"-collect.perf_schema.file_events=true",
		"-collect.perf_schema.indexiowaits=true",
		"-collect.perf_schema.tableiowaits=true",
		"-collect.perf_schema.tablelocks=true",
		"-collect.slave_status=true",
		//"-collect.engine_innodb_status=true",
		//"-collect.engine_tokudb_status=true",
		//"-collect.info_schema.clientstats=true",
		//"-collect.info_schema.innodb_tablespaces=true",
		//"-collect.perf_schema.eventsstatements=true",
	}

	// disableArgs is a list of optional pmm-admin args to disable mysqld_exporter args.
	var disableArgs = map[string][]string{
		"tablestats": {
			"-collect.auto_increment.columns=",
			"-collect.info_schema.tables=",
			"-collect.info_schema.tablestats=",
			"-collect.perf_schema.indexiowaits=",
			"-collect.perf_schema.tableiowaits=",
			"-collect.perf_schema.tablelocks=",
		},
		"userstats":   {"-collect.info_schema.userstats="},
		"binlogstats": {"-collect.binlog_size="},
		"processlist": {"-collect.info_schema.processlist="},
	}

	// Disable exporter options if set so.
	args := defaultArgs
	for _, o := range m.optsToDisable {
		for _, f := range disableArgs[o] {
			for i, a := range defaultArgs {
				if strings.HasPrefix(a, f) {
					args[i] = fmt.Sprintf("%sfalse", f)
					break
				}
			}
		}
	}
	return args
}

// Environment is a list of additional environment variables passed to exporter executable.
func (m Metrics) Environment() []string {
	return []string{
		fmt.Sprintf("DATA_SOURCE_NAME=%s", m.dsn),
	}
}

// Executable is a name of exporter executable under PMMBaseDir.
func (m Metrics) Executable() string {
	return "mysqld_exporter"
}

// KV is a list of additional Key-Value data stored in consul.
func (m Metrics) KV() map[string][]byte {
	kv := map[string][]byte{}
	kv["dsn"] = []byte(utils.SanitizeDSN(m.dsn))
	for _, o := range m.optsToDisable {
		kv[o] = []byte("OFF")
	}
	return kv
}

// Cluster defines cluster name for the target.
func (m Metrics) Cluster() string {
	return ""
}

// Multiple returns true if exporter can be added multiple times.
func (m Metrics) Multiple() bool {
	return true
}

func optsToDisable(ctx context.Context, dsn string, flags Flags) ([]string, error) {
	// Opts to disable.
	var optsToDisable []string
	if !flags.DisableTableStats {
		tableCount, err := tableCount(ctx, dsn)
		if err != nil {
			return nil, err
		}
		// Disable table stats if number of tables is higher than limit.
		if uint16(tableCount) > flags.DisableTableStatsLimit {
			flags.DisableTableStats = true
		}
	}
	if flags.DisableTableStats {
		optsToDisable = append(optsToDisable, "tablestats")
	}
	if flags.DisableUserStats {
		optsToDisable = append(optsToDisable, "userstats")
	}
	if flags.DisableBinlogStats {
		optsToDisable = append(optsToDisable, "binlogstats")
	}
	if flags.DisableProcesslist {
		optsToDisable = append(optsToDisable, "processlist")
	}

	return optsToDisable, nil
}

func tableCount(ctx context.Context, dsn string) (int, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return 0, err
	}
	defer db.Close()

	tableCount := 0
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.tables").Scan(&tableCount)
	return tableCount, err
}
