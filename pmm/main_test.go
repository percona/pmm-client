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

func TestSanitizeDSN(t *testing.T) {
	uris := map[string]string{
		"mongodb://localhost:27017":                          "localhost:27017",
		"mongodb://admin:abc123@localhost:27017":             "admin:***@localhost:27017",
		"mongodb://admin:abc123@localhost":                   "admin:***@localhost",
		"mongodb://admin:abc123@localhost/database":          "admin:***@localhost/database",
		"mongodb://admin:abc123@localhost:27017/db?opt=true": "admin:***@localhost:27017/db",
		"admin:abc123@127.0.0.1:100":                         "admin:***@127.0.0.1:100",
		"localhost:27017/":                                   "localhost:27017",
		"localhost:27017?opt=5":                              "localhost:27017",
		"localhost":                                          "localhost",
		"admin:abc123@localhost:1,localhost:2":               "admin:***@localhost:1,localhost:2",
		"root:qwertyUIOP)(*&^%$#@1@localhost":                "root:***@localhost",
		"root:qwerty:UIOP)(*&^%$#@1@localhost":               "root:***@localhost",
	}

	for uri, expected := range uris {
		assert.Equal(t, expected, SanitizeDSN(uri), "uri = %s", uri)
	}
}

func TestIsValidSvcType(t *testing.T) {
	// check valid types
	for _, v := range svcTypes {
		assert.Nil(t, isValidSvcType(v))
	}

	// check invalid type
	assert.Error(t, isValidSvcType("invalid type"))
}
