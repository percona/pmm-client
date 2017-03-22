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

package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/url"
	"os"
	"os/exec"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/percona/pmm-client/pmm"
	"github.com/percona/pmm-client/test/cmdtest"
	"github.com/percona/pmm-client/test/fakeapi"
	"github.com/percona/pmm/proto"
	"github.com/stretchr/testify/assert"
)

type pmmAdminData struct {
	bin     string
	basedir string
}

func TestPmmAdmin(t *testing.T) {
	var err error

	// We can't/shouldn't use /usr/local/percona/ (the default basedir), so use
	// a tmpdir instead with roughly the same structure.
	basedir, err := ioutil.TempDir("/tmp", "pmm-client-test-basedir-")
	assert.Nil(t, err)
	defer func() {
		err := os.RemoveAll(basedir)
		assert.Nil(t, err)
	}()

	bindir, err := ioutil.TempDir("/tmp", "pmm-client-test-bindir-")
	assert.Nil(t, err)
	defer func() {
		err := os.RemoveAll(bindir)
		assert.Nil(t, err)
	}()

	bin := bindir + "/pmm-admin"
	pmmBaseDir := basedir + "/pmm-client"
	agentBaseDir := basedir + "/qan-agent"
	xVariables := map[string]string{
		"github.com/percona/pmm-client/pmm.Version":      "gotest",
		"github.com/percona/pmm-client/pmm.PMMBaseDir":   pmmBaseDir,
		"github.com/percona/pmm-client/pmm.AgentBaseDir": agentBaseDir,
	}
	var ldflags []string
	for x, value := range xVariables {
		ldflags = append(ldflags, fmt.Sprintf("-X %s=%s", x, value))
	}
	cmd := exec.Command(
		"go",
		"build",
		"-o",
		bin,
		"-ldflags",
		strings.Join(ldflags, " "),
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	assert.Nil(t, err, "Failed to build: %s", err)

	data := pmmAdminData{
		bin:     bin,
		basedir: basedir,
	}
	tests := []func(*testing.T, pmmAdminData){
		testVersion,
		testConfig,
		testMongoDB,
		testMongoDBQueries,
	}
	t.Run("pmm-admin", func(t *testing.T) {
		for _, f := range tests {
			f := f // capture range variable
			fName := runtime.FuncForPC(reflect.ValueOf(f).Pointer()).Name()
			t.Run(fName, func(t *testing.T) {
				// t.Parallel()
				f(t, data)
			})
		}
	})

}

func testVersion(t *testing.T, data pmmAdminData) {
	defer func() {
		err := os.RemoveAll(data.basedir)
		assert.Nil(t, err)
	}()

	cmd := exec.Command(
		data.bin,
		"--version",
	)

	cmdTest := cmdtest.New(cmd)
	if err := cmd.Start(); err != nil {
		log.Fatal(err)
	}
	err := cmd.Wait()
	assert.Nil(t, err)

	// sanity check that version number was changed with ldflag for this test build
	assert.Equal(t, "EXPERIMENTAL", pmm.Version)
	assert.Equal(t, "gotest\n", cmdTest.ReadLine())

	assert.Equal(t, "", cmdTest.ReadLine()) // No more data
}

func testConfig(t *testing.T, data pmmAdminData) {
	defer func() {
		err := os.RemoveAll(data.basedir)
		assert.Nil(t, err)
	}()

	os.MkdirAll(data.basedir+"/pmm-client", 0777)

	// Create fake api server
	api := fakeapi.New()
	u, _ := url.Parse(api.URL())
	clientAddress, _, _ := net.SplitHostPort(u.Host)
	clientName, _ := os.Hostname()
	api.AppendRoot()
	api.AppendConsulV1StatusLeader(clientAddress)
	api.AppendConsulV1CatalogNode()

	cmd := exec.Command(
		data.bin,
		"config",
		"--server",
		u.Host,
	)

	cmdTest := cmdtest.New(cmd)
	if err := cmd.Start(); err != nil {
		log.Fatal(err)
	}
	err := cmd.Wait()
	assert.Nil(t, err)

	// sanity check that version number was changed with ldflag for this test build
	assert.Equal(t, "OK, PMM server is alive.\n", cmdTest.ReadLine())
	assert.Equal(t, "\n", cmdTest.ReadLine())
	assert.Equal(t, fmt.Sprintf("%-15s | %s \n", "PMM Server", u.Host), cmdTest.ReadLine())
	assert.Equal(t, fmt.Sprintf("%-15s | %s\n", "Client Name", clientName), cmdTest.ReadLine())
	assert.Equal(t, fmt.Sprintf("%-15s | %s \n", "Client Address", clientAddress), cmdTest.ReadLine())

	assert.Equal(t, "", cmdTest.ReadLine()) // No more data
}

func testMongoDB(t *testing.T, data pmmAdminData) {
	defer func() {
		err := os.RemoveAll(data.basedir)
		assert.Nil(t, err)
	}()

	os.MkdirAll(data.basedir+"/pmm-client", 0777)
	os.MkdirAll(data.basedir+"/qan-agent/bin", 0777)
	os.MkdirAll(data.basedir+"/qan-agent/config", 0777)
	os.MkdirAll(data.basedir+"/qan-agent/instance", 0777)
	os.Create(data.basedir + "/pmm-client/node_exporter")
	os.Create(data.basedir + "/pmm-client/mysqld_exporter")
	os.Create(data.basedir + "/pmm-client/mongodb_exporter")
	os.Create(data.basedir + "/pmm-client/proxysql_exporter")
	os.Create(data.basedir + "/qan-agent/bin/percona-qan-agent")

	f, _ := os.Create(data.basedir + "/qan-agent/bin/percona-qan-agent-installer")
	f.WriteString("#!/bin/sh\n")
	f.WriteString("echo 'it works'")
	f.Close()
	os.Chmod(data.basedir+"/qan-agent/bin/percona-qan-agent-installer", 0777)

	f, _ = os.Create(data.basedir + "/qan-agent/config/agent.conf")
	f.WriteString(`{"UUID":"42","ApiHostname":"somehostname","ApiPath":"/qan-api","ServerUser":"pmm"}`)
	f.WriteString("\n")
	f.Close()
	os.Chmod(data.basedir+"/qan-agent/bin/percona-qan-agent-installer", 0777)
	{
		// Create fake api server
		api := fakeapi.New()
		u, _ := url.Parse(api.URL())
		clientAddress, _, _ := net.SplitHostPort(u.Host)
		api.AppendRoot()
		api.AppendConsulV1StatusLeader(clientAddress)
		api.AppendConsulV1CatalogNode()
		api.AppendConsulV1CatalogService()
		api.AppendConsulV1CatalogRegister()
		mongodbInstance := &proto.Instance{
			Subsystem: "mongodb",
			UUID:      "13",
		}
		agentInstance := &proto.Instance{
			Subsystem: "agent",
			UUID:      "42",
		}
		api.AppendQanAPIInstancesId(agentInstance.UUID, agentInstance)
		api.AppendQanAPIAgents(agentInstance.UUID)
		api.AppendQanAPIInstances([]*proto.Instance{
			mongodbInstance,
		})

		// Configure pmm
		cmd := exec.Command(
			data.bin,
			"config",
			"--server",
			u.Host,
		)
		output, err := cmd.CombinedOutput()
		assert.Nil(t, err, string(output))
	}

	cmd := exec.Command(
		data.bin,
		"add",
		"mongodb",
	)

	cmdTest := cmdtest.New(cmd)
	if err := cmd.Start(); err != nil {
		log.Fatal(err)
	}
	err := cmd.Wait()
	assert.Nil(t, err)

	assert.Equal(t, fmt.Sprintln("[linux:metrics]   OK, now monitoring this system."), cmdTest.ReadLine())
	assert.Equal(t, fmt.Sprintln("[mongodb:metrics] OK, now monitoring MongoDB metrics using URI localhost:27017"), cmdTest.ReadLine())
	assert.Equal(t, fmt.Sprintln("[mongodb:queries] OK, now monitoring MongoDB queries using URI localhost:27017"), cmdTest.ReadLine())

	assert.Equal(t, "", cmdTest.ReadLine()) // No more data
}

func testMongoDBQueries(t *testing.T, data pmmAdminData) {
	defer func() {
		err := os.RemoveAll(data.basedir)
		assert.Nil(t, err)
	}()

	os.MkdirAll(data.basedir+"/pmm-client", 0777)
	os.MkdirAll(data.basedir+"/qan-agent/bin", 0777)
	os.MkdirAll(data.basedir+"/qan-agent/config", 0777)
	os.MkdirAll(data.basedir+"/qan-agent/instance", 0777)
	os.Create(data.basedir + "/pmm-client/node_exporter")
	os.Create(data.basedir + "/pmm-client/mysqld_exporter")
	os.Create(data.basedir + "/pmm-client/mongodb_exporter")
	os.Create(data.basedir + "/pmm-client/proxysql_exporter")
	os.Create(data.basedir + "/qan-agent/bin/percona-qan-agent")

	f, _ := os.Create(data.basedir + "/qan-agent/bin/percona-qan-agent-installer")
	f.WriteString("#!/bin/sh\n")
	f.WriteString("echo 'it works'")
	f.Close()
	os.Chmod(data.basedir+"/qan-agent/bin/percona-qan-agent-installer", 0777)

	f, _ = os.Create(data.basedir + "/qan-agent/config/agent.conf")
	f.WriteString(`{"UUID":"42","ApiHostname":"somehostname","ApiPath":"/qan-api","ServerUser":"pmm"}`)
	f.WriteString("\n")
	f.Close()
	os.Chmod(data.basedir+"/qan-agent/bin/percona-qan-agent-installer", 0777)
	{
		// Create fake api server
		api := fakeapi.New()
		u, _ := url.Parse(api.URL())
		clientAddress, _, _ := net.SplitHostPort(u.Host)
		api.AppendRoot()
		api.AppendConsulV1StatusLeader(clientAddress)
		api.AppendConsulV1CatalogNode()
		api.AppendConsulV1CatalogService()
		api.AppendConsulV1CatalogRegister()
		mongodbInstance := &proto.Instance{
			Subsystem: "mongodb",
			UUID:      "13",
		}
		agentInstance := &proto.Instance{
			Subsystem: "agent",
			UUID:      "42",
		}
		api.AppendQanAPIInstancesId(agentInstance.UUID, agentInstance)
		api.AppendQanAPIAgents(agentInstance.UUID)
		api.AppendQanAPIInstances([]*proto.Instance{
			mongodbInstance,
		})

		// Configure pmm
		cmd := exec.Command(
			data.bin,
			"config",
			"--server",
			u.Host,
		)
		output, err := cmd.CombinedOutput()
		assert.Nil(t, err, string(output))
	}

	cmd := exec.Command(
		data.bin,
		"add",
		"mongodb:queries",
	)

	cmdTest := cmdtest.New(cmd)
	if err := cmd.Start(); err != nil {
		log.Fatal(err)
	}
	err := cmd.Wait()
	assert.Nil(t, err)

	assert.Equal(t, fmt.Sprintln("OK, now monitoring MongoDB queries using URI localhost:27017"), cmdTest.ReadLine())

	assert.Equal(t, "", cmdTest.ReadLine()) // No more data
}
