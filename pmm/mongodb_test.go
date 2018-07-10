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
	"context"
	"io/ioutil"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestAdmin_DetectMongoDB(t *testing.T) {
	rootDir, err := ioutil.TempDir("/tmp", "pmm-client-test-rootdir-")
	assert.Nil(t, err)
	defer func() {
		err := os.RemoveAll(rootDir)
		assert.Nil(t, err)
	}()

	os.MkdirAll(rootDir+PMMBaseDir, 0777)
	f, _ := os.Create(rootDir + PMMBaseDir + "/mongodb_exporter")
	f.WriteString("#!/bin/sh\n")
	f.WriteString(`cat << 'EOF'
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

EOF
`)
	f.Close()
	os.Chmod(rootDir+PMMBaseDir+"/mongodb_exporter", 0777)
	PMMBaseDir = rootDir + PMMBaseDir

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	admin := Admin{}
	buildInfo, err := admin.DetectMongoDB(ctx, "")
	assert.Nil(t, err)
	assert.NotEmpty(t, buildInfo)
}
