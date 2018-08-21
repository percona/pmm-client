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

package mongodb

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestInit(t *testing.T) {
	pmmBaseDir, err := ioutil.TempDir("/tmp", "pmm-client-test-rootdir-")
	assert.NoError(t, err)
	defer func() {
		err := os.RemoveAll(pmmBaseDir)
		assert.Nil(t, err)
	}()

	err = os.MkdirAll(pmmBaseDir, 0777)
	assert.NoError(t, err)
	f, _ := os.Create(filepath.Join(pmmBaseDir, "mongodb_exporter"))
	fmt.Fprintln(f, "#!/bin/sh")
	fmt.Fprintln(f, `cat << 'EOF'
{
  "Version": "3.4.12",
  "VersionArray": [
    3,
    4,
    12,
    0
  ],
  "GitVersion": "bfde702b19c1baad532ed183a871c12630c1bbba",
  "OpenSSLVersion": "",
  "SysInfo": "",
  "Bits": 64,
  "Debug": false,
  "MaxObjectSize": 16777216
}

EOF`)
	f.Close()
	err = os.Chmod(filepath.Join(pmmBaseDir, "mongodb_exporter"), 0777)
	assert.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	buildInfo, err := Init(ctx, "", []string{}, pmmBaseDir)
	assert.NoError(t, err)
	assert.NotEmpty(t, buildInfo)
}
