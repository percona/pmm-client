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
	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v2"
)

type pmmAdminData struct {
	bin     string
	rootDir string
}

func TestPmmAdmin(t *testing.T) {
	var err error

	// We can't/shouldn't use /usr/local/percona/ (the default basedir), so use
	// a tmpdir instead with roughly the same structure.
	rootDir, err := ioutil.TempDir("/tmp", "pmm-client-test-rootdir-")
	assert.Nil(t, err)
	defer func() {
		err := os.RemoveAll(rootDir)
		assert.Nil(t, err)
	}()

	binDir, err := ioutil.TempDir("/tmp", "pmm-client-test-bindir-")
	assert.Nil(t, err)
	defer func() {
		err := os.RemoveAll(binDir)
		assert.Nil(t, err)
	}()

	bin := binDir + "/pmm-admin"
	xVariables := map[string]string{
		"github.com/percona/pmm-client/pmm.Version": "gotest",
		"github.com/percona/pmm-client/pmm.RootDir": rootDir,
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
		rootDir: rootDir,
	}
	tests := []func(*testing.T, pmmAdminData){
		testVersion,
		testConfig,
    testConfigVerbose,
		testStartStopRestartAllWithNoServices,
		testStartStopRestartAllWithServices,
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
		err := os.RemoveAll(data.rootDir)
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
		err := os.RemoveAll(data.rootDir)
		assert.Nil(t, err)
	}()

	os.MkdirAll(data.rootDir+pmm.PMMBaseDir, 0777)

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

	assert.Equal(t, "OK, PMM server is alive.\n", cmdTest.ReadLine())
	assert.Equal(t, "\n", cmdTest.ReadLine())
	assert.Equal(t, fmt.Sprintf("%-15s | %s \n", "PMM Server", u.Host), cmdTest.ReadLine())
	assert.Equal(t, fmt.Sprintf("%-15s | %s\n", "Client Name", clientName), cmdTest.ReadLine())
	assert.Equal(t, fmt.Sprintf("%-15s | %s \n", "Client Address", clientAddress), cmdTest.ReadLine())

	assert.Equal(t, []string{}, cmdTest.Output()) // No more data
}

func testConfigVerbose(t *testing.T, data pmmAdminData) {
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
		"--verbose",
		"--server",
		u.Host,
	)

	cmdTest := cmdtest.New(cmd)
	if err := cmd.Start(); err != nil {
		log.Fatal(err)
	}
	err := cmd.Wait()
	assert.Nil(t, err)

	// with --verbose flag we should have bunch of http requests to server
	// api
	assert.Regexp(t, ".+ request:\n", cmdTest.ReadLine())
	assert.Equal(t, "> GET / HTTP/1.1\n", cmdTest.ReadLine())
	assert.Regexp(t, "> Host: .+\n", cmdTest.ReadLine())
	assert.Equal(t, "> User-Agent: Go-http-client/1.1\n", cmdTest.ReadLine())
	assert.Equal(t, "> Accept-Encoding: gzip\n", cmdTest.ReadLine())
	assert.Equal(t, "> \n", cmdTest.ReadLine())
	assert.Equal(t, "> \n", cmdTest.ReadLine())
	assert.Regexp(t, ".+ response:\n", cmdTest.ReadLine())
	assert.Equal(t, "< HTTP/1.1 200 OK\n", cmdTest.ReadLine())
	assert.Equal(t, "< Content-Type: text/plain; charset=utf-8\n", cmdTest.ReadLine())
	assert.Regexp(t, "< Date: .+\n", cmdTest.ReadLine())
	assert.Equal(t, "< Content-Length: 0\n", cmdTest.ReadLine())
	assert.Equal(t, "< \n", cmdTest.ReadLine())
	assert.Equal(t, "< \n", cmdTest.ReadLine())
	assert.Regexp(t, ".+ request:\n", cmdTest.ReadLine())
	assert.Equal(t, "> GET /v1/status/leader HTTP/1.1\n", cmdTest.ReadLine())
	assert.Regexp(t, "> Host: .+\n", cmdTest.ReadLine())
	assert.Equal(t, "> User-Agent: Go-http-client/1.1\n", cmdTest.ReadLine())
	assert.Equal(t, "> Accept-Encoding: gzip\n", cmdTest.ReadLine())
	assert.Equal(t, "> \n", cmdTest.ReadLine())
	assert.Equal(t, "> \n", cmdTest.ReadLine())
	assert.Regexp(t, ".+ response:\n", cmdTest.ReadLine())
	assert.Equal(t, "< HTTP/1.1 200 OK\n", cmdTest.ReadLine())
	assert.Equal(t, "< Content-Length: 16\n", cmdTest.ReadLine())
	assert.Equal(t, "< Content-Type: text/plain; charset=utf-8\n", cmdTest.ReadLine())
	assert.Regexp(t, "< Date: .+\n", cmdTest.ReadLine())
	assert.Equal(t, "< X-Remote-Ip: 127.0.0.1\n", cmdTest.ReadLine())
	assert.Equal(t, "< \n", cmdTest.ReadLine())
	assert.Equal(t, "< \"127.0.0.1:8300\"\n", cmdTest.ReadLine())

	// consul
	assert.Regexp(t, ".+ request:\n", cmdTest.ReadLine())
	assert.Regexp(t, "> GET /v1/catalog/node/.+ HTTP/1.1\n", cmdTest.ReadLine())
	assert.Regexp(t, "> Host: .+\n", cmdTest.ReadLine())
	assert.Equal(t, "> User-Agent: Go-http-client/1.1\n", cmdTest.ReadLine())
	assert.Equal(t, "> Accept-Encoding: gzip\n", cmdTest.ReadLine())
	assert.Equal(t, "> \n", cmdTest.ReadLine())
	assert.Equal(t, "> \n", cmdTest.ReadLine())
	assert.Regexp(t, ".+ response:\n", cmdTest.ReadLine())
	assert.Equal(t, "< HTTP/1.1 200 OK\n", cmdTest.ReadLine())
	assert.Equal(t, "< Content-Length: 92\n", cmdTest.ReadLine())
	assert.Equal(t, "< Content-Type: text/plain; charset=utf-8\n", cmdTest.ReadLine())
	assert.Regexp(t, "< Date: .+\n", cmdTest.ReadLine())
	assert.Equal(t, "< \n", cmdTest.ReadLine())
	assert.Equal(t, "< {\"Node\":{\"ID\":\"\",\"Node\":\"\",\"Address\":\"\",\"TaggedAddresses\":null,\"Meta\":null},\"Services\":null}\n", cmdTest.ReadLine())
	assert.Regexp(t, ".+ request:\n", cmdTest.ReadLine())
	assert.Equal(t, "> GET /v1/status/leader HTTP/1.1\n", cmdTest.ReadLine())
	assert.Regexp(t, "> Host: .+\n", cmdTest.ReadLine())
	assert.Equal(t, "> User-Agent: Go-http-client/1.1\n", cmdTest.ReadLine())
	assert.Equal(t, "> Accept-Encoding: gzip\n", cmdTest.ReadLine())
	assert.Equal(t, "> \n", cmdTest.ReadLine())
	assert.Equal(t, "> \n", cmdTest.ReadLine())
	assert.Regexp(t, ".+ response:\n", cmdTest.ReadLine())
	assert.Equal(t, "< HTTP/1.1 200 OK\n", cmdTest.ReadLine())
	assert.Equal(t, "< Content-Length: 16\n", cmdTest.ReadLine())
	assert.Equal(t, "< Content-Type: text/plain; charset=utf-8\n", cmdTest.ReadLine())
	assert.Regexp(t, "< Date: .+\n", cmdTest.ReadLine())
	assert.Equal(t, "< X-Remote-Ip: 127.0.0.1\n", cmdTest.ReadLine())
	assert.Equal(t, "< \n", cmdTest.ReadLine())
	assert.Equal(t, "< \"127.0.0.1:8300\"\n", cmdTest.ReadLine())
	assert.Regexp(t, ".+ request:\n", cmdTest.ReadLine())
	assert.Regexp(t, "> GET /v1/catalog/node/.+ HTTP/1.1\n", cmdTest.ReadLine())
	assert.Regexp(t, "> Host: .+\n", cmdTest.ReadLine())
	assert.Equal(t, "> User-Agent: Go-http-client/1.1\n", cmdTest.ReadLine())
	assert.Equal(t, "> Accept-Encoding: gzip\n", cmdTest.ReadLine())
	assert.Equal(t, "> \n", cmdTest.ReadLine())
	assert.Equal(t, "> \n", cmdTest.ReadLine())
	assert.Regexp(t, ".+ response:\n", cmdTest.ReadLine())
	assert.Equal(t, "< HTTP/1.1 200 OK\n", cmdTest.ReadLine())
	assert.Equal(t, "< Content-Length: 92\n", cmdTest.ReadLine())
	assert.Equal(t, "< Content-Type: text/plain; charset=utf-8\n", cmdTest.ReadLine())
	assert.Regexp(t, "< Date: .+\n", cmdTest.ReadLine())
	assert.Equal(t, "< \n", cmdTest.ReadLine())
	assert.Equal(t, "< {\"Node\":{\"ID\":\"\",\"Node\":\"\",\"Address\":\"\",\"TaggedAddresses\":null,\"Meta\":null},\"Services\":null}\n", cmdTest.ReadLine())

	// stdout
	assert.Equal(t, "OK, PMM server is alive.\n", cmdTest.ReadLine())
	assert.Equal(t, "\n", cmdTest.ReadLine())
	assert.Equal(t, fmt.Sprintf("%-15s | %s \n", "PMM Server", u.Host), cmdTest.ReadLine())
	assert.Equal(t, fmt.Sprintf("%-15s | %s\n", "Client Name", clientName), cmdTest.ReadLine())
	assert.Equal(t, fmt.Sprintf("%-15s | %s \n", "Client Address", clientAddress), cmdTest.ReadLine())

	assert.Equal(t, []string{}, cmdTest.Output()) // No more data
}

func testStartStopRestartAllWithNoServices(t *testing.T, data pmmAdminData) {
	defer func() {
		err := os.RemoveAll(data.rootDir)
		assert.Nil(t, err)
	}()

	os.MkdirAll(data.rootDir+pmm.PMMBaseDir, 0777)
	os.Create(data.rootDir + pmm.PMMBaseDir + "/node_exporter")
	os.Create(data.rootDir + pmm.PMMBaseDir + "/mysqld_exporter")
	os.Create(data.rootDir + pmm.PMMBaseDir + "/mongodb_exporter")
	os.Create(data.rootDir + pmm.PMMBaseDir + "/proxysql_exporter")

	os.MkdirAll(data.rootDir+pmm.AgentBaseDir+"/bin", 0777)
	os.Create(data.rootDir + pmm.AgentBaseDir + "/bin/percona-qan-agent")
	os.MkdirAll(data.rootDir+pmm.AgentBaseDir+"/config", 0777)
	os.MkdirAll(data.rootDir+pmm.AgentBaseDir+"/instance", 0777)

	f, _ := os.Create(data.rootDir + pmm.AgentBaseDir + "/bin/percona-qan-agent-installer")
	f.WriteString("#!/bin/sh\n")
	f.WriteString("echo 'it works'")
	f.Close()
	os.Chmod(data.rootDir+pmm.AgentBaseDir+"/bin/percona-qan-agent-installer", 0777)

	f, _ = os.Create(data.rootDir + pmm.AgentBaseDir + "/config/agent.conf")
	f.WriteString(`{"UUID":"42","ApiHostname":"somehostname","ApiPath":"/qan-api","ServerUser":"pmm"}`)
	f.WriteString("\n")
	f.Close()
	os.Chmod(data.rootDir+pmm.AgentBaseDir+"/bin/percona-qan-agent-installer", 0777)

	pmmConfig := pmm.Config{
		ServerAddress: "just",
		ClientName:    "non",
		ClientAddress: "empty",
		BindAddress:   "data",
	}
	bytes, _ := yaml.Marshal(pmmConfig)
	ioutil.WriteFile(data.rootDir+pmm.PMMBaseDir+"/pmm.yml", bytes, 0600)

	services := []string{
		"start",
		"stop",
		"restart",
	}
	t.Run("service", func(t *testing.T) {
		for _, service := range services {
			service := service // capture range variable
			t.Run(service, func(t *testing.T) {
				t.Parallel()
				cmd := exec.Command(
					data.bin,
					service,
					"--all",
				)

				cmdTest := cmdtest.New(cmd)
				if err := cmd.Start(); err != nil {
					log.Fatal(err)
				}
				err := cmd.Wait()
				assert.Nil(t, err)

				assert.Equal(t, "OK, no services found.\n", cmdTest.ReadLine())

				assert.Equal(t, []string{}, cmdTest.Output()) // No more data
			})
		}
	})
}

func testStartStopRestartAllWithServices(t *testing.T, data pmmAdminData) {
	defer func() {
		err := os.RemoveAll(data.rootDir)
		assert.Nil(t, err)
	}()

	os.MkdirAll(data.rootDir+pmm.PMMBaseDir, 0777)
	os.Create(data.rootDir + pmm.PMMBaseDir + "/node_exporter")
	os.Create(data.rootDir + pmm.PMMBaseDir + "/mysqld_exporter")
	os.Create(data.rootDir + pmm.PMMBaseDir + "/mongodb_exporter")
	os.Create(data.rootDir + pmm.PMMBaseDir + "/proxysql_exporter")

	os.MkdirAll(data.rootDir+pmm.AgentBaseDir+"/bin", 0777)
	os.Create(data.rootDir + pmm.AgentBaseDir + "/bin/percona-qan-agent")
	os.MkdirAll(data.rootDir+pmm.AgentBaseDir+"/config", 0777)
	os.MkdirAll(data.rootDir+pmm.AgentBaseDir+"/instance", 0777)

	f, _ := os.Create(data.rootDir + pmm.AgentBaseDir + "/bin/percona-qan-agent-installer")
	f.WriteString("#!/bin/sh\n")
	f.WriteString("echo 'it works'")
	f.Close()
	os.Chmod(data.rootDir+pmm.AgentBaseDir+"/bin/percona-qan-agent-installer", 0777)

	f, _ = os.Create(data.rootDir + pmm.AgentBaseDir + "/config/agent.conf")
	f.WriteString(`{"UUID":"42","ApiHostname":"somehostname","ApiPath":"/qan-api","ServerUser":"pmm"}`)
	f.WriteString("\n")
	f.Close()
	os.Chmod(data.rootDir+pmm.AgentBaseDir+"/bin/percona-qan-agent-installer", 0777)

	pmmConfig := pmm.Config{
		ServerAddress: "just",
		ClientName:    "non",
		ClientAddress: "empty",
		BindAddress:   "data",
	}
	bytes, _ := yaml.Marshal(pmmConfig)
	ioutil.WriteFile(data.rootDir+pmm.PMMBaseDir+"/pmm.yml", bytes, 0600)

	// create fake system services
	numOfServices := 3
	{
		dir, extension := pmm.GetServiceDirAndExtension()
		os.MkdirAll(data.rootDir+dir, 0777)
		for i := 0; i < numOfServices; i++ {
			name := fmt.Sprintf("pmm-service-%d.%s", i, extension)
			if extension == "" {
				name = fmt.Sprintf("pmm-service-%d", i)
			}

			os.Create(data.rootDir + dir + "/" + name)
		}
	}

	t.Run("start", func(t *testing.T) {
		cmd := exec.Command(
			data.bin,
			"start",
			"--all",
		)

		cmdTest := cmdtest.New(cmd)
		if err := cmd.Start(); err != nil {
			log.Fatal(err)
		}
		err := cmd.Wait()
		assert.Nil(t, err)

		assert.Equal(t, fmt.Sprintf("OK, all services already %s. Run 'pmm-admin list' to see monitoring services.\n", "started"), cmdTest.ReadLine())

		assert.Equal(t, []string{}, cmdTest.Output()) // No more data
	})

	t.Run("stop", func(t *testing.T) {
		cmd := exec.Command(
			data.bin,
			"stop",
			"--all",
		)

		cmdTest := cmdtest.New(cmd)
		if err := cmd.Start(); err != nil {
			log.Fatal(err)
		}
		err := cmd.Wait()
		assert.Nil(t, err)

		assert.Equal(t, fmt.Sprintf("OK, %s %d services.\n", "stopped", numOfServices), cmdTest.ReadLine())

		assert.Equal(t, []string{}, cmdTest.Output()) // No more data
	})

	t.Run("restart", func(t *testing.T) {
		cmd := exec.Command(
			data.bin,
			"restart",
			"--all",
		)

		cmdTest := cmdtest.New(cmd)
		if err := cmd.Start(); err != nil {
			log.Fatal(err)
		}
		err := cmd.Wait()
		assert.Nil(t, err)

		assert.Equal(t, fmt.Sprintf("OK, %s %d services.\n", "restarted", numOfServices), cmdTest.ReadLine())

		assert.Equal(t, []string{}, cmdTest.Output()) // No more data
	})
}
