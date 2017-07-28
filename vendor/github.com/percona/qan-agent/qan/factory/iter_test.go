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

package factory_test

import (
	"testing"

	"github.com/percona/qan-agent/qan/factory"
	. "gopkg.in/check.v1"
)

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) { TestingT(t) }

type IterTestSuite struct {
}

var _ = Suite(&IterTestSuite{})

func (s *IterTestSuite) TestAbsDataFile(t *C) {
	// Test AbsDataFile. It is used to get an absolute path for a MySQL data file
	// like slow_query_log_file
	dataDir := "/home/somedir/"
	testFileName := factory.AbsDataFile(dataDir, "anotherdir")
	t.Check(testFileName, Equals, "/home/somedir/anotherdir")
}
