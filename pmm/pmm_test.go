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

func TestIsValidSvcType(t *testing.T) {
	// check valid types
	for _, v := range svcTypes {
		assert.Nil(t, isValidSvcType(v))
	}

	// check invalid type
	assert.Error(t, isValidSvcType("invalid type"))
}
