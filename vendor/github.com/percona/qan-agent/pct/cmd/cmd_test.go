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

package cmd_test

import (
	"github.com/percona/qan-agent/pct/cmd"
	. "gopkg.in/check.v1"
	"testing"
)

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) { TestingT(t) }

type TestSuite struct {
}

var _ = Suite(&TestSuite{})

// --------------------------------------------------------------------------

func (s *TestSuite) TestCmdNotFound(t *C) {
	unknownCmd := cmd.NewRealCmd("unknown-cmd")
	output, err := unknownCmd.Run()
	t.Assert(output, Equals, "")
	t.Assert(err, Equals, cmd.ErrNotFound)
}
