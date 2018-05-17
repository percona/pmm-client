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

package pmm

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetMySQLQueriesOptionsForPerfschema(t *testing.T) {
	config, err := getProtoQAN("testdata/qan-2b6c3eb3669943c160502874036968ba.conf")
	assert.NoError(t, err)
	opts := getMySQLQueriesOptions(config)
	assert.Empty(t, opts)
}

func TestGetMySQLQueriesOptions(t *testing.T) {
	config, err := getProtoQAN("testdata/qan-2b6c3eb3669943c160502874036968bb.conf")
	assert.NoError(t, err)
	opts := getMySQLQueriesOptions(config)
	expected := []string{
		"slow_log_rotation=true",
		"retain_slow_logs=23",
	}
	assert.Equal(t, expected, opts)
}
