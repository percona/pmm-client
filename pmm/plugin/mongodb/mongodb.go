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
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/percona/pmm-client/pmm/plugin"
	"gopkg.in/mgo.v2"
)

// Init verifies MongoDB connection.
func Init(ctx context.Context, uri string, args []string, pmmBaseDir string) (*plugin.Info, error) {
	path := fmt.Sprintf("%s/mongodb_exporter", pmmBaseDir)
	// Add additional args passed to pmm-admin
	args = append([]string{"--test"}, args...)
	cmd := exec.CommandContext(
		ctx,
		path,
		args...,
	)
	cmd.Env = append(os.Environ(), fmt.Sprintf("MONGODB_URI=%s", uri))

	b, err := cmd.CombinedOutput()
	if err != nil {
		err = fmt.Errorf("cannot verify MongoDB connection with `%s %s`: %s: %s", path, strings.Join(args, " "), err, string(b))
		return nil, err
	}
	buildInfo := mgo.BuildInfo{}
	err = json.Unmarshal(b, &buildInfo)
	if err != nil {
		err = fmt.Errorf("cannot read BuildInfo from output of `%s %s`: %s: %s", path, strings.Join(args, " "), err, string(b))
		return nil, err
	}

	info := &plugin.Info{
		Distro:  "MongoDB",
		Version: buildInfo.Version,
		DSN:     uri,
	}
	return info, nil
}
