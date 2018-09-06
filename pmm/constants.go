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
	"errors"
	"fmt"
	"time"
)

const (
	qanAPIBasePath = "qan-api"
	noMonitoring   = "No monitoring registered for this node identified as"
	apiTimeout     = 10 * time.Second
	NameRegex      = `^[-\w:\.]{2,60}$`
)

var (
	// you can use `-ldflags -X github.com/percona/pmm-client/pmm.Version=`
	// to set build version number
	Version = "1.14.1"

	// you can use `-ldflags -X github.com/percona/pmm-client/pmm.RootDir=`
	// to set root filesystem for pmm-admin
	RootDir = ""

	PMMBaseDir   = RootDir + "/usr/local/percona/pmm-client"
	AgentBaseDir = RootDir + "/usr/local/percona/qan-agent"

	ConfigFile  = fmt.Sprintf("%s/pmm.yml", PMMBaseDir)
	SSLCertFile = fmt.Sprintf("%s/server.crt", PMMBaseDir)
	SSLKeyFile  = fmt.Sprintf("%s/server.key", PMMBaseDir)

	ErrDuplicate  = errors.New("there is already one instance with this name under monitoring.")
	ErrNoService  = errors.New("no service found.")
	errNoInstance = errors.New("no instance found on QAN API.")
)
