/*
   Copyright (c) 2016, Percona LLC and/or its affiliates. All rights reserved.

   This program is free software: you can redistribute it and/or modify
   it under the terms of the GNU Affero General Public License as published by
   the Free Software Foundation, either version 3 of the License, or
   (at your option) any later version.

   This program is distributed in the hope that it will be useful,
   but WITHOUT ANY WARRANTY; without even the implied warranty of
   MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
   GNU Affero General Public License for more details.

   You should have received a copy of the GNU Affero General Public License
   along with this program.  If not, see <http://www.gnu.org/licenses/>
*/

package qan

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	vcmp "github.com/hashicorp/go-version"
	pc "github.com/percona/pmm/proto/config"
	"github.com/percona/qan-agent/pct"
)

const (
	DEFAULT_COLLECT_FROM              = "slowlog"
	DEFAULT_INTERVAL                  = 60         // 1 minute
	DEFAULT_LONG_QUERY_TIME           = 0.001      // 1ms
	DEFAULT_MAX_SLOW_LOG_SIZE         = 1073741824 // 1G
	DEFAULT_REMOVE_OLD_SLOW_LOGS      = true
	DEFAULT_EXAMPLE_QUERIES           = true
	DEFAULT_SLOW_LOG_VERBOSITY        = "full" // all metrics, Percona Server
	DEFAULT_RATE_LIMIT                = 100    // 1%, Percona Server
	DEFAULT_LOG_SLOW_ADMIN_STATEMENTS = true   // Percona Server
	DEFAULT_LOG_SLOW_SLAVE_STATEMENTS = true   // Percona Server
	// internal
	DEFAULT_WORKER_RUNTIME = 55
	DEFAULT_REPORT_LIMIT   = 200
)

func ValidateConfig(setConfig map[string]string) (pc.QAN, error) {
	runConfig := pc.QAN{
		UUID:                    setConfig["UUID"],
		CollectFrom:             DEFAULT_COLLECT_FROM,
		Interval:                DEFAULT_INTERVAL,
		LongQueryTime:           DEFAULT_LONG_QUERY_TIME,
		MaxSlowLogSize:          DEFAULT_MAX_SLOW_LOG_SIZE,
		RemoveOldSlowLogs:       DEFAULT_REMOVE_OLD_SLOW_LOGS,
		ExampleQueries:          DEFAULT_EXAMPLE_QUERIES,
		SlowLogVerbosity:        DEFAULT_SLOW_LOG_VERBOSITY,
		RateLimit:               DEFAULT_RATE_LIMIT,
		LogSlowAdminStatements:  DEFAULT_LOG_SLOW_ADMIN_STATEMENTS,
		LogSlowSlaveStatemtents: DEFAULT_LOG_SLOW_SLAVE_STATEMENTS,
		WorkerRunTime:           DEFAULT_WORKER_RUNTIME,
		ReportLimit:             DEFAULT_REPORT_LIMIT,
	}

	// Strings

	if val, set := setConfig["CollectFrom"]; set {
		if val != "slowlog" && val != "perfschema" {
			return runConfig, fmt.Errorf("CollectFrom must be 'slowlog' or 'perfschema'")
		}
		runConfig.CollectFrom = val
	}

	if val, set := setConfig["SlowLogVerbosity"]; set {
		if val != "minimal" && val != "standard" && val != "full" {
			return runConfig, fmt.Errorf("CollectFrom must be 'minimal', 'standard', or 'full'")
		}
		runConfig.SlowLogVerbosity = val
	}

	// Integers

	if val, set := setConfig["Interval"]; set {
		n, err := strconv.ParseUint(val, 10, 32)
		if err != nil {
			return runConfig, fmt.Errorf("invalid Interval: '%s': %s", val, err)
		}
		if n < 0 || n > 3600 {
			return runConfig, fmt.Errorf("Interval must be > 0 and <= 3600 (1 hour)")
		}
		runConfig.Interval = uint(n)
	}
	runConfig.WorkerRunTime = uint(float64(runConfig.Interval) * 0.9) // 90% of interval

	if val, set := setConfig["MaxSlowLogSize"]; set {
		n, err := strconv.ParseInt(val, 10, 64)
		if err != nil {
			return runConfig, fmt.Errorf("invalid MaxSlowLogSize: '%s': %s", val, err)
		}
		if n < 0 {
			return runConfig, fmt.Errorf("MaxSlowLogSize must be > 0")
		}
		runConfig.MaxSlowLogSize = n
	}

	if val, set := setConfig["RateLimit"]; set {
		n, err := strconv.ParseUint(val, 10, 32)
		if err != nil {
			return runConfig, fmt.Errorf("invalid RateLimit: '%s': %s", val, err)
		}
		if n < 0 {
			return runConfig, fmt.Errorf("RateLimit must be > 0")
		}
		runConfig.RateLimit = uint(n)
	}

	// Floats

	if val, set := setConfig["LongQueryTime"]; set {
		n, err := strconv.ParseFloat(val, 64)
		if err != nil {
			return runConfig, fmt.Errorf("invalid LongQueryTime: '%s': %s", val, err)
		}
		if n < 0 || n < 0.000001 {
			return runConfig, fmt.Errorf("LongQueryTime must be > 0 and >= 0.000001")
		}
		runConfig.LongQueryTime = n
	}

	// Bools

	if val, set := setConfig["RemoveOldSlowLogs"]; set {
		runConfig.RemoveOldSlowLogs = pct.ToBool(val)
	}

	if val, set := setConfig["ExampleQueries"]; set {
		runConfig.ExampleQueries = pct.ToBool(val)
	}

	if val, set := setConfig["LogSlowAdminStatements"]; set {
		runConfig.LogSlowAdminStatements = pct.ToBool(val)
	}

	if val, set := setConfig["LogSlowSlaveStatemtents"]; set {
		runConfig.LogSlowSlaveStatemtents = pct.ToBool(val)
	}

	return runConfig, nil
}

var reCleanVersion = regexp.MustCompile("-.*$") // remove everything after first dash
var v5147, _ = vcmp.NewVersion("5.1.47")
var v5510, _ = vcmp.NewVersion("5.5.10")
var v5534, _ = vcmp.NewVersion("5.5.34")
var v5613, _ = vcmp.NewVersion("5.6.13")

func GetMySQLConfig(config pc.QAN, distro, version string) ([]string, []string, error) {
	distro = strings.TrimSpace(strings.ToLower(distro))

	version = strings.TrimSpace(strings.ToLower(version))
	version = reCleanVersion.ReplaceAllString(version, "")
	v, err := vcmp.NewVersion(version)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid version: '%s': %s", version, err)
	}

	switch config.CollectFrom {
	case "slowlog":
		return makeSlowLogConfig(config, distro, v)
	case "perfschema":
		return makePerfSchemaConfig(config, distro, v)
	default:
		return nil, nil, fmt.Errorf("invalid CollectFrom: '%s'; expected 'slowlog' or 'perfschema'", config.CollectFrom)
	}
}

func makeSlowLogConfig(config pc.QAN, distro string, v *vcmp.Version) ([]string, []string, error) {
	// Break "5.6.13" into major, minor, and patch numbers because some vars
	// were added mid-series, e.g. slow_query_log_always_write_time as of 5.5.34
	// but only as of 5.6.13 in the 5.6 series. So if we have 5.6.12, we can't
	// just check that v >= 5.5.34, we have to know we're 5.6 then check >= 5.6.13.
	mmp := v.Segments()
	series := fmt.Sprintf("%d.%d", mmp[0], mmp[1])

	on := []string{
		"SET GLOBAL slow_query_log=OFF",
		"SET GLOBAL log_output='file'", // as of MySQL 5.1.6
	}
	off := []string{
		"SET GLOBAL slow_query_log=OFF",
	}

	// If not running Percona Server, it's Oracle MySQL, MariaDB, or something
	// else like Homebrew (for Mac), so our only option is the simplest config.
	if distro != "percona server" {
		on = append(on,
			fmt.Sprintf("SET GLOBAL long_query_time=%f", config.LongQueryTime),
			"SET GLOBAL slow_query_log=ON",
		)
		return on, off, nil
	}

	// //////////////////////////////////////////////////////////////////////
	// Running Percona Server, enable all its features. See
	// https://docs.google.com/a/percona.com/document/d/1izDKRASJHtoEjTndWBy_lztUe_98SuAfHN1JUGEE6E4/edit?usp=sharing
	// //////////////////////////////////////////////////////////////////////

	// log_slow_rate_limit was introduced earlier, but it's not until 5.5.34
	// that the slow log contains "Log_slow_rate_limit: 1000", required for
	// agent to know data is sampled.
	if v.LessThan(v5534) {
		// version < 5.5.34
		on = append(on, fmt.Sprintf("SET GLOBAL long_query_time=%f", config.LongQueryTime))
	} else {
		// version >= 5.5.34
		on = append(on,
			"SET GLOBAL log_slow_rate_type='query'",
			fmt.Sprintf("SET GLOBAL log_slow_rate_limit=%d", config.RateLimit),
			"SET GLOBAL long_query_time=0",
		)
	}

	// Slow log verbosity controls exist as of 5.1.47.
	if !v.LessThan(v5147) {
		// version >= 5.1.47
		on = append(on, fmt.Sprintf("SET GLOBAL log_slow_verbosity='%s'", config.SlowLogVerbosity))

		if config.LogSlowAdminStatements {
			on = append(on, "SET GLOBAL log_slow_admin_statements=ON")
		} else {
			on = append(on, "SET GLOBAL log_slow_admin_statements=OFF")
		}

		if config.LogSlowSlaveStatemtents {
			on = append(on, "SET GLOBAL log_slow_slave_statements=ON")
		} else {
			on = append(on, "SET GLOBAL log_slow_slave_statements=OFF")
		}

		// This var changed in 5.5.10:
		if v.LessThan(v5510) {
			// version < 5.5.10
			on = append(on,
				"SET GLOBAL use_global_log_slow_control='all'",
			)
			off = append(off,
				"SET GLOBAL use_global_log_slow_control=''",
			)
		} else {
			// version >= 5.5.10
			on = append(on,
				"SET GLOBAL slow_query_log_use_global_control='all'",
			)
			off = append(off,
				"SET GLOBAL slow_query_log_use_global_control=''",
			)
		}
	}

	// slow_query_log_always_write_time as of 5.5.34 and 5.6.13, causes queries
	// to be logged regardless of all other settings/filters if the exec time is
	// greater than this value (in seconds).
	if (series == "5.5" && !v.LessThan(v5534)) || (series == "5.6" && !v.LessThan(v5613)) {
		on = append(on,
			"SET GLOBAL slow_query_log_always_write_time=1",
		)
		off = append(off,
			"SET GLOBAL slow_query_log_always_write_time=10", // effectively off
		)
	}

	on = append(on,
		"SET GLOBAL slow_query_log=ON",
	)

	return on, off, nil
}

func makePerfSchemaConfig(config pc.QAN, distro string, v *vcmp.Version) ([]string, []string, error) {
	// From the docs:
	// "[events_statements_summary_by_digest] was added in 5.6.5. Before MySQL 5.6.9,
	//  there is no SCHEMA_NAME column and grouping is based on DIGEST values only."
	on := []string{
		"UPDATE performance_schema.setup_consumers SET ENABLED = 'YES' WHERE NAME = 'statements_digest'",
		"UPDATE performance_schema.setup_instruments SET ENABLED = 'YES', TIMED = 'YES' WHERE NAME LIKE 'statement/sql/%'",
		//"TRUNCATE performance_schema.events_statements_summary_by_digest",
	}
	off := []string{
		"UPDATE performance_schema.setup_consumers SET ENABLED = 'NO' WHERE NAME = 'statements_digest'",
		"UPDATE performance_schema.setup_instruments SET ENABLED = 'NO', TIMED = 'NO' WHERE NAME LIKE 'statement/sql/%'",
	}
	return on, off, nil
}
