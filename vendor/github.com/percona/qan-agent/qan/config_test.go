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

package qan_test

import (
	"github.com/percona/qan-agent/qan"
	. "gopkg.in/check.v1"
)

type ConfigTestSuite struct{}

var _ = Suite(&ConfigTestSuite{})

func (s *ConfigTestSuite) TestSlowLogMySQLBasic(t *C) {
	on, off, err := qan.MakeConfig(
		"MySQL",
		"5.6.24",
		"slowlog",
	)
	t.Assert(err, IsNil)
	t.Check(on, DeepEquals, []string{
		"SET GLOBAL slow_query_log=OFF",
		"SET GLOBAL log_output='file'",
		"SET GLOBAL long_query_time=0.001",
		"SET GLOBAL slow_query_log=ON",
	})
	t.Check(off, DeepEquals, []string{
		"SET GLOBAL slow_query_log=OFF",
	})
}

func (s *ConfigTestSuite) TestSlowLogPerconaBasic5625(t *C) {
	on, off, err := qan.MakeConfig(
		"Percona Server",
		"5.6.25",
		"slowlog",
	)
	t.Assert(err, IsNil)
	t.Check(on, DeepEquals, []string{
		"SET GLOBAL slow_query_log=OFF",
		"SET GLOBAL log_output='file'",
		"SET GLOBAL log_slow_rate_type='query'",
		"SET GLOBAL log_slow_rate_limit=100",
		"SET GLOBAL long_query_time=0",
		"SET GLOBAL log_slow_verbosity='full'",
		"SET GLOBAL log_slow_admin_statements=ON",
		"SET GLOBAL log_slow_slave_statements=ON",
		"SET GLOBAL slow_query_log_use_global_control='all'",
		"SET GLOBAL slow_query_log_always_write_time=1",
		"SET GLOBAL slow_query_log=ON",
	})
	t.Check(off, DeepEquals, []string{
		"SET GLOBAL slow_query_log=OFF",
		"SET GLOBAL slow_query_log_use_global_control=''",
		"SET GLOBAL slow_query_log_always_write_time=10",
	})
}

func (s *ConfigTestSuite) TestSlowLogPerconaBasic5534(t *C) {
	on, off, err := qan.MakeConfig(
		"Percona Server",
		"5.5.34",
		"slowlog",
	)
	t.Assert(err, IsNil)
	t.Check(on, DeepEquals, []string{
		"SET GLOBAL slow_query_log=OFF",
		"SET GLOBAL log_output='file'",
		"SET GLOBAL log_slow_rate_type='query'",
		"SET GLOBAL log_slow_rate_limit=100",
		"SET GLOBAL long_query_time=0",
		"SET GLOBAL log_slow_verbosity='full'",
		"SET GLOBAL log_slow_admin_statements=ON",
		"SET GLOBAL log_slow_slave_statements=ON",
		"SET GLOBAL slow_query_log_use_global_control='all'",
		"SET GLOBAL slow_query_log_always_write_time=1",
		"SET GLOBAL slow_query_log=ON",
	})
	t.Check(off, DeepEquals, []string{
		"SET GLOBAL slow_query_log=OFF",
		"SET GLOBAL slow_query_log_use_global_control=''",
		"SET GLOBAL slow_query_log_always_write_time=10",
	})
}

func (s *ConfigTestSuite) TestSlowLogPerconaBasic5510(t *C) {
	on, off, err := qan.MakeConfig(
		"Percona Server",
		"5.5.10",
		"slowlog",
	)
	t.Assert(err, IsNil)
	t.Check(on, DeepEquals, []string{
		"SET GLOBAL slow_query_log=OFF",
		"SET GLOBAL log_output='file'",
		"SET GLOBAL long_query_time=0.001", // no rate limit, requires 5.5.34+
		"SET GLOBAL log_slow_verbosity='full'",
		"SET GLOBAL log_slow_admin_statements=ON",
		"SET GLOBAL log_slow_slave_statements=ON",
		"SET GLOBAL slow_query_log_use_global_control='all'", // new var
		"SET GLOBAL slow_query_log=ON",
	})
	t.Check(off, DeepEquals, []string{
		"SET GLOBAL slow_query_log=OFF",
		"SET GLOBAL slow_query_log_use_global_control=''", // new var
	})
}

func (s *ConfigTestSuite) TestSlowLogPerconaBasic5147(t *C) {
	on, off, err := qan.MakeConfig(
		"Percona Server",
		"5.1.47",
		"slowlog",
	)
	t.Assert(err, IsNil)
	t.Check(on, DeepEquals, []string{
		"SET GLOBAL slow_query_log=OFF",
		"SET GLOBAL log_output='file'",
		"SET GLOBAL long_query_time=0.001", // no rate limit, requires 5.5.34+
		"SET GLOBAL log_slow_verbosity='full'",
		"SET GLOBAL log_slow_admin_statements=ON",
		"SET GLOBAL log_slow_slave_statements=ON",
		"SET GLOBAL use_global_log_slow_control='all'", // old var
		"SET GLOBAL slow_query_log=ON",
	})
	t.Check(off, DeepEquals, []string{
		"SET GLOBAL slow_query_log=OFF",
		"SET GLOBAL use_global_log_slow_control=''", // old var
	})
}

func (s *ConfigTestSuite) TestSlowLogPerconaBasic5612(t *C) {
	on, off, err := qan.MakeConfig(
		"Percona Server",
		"5.6.12",
		"slowlog",
	)
	t.Assert(err, IsNil)
	t.Check(on, DeepEquals, []string{
		"SET GLOBAL slow_query_log=OFF",
		"SET GLOBAL log_output='file'",
		"SET GLOBAL log_slow_rate_type='query'",
		"SET GLOBAL log_slow_rate_limit=100",
		"SET GLOBAL long_query_time=0",
		"SET GLOBAL log_slow_verbosity='full'",
		"SET GLOBAL log_slow_admin_statements=ON",
		"SET GLOBAL log_slow_slave_statements=ON",
		"SET GLOBAL slow_query_log_use_global_control='all'",
		//"SET GLOBAL slow_query_log_always_write_time=1", // not until 5.6.13
		"SET GLOBAL slow_query_log=ON",
	})
	t.Check(off, DeepEquals, []string{
		"SET GLOBAL slow_query_log=OFF",
		"SET GLOBAL slow_query_log_use_global_control=''",
		//"SET GLOBAL slow_query_log_always_write_time=10", // not until 5.6.13
	})
}
