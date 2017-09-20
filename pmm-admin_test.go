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
	"encoding/json"
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

	"github.com/hashicorp/consul/api"
	"github.com/percona/pmm-client/pmm"
	"github.com/percona/pmm-client/test/cmdtest"
	"github.com/percona/pmm-client/test/fakeapi"
	"github.com/percona/pmm/proto"
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
		testAddMongoDB,
		testAddMongoDBQueries,
		testAddLinuxMetricsWithAdditionalArgsOk,
		testAddLinuxMetricsWithAdditionalArgsFail,
		testCheckNetwork,
		testConfig,
		testConfigVerbose,
		testConfigVerboseServerNotAvailable,
		testList,
		testStartStopRestart,
		testStartStopRestartAllWithNoServices,
		testStartStopRestartAllWithServices,
		testStartStopRestartNoServiceFound,
		testVersion,
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
	fapi := fakeapi.New()
	defer fapi.Close()
	u, _ := url.Parse(fapi.URL())
	clientAddress, _, _ := net.SplitHostPort(u.Host)
	clientName, _ := os.Hostname()
	fapi.AppendRoot()
	fapi.AppendConsulV1StatusLeader(clientAddress)
	node := &api.CatalogNode{
		Node: &api.Node{},
	}
	fapi.AppendConsulV1CatalogNode(clientName, node)

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
		err := os.RemoveAll(data.rootDir)
		assert.Nil(t, err)
	}()

	os.MkdirAll(data.rootDir+pmm.PMMBaseDir, 0777)

	// Create fake api server
	fapi := fakeapi.New()
	defer fapi.Close()
	u, _ := url.Parse(fapi.URL())
	clientAddress, _, _ := net.SplitHostPort(u.Host)
	clientName, _ := os.Hostname()
	fapi.AppendRoot()
	fapi.AppendConsulV1StatusLeader(clientAddress)
	node := &api.CatalogNode{
		Node: &api.Node{},
	}
	fapi.AppendConsulV1CatalogNode(clientName, node)

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
	assert.Regexp(t, "< X-Server-Time: .+\n", cmdTest.ReadLine())
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
	assert.Equal(t, "< Content-Length: 140\n", cmdTest.ReadLine())
	assert.Equal(t, "< Content-Type: text/plain; charset=utf-8\n", cmdTest.ReadLine())
	assert.Regexp(t, "< Date: .+\n", cmdTest.ReadLine())
	assert.Equal(t, "< \n", cmdTest.ReadLine())
	assert.Equal(t, "< {\"Node\":{\"ID\":\"\",\"Node\":\"\",\"Address\":\"\",\"Datacenter\":\"\",\"TaggedAddresses\":null,\"Meta\":null,\"CreateIndex\":0,\"ModifyIndex\":0},\"Services\":null}\n", cmdTest.ReadLine())
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
	assert.Regexp(t, "< X-Server-Time: .+\n", cmdTest.ReadLine())
	assert.Equal(t, "< \n", cmdTest.ReadLine())
	assert.Equal(t, "< \"127.0.0.1:8300\"\n", cmdTest.ReadLine())

	// stdout
	assert.Equal(t, "OK, PMM server is alive.\n", cmdTest.ReadLine())
	assert.Equal(t, "\n", cmdTest.ReadLine())
	assert.Equal(t, fmt.Sprintf("%-15s | %s \n", "PMM Server", u.Host), cmdTest.ReadLine())
	assert.Equal(t, fmt.Sprintf("%-15s | %s\n", "Client Name", clientName), cmdTest.ReadLine())
	assert.Equal(t, fmt.Sprintf("%-15s | %s \n", "Client Address", clientAddress), cmdTest.ReadLine())

	assert.Equal(t, []string{}, cmdTest.Output()) // No more data
}

func testConfigVerboseServerNotAvailable(t *testing.T, data pmmAdminData) {
	defer func() {
		err := os.RemoveAll(data.rootDir)
		assert.Nil(t, err)
	}()

	os.MkdirAll(data.rootDir+pmm.PMMBaseDir, 0777)

	cmd := exec.Command(
		data.bin,
		"config",
		"--verbose",
		"--server",
		"xyz",
	)

	cmdTest := cmdtest.New(cmd)
	if err := cmd.Start(); err != nil {
		log.Fatal(err)
	}
	err := cmd.Wait()
	assert.Error(t, err)

	// with --verbose flag we should have bunch of http requests to server
	// however api is unavailable, so `--verbose` prints only request...
	assert.Regexp(t, ".+ request:\n", cmdTest.ReadLine())
	assert.Equal(t, "> GET / HTTP/1.1\n", cmdTest.ReadLine())
	assert.Regexp(t, "> Host: .+\n", cmdTest.ReadLine())
	assert.Equal(t, "> User-Agent: Go-http-client/1.1\n", cmdTest.ReadLine())
	assert.Equal(t, "> Accept-Encoding: gzip\n", cmdTest.ReadLine())
	assert.Equal(t, "> \n", cmdTest.ReadLine())
	assert.Equal(t, "> \n", cmdTest.ReadLine())
	// ... and then error message
	assert.Equal(t, "Unable to connect to PMM server by address: xyz\n", cmdTest.ReadLine())
	assert.Regexp(t, "Get http://xyz: dial tcp: lookup xyz.*: no such host\n", cmdTest.ReadLine())
	assert.Equal(t, "\n", cmdTest.ReadLine())
	assert.Equal(t, "* Check if the configured address is correct.\n", cmdTest.ReadLine())
	assert.Equal(t, "* If server is running on non-default port, ensure it was specified along with the address.\n", cmdTest.ReadLine())
	assert.Equal(t, "* If server is enabled for SSL or self-signed SSL, enable the corresponding option.\n", cmdTest.ReadLine())
	assert.Equal(t, "* You may also check the firewall settings.\n", cmdTest.ReadLine())

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

func testList(t *testing.T, data pmmAdminData) {
	defer func() {
		err := os.RemoveAll(data.rootDir)
		assert.Nil(t, err)
	}()

	// Create fake api server
	fapi := fakeapi.New()
	defer fapi.Close()
	u, _ := url.Parse(fapi.URL())
	serverAddress, _, _ := net.SplitHostPort(u.Host)
	clientName := "test-client-name"
	fapi.AppendRoot()
	fapi.AppendConsulV1StatusLeader(serverAddress)
	node := &api.CatalogNode{
		Node: &api.Node{},
	}
	fapi.AppendConsulV1CatalogNode(clientName, node)
	fapi.AppendConsulV1KV()

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
		ServerAddress: fmt.Sprintf("%s:%s", fapi.Host(), fapi.Port()),
		ClientName:    clientName,
		ClientAddress: "empty",
		BindAddress:   "data",
	}
	bytes, _ := yaml.Marshal(pmmConfig)
	ioutil.WriteFile(data.rootDir+pmm.PMMBaseDir+"/pmm.yml", bytes, 0600)

	// Test empty list
	t.Run("list (empty)", func(t *testing.T) {
		cmd := exec.Command(
			data.bin,
			"list",
		)

		cmdTest := cmdtest.New(cmd)
		if err := cmd.Start(); err != nil {
			log.Fatal(err)
		}
		err := cmd.Wait()
		assert.Nil(t, err)

		expect := `pmm-admin gotest

PMM Server      \| .*
Client Name     \| test-client-name
Client Address  \| .*
Service Manager \| .*

No services under monitoring.
`
		got := strings.Join(cmdTest.Output(), "")
		assert.Regexp(t, expect, got)
	})

	node.Services = map[string]*api.AgentService{
		"a": {
			ID:      "id",
			Service: "mysql:queries",
			Port:    0,
			Tags: []string{
				fmt.Sprintf("alias_%s", clientName),
			},
		},
		"b": {
			ID:      "id",
			Service: "mongodb:queries",
			Port:    0,
			Tags: []string{
				fmt.Sprintf("alias_%s", clientName),
			},
		},
	}

	// create fake system service
	{
		dir, extension := pmm.GetServiceDirAndExtension()
		os.MkdirAll(data.rootDir+dir, 0777)
		name := fmt.Sprintf("pmm-mysql-queries-0%s", extension)
		os.Create(data.rootDir + dir + "/" + name)
	}
	{
		dir, extension := pmm.GetServiceDirAndExtension()
		os.MkdirAll(data.rootDir+dir, 0777)
		name := fmt.Sprintf("pmm-mongodb-queries-0%s", extension)
		os.Create(data.rootDir + dir + "/" + name)
	}

	// Test --help output
	t.Run("list --help", func(t *testing.T) {
		cmd := exec.Command(
			data.bin,
			"list",
			"--help",
		)

		cmdTest := cmdtest.New(cmd)
		if err := cmd.Start(); err != nil {
			log.Fatal(err)
		}
		err := cmd.Wait()
		assert.Nil(t, err)

		expect := `This command displays the list of monitoring services and their details.

Usage:
  pmm-admin list \[flags\]

Aliases:
  list, ls

Flags:
      --format string   print result using a Go template
  -h, --help            help for list
      --json            print result as json

Global Flags:
  -c, --config-file string   PMM config file \(default ".*?"\)
      --verbose              verbose output
`
		got := strings.Join(cmdTest.Output(), "")
		assert.Regexp(t, expect, got)
	})

	// Test text output
	t.Run("list", func(t *testing.T) {
		cmd := exec.Command(
			data.bin,
			"list",
		)

		cmdTest := cmdtest.New(cmd)
		if err := cmd.Start(); err != nil {
			log.Fatal(err)
		}
		err := cmd.Wait()
		assert.Nil(t, err)

		expect := `pmm-admin gotest

PMM Server      \| .*
Client Name     \| test-client-name
Client Address  \| .*
Service Manager \| .*

---------------- ----------------- ----------- -------- ------------ --------
SERVICE TYPE     NAME              LOCAL PORT  RUNNING  DATA SOURCE  OPTIONS\s*
---------------- ----------------- ----------- -------- ------------ --------
mongodb:queries  test-client-name  -           YES                 - \s*
mysql:queries    test-client-name  -           YES                 - \s*
`
		got := strings.Join(cmdTest.Output(), "")
		assert.Regexp(t, expect, got)
	})

	// Test json output
	t.Run("list --json", func(t *testing.T) {
		cmd := exec.Command(
			data.bin,
			"list",
			"--json",
		)

		cmdTest := cmdtest.New(cmd)
		if err := cmd.Start(); err != nil {
			log.Fatal(err)
		}
		err := cmd.Wait()
		assert.Nil(t, err)

		expect := pmm.List{
			Version: "gotest",
			ServerInfo: pmm.ServerInfo{
				ClientName:        "test-client-name",
				ClientAddress:     "empty",
				ClientBindAddress: "(data)",
			},
			Services: []pmm.ServiceStatus{
				{
					Type:    "mongodb:queries",
					Name:    "test-client-name",
					Port:    "-",
					DSN:     "-",
					Running: true,
				},
				{
					Type:    "mysql:queries",
					Name:    "test-client-name",
					Port:    "-",
					DSN:     "-",
					Running: true,
				},
			},
		}
		out := strings.Join(cmdTest.Output(), "")
		got := pmm.List{}
		err = json.Unmarshal([]byte(out), &got)

		// we can't really test this data
		got.Platform = ""
		got.ServerAddress = ""

		assert.Nil(t, err)
		assert.Equal(t, expect, got)
	})

	// Test custom text template with table data ()
	t.Run("list --format <table data>", func(t *testing.T) {
		format := `SERVICE TYPE	NAME	LOCAL PORT	RUNNING	DATA SOURCE	OPTIONS
{{range .Services}}{{.Type}}	{{.Name}}	{{.Port}}	{{if .Running}}YES{{else}}NO{{end}}	{{.DSN}}	{{.Options}}
{{end}}`

		cmd := exec.Command(
			data.bin,
			"list",
			"--format", format,
		)

		cmdTest := cmdtest.New(cmd)
		if err := cmd.Start(); err != nil {
			log.Fatal(err)
		}
		err := cmd.Wait()
		assert.Nil(t, err)

		expect := `SERVICE TYPE           NAME                    LOCAL PORT        RUNNING        DATA SOURCE        OPTIONS\s*
mongodb:queries        test-client-name        -                 YES            -\s*
mysql:queries          test-client-name        -                 YES            -\s*
`
		got := strings.Join(cmdTest.Output(), "")
		assert.Regexp(t, expect, got)
	})

	// Test custom format that produces just json list
	t.Run("list --format '{{json .Services'}}", func(t *testing.T) {
		format := `{{json .Services}}`

		cmd := exec.Command(
			data.bin,
			"list",
			"--format", format,
		)

		cmdTest := cmdtest.New(cmd)
		if err := cmd.Start(); err != nil {
			log.Fatal(err)
		}
		err := cmd.Wait()
		assert.Nil(t, err)

		expect := `[{"Type":"mongodb:queries","Name":"test-client-name","Port":"-","Running":true,"DSN":"-","Options":"","SSL":"","Password":""},{"Type":"mysql:queries","Name":"test-client-name","Port":"-","Running":true,"DSN":"-","Options":"","SSL":"","Password":""}]`
		got := strings.Join(cmdTest.Output(), "")
		assert.JSONEq(t, expect, got)
	})
}

func testStartStopRestart(t *testing.T, data pmmAdminData) {
	defer func() {
		err := os.RemoveAll(data.rootDir)
		assert.Nil(t, err)
	}()

	svcName := "mysql:queries"

	// Create fake api server
	fapi := fakeapi.New()
	defer fapi.Close()
	u, _ := url.Parse(fapi.URL())
	serverAddress, _, _ := net.SplitHostPort(u.Host)
	clientName := "test-client-name"
	fapi.AppendRoot()
	fapi.AppendConsulV1StatusLeader(serverAddress)
	node := &api.CatalogNode{
		Node: &api.Node{},
		Services: map[string]*api.AgentService{
			"a": {
				ID:      "id",
				Service: svcName,
				Port:    0,
				Tags: []string{
					fmt.Sprintf("alias_%s", clientName),
				},
			},
		},
	}
	fapi.AppendConsulV1CatalogNode(clientName, node)

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
		ServerAddress: fmt.Sprintf("%s:%s", fapi.Host(), fapi.Port()),
		ClientName:    clientName,
		ClientAddress: "empty",
		BindAddress:   "data",
	}
	bytes, _ := yaml.Marshal(pmmConfig)
	ioutil.WriteFile(data.rootDir+pmm.PMMBaseDir+"/pmm.yml", bytes, 0600)

	// create fake system service
	{
		dir, extension := pmm.GetServiceDirAndExtension()
		os.MkdirAll(data.rootDir+dir, 0777)
		name := fmt.Sprintf("pmm-mysql-queries-0%s", extension)
		os.Create(data.rootDir + dir + "/" + name)
	}

	t.Run("start", func(t *testing.T) {
		cmd := exec.Command(
			data.bin,
			"start",
			svcName,
		)

		cmdTest := cmdtest.New(cmd)
		if err := cmd.Start(); err != nil {
			log.Fatal(err)
		}
		err := cmd.Wait()
		assert.Nil(t, err)

		assert.Equal(t, fmt.Sprintf("OK, service %s already %s for %s.\n", svcName, "started", clientName), cmdTest.ReadLine())

		assert.Equal(t, []string{}, cmdTest.Output()) // No more data
	})

	t.Run("stop", func(t *testing.T) {
		cmd := exec.Command(
			data.bin,
			"stop",
			svcName,
		)

		cmdTest := cmdtest.New(cmd)
		if err := cmd.Start(); err != nil {
			log.Fatal(err)
		}
		err := cmd.Wait()
		assert.Nil(t, err)

		assert.Equal(t, fmt.Sprintf("OK, %s %s service for %s.\n", "stopped", svcName, clientName), cmdTest.ReadLine())

		assert.Equal(t, []string{}, cmdTest.Output()) // No more data
	})

	t.Run("restart", func(t *testing.T) {
		cmd := exec.Command(
			data.bin,
			"restart",
			svcName,
		)

		cmdTest := cmdtest.New(cmd)
		if err := cmd.Start(); err != nil {
			log.Fatal(err)
		}
		err := cmd.Wait()
		assert.Nil(t, err)

		assert.Equal(t, fmt.Sprintf("OK, %s %s service for %s.\n", "restarted", svcName, clientName), cmdTest.ReadLine())

		assert.Equal(t, []string{}, cmdTest.Output()) // No more data
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
			name := fmt.Sprintf("pmm-service-%d%s", i, extension)
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

		assert.Regexp(t, "Unable to connect to PMM server by address: .*\n", cmdTest.ReadLine())
		assert.Regexp(t, "Get http://.*: dial tcp: lookup .*: no such host\n", cmdTest.ReadLine())
		assert.Equal(t, "\n", cmdTest.ReadLine())
		assert.Equal(t, "* Check if the configured address is correct.\n", cmdTest.ReadLine())
		assert.Equal(t, "* If server is running on non-default port, ensure it was specified along with the address.\n", cmdTest.ReadLine())
		assert.Equal(t, "* If server is enabled for SSL or self-signed SSL, enable the corresponding option.\n", cmdTest.ReadLine())
		assert.Equal(t, "* You may also check the firewall settings.\n", cmdTest.ReadLine())

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

		assert.Regexp(t, "Unable to connect to PMM server by address: .*\n", cmdTest.ReadLine())
		assert.Regexp(t, "Get http://.*: dial tcp: lookup .*: no such host\n", cmdTest.ReadLine())
		assert.Equal(t, "\n", cmdTest.ReadLine())
		assert.Equal(t, "* Check if the configured address is correct.\n", cmdTest.ReadLine())
		assert.Equal(t, "* If server is running on non-default port, ensure it was specified along with the address.\n", cmdTest.ReadLine())
		assert.Equal(t, "* If server is enabled for SSL or self-signed SSL, enable the corresponding option.\n", cmdTest.ReadLine())
		assert.Equal(t, "* You may also check the firewall settings.\n", cmdTest.ReadLine())

		assert.Equal(t, []string{}, cmdTest.Output()) // No more data
	})
}

func testStartStopRestartNoServiceFound(t *testing.T, data pmmAdminData) {
	defer func() {
		err := os.RemoveAll(data.rootDir)
		assert.Nil(t, err)
	}()

	// Create fake api server
	fapi := fakeapi.New()
	defer fapi.Close()
	fapi.AppendRoot()
	fapi.AppendConsulV1StatusLeader(fapi.Host())
	clientName, _ := os.Hostname()
	node := &api.CatalogNode{
		Node: &api.Node{},
	}
	fapi.AppendConsulV1CatalogNode(clientName, node)

	// Create fake filesystem
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
		ServerAddress: fmt.Sprintf("%s:%s", fapi.Host(), fapi.Port()),
		ClientName:    clientName,
		ClientAddress: "localhost",
		BindAddress:   "localhost",
	}
	bytes, _ := yaml.Marshal(pmmConfig)
	ioutil.WriteFile(data.rootDir+pmm.PMMBaseDir+"/pmm.yml", bytes, 0600)
	svcName := "mysql:queries"

	t.Run("start", func(t *testing.T) {
		cmd := exec.Command(
			data.bin,
			"start",
			svcName,
		)

		cmdTest := cmdtest.New(cmd)
		if err := cmd.Start(); err != nil {
			log.Fatal(err)
		}
		err := cmd.Wait()
		assert.Error(t, err)

		assert.Equal(t, fmt.Sprintf("Error %s %s service for %s: no service found.\n", "starting", svcName, clientName), cmdTest.ReadLine())

		assert.Equal(t, []string{}, cmdTest.Output()) // No more data
	})

	t.Run("stop", func(t *testing.T) {
		cmd := exec.Command(
			data.bin,
			"stop",
			svcName,
		)

		cmdTest := cmdtest.New(cmd)
		if err := cmd.Start(); err != nil {
			log.Fatal(err)
		}
		err := cmd.Wait()
		assert.Error(t, err)

		assert.Equal(t, fmt.Sprintf("Error %s %s service for %s: no service found.\n", "stopping", svcName, clientName), cmdTest.ReadLine())

		assert.Equal(t, []string{}, cmdTest.Output()) // No more data
	})

	t.Run("restart", func(t *testing.T) {
		cmd := exec.Command(
			data.bin,
			"restart",
			svcName,
		)

		cmdTest := cmdtest.New(cmd)
		if err := cmd.Start(); err != nil {
			log.Fatal(err)
		}
		err := cmd.Wait()
		assert.Error(t, err)

		assert.Equal(t, fmt.Sprintf("Error %s %s service for %s: no service found.\n", "restarting", svcName, clientName), cmdTest.ReadLine())

		assert.Equal(t, []string{}, cmdTest.Output()) // No more data
	})
}

func testCheckNetwork(t *testing.T, data pmmAdminData) {
	defer func() {
		err := os.RemoveAll(data.rootDir)
		assert.Nil(t, err)
	}()

	// Create fake api server
	fapi := fakeapi.New()
	defer fapi.Close()
	fapi.AppendRoot()
	fapi.AppendPrometheusAPIV1Query()
	fapi.AppendQanAPIPing()
	fapi.AppendConsulV1StatusLeader(fapi.Host())
	clientName, _ := os.Hostname()
	node := &api.CatalogNode{
		Node: &api.Node{},
	}
	fapi.AppendConsulV1CatalogNode(clientName, node)

	// Create fake filesystem
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
		ServerAddress: fmt.Sprintf("%s:%s", fapi.Host(), fapi.Port()),
		ClientName:    clientName,
		ClientAddress: "localhost",
		BindAddress:   "localhost",
	}
	bytes, _ := yaml.Marshal(pmmConfig)
	ioutil.WriteFile(data.rootDir+pmm.PMMBaseDir+"/pmm.yml", bytes, 0600)

	// Test the command
	{
		cmd := exec.Command(
			data.bin,
			"check-network",
		)

		cmdTest := cmdtest.New(cmd)
		if err := cmd.Start(); err != nil {
			log.Fatal(err)
		}
		err := cmd.Wait()
		assert.Nil(t, err)

		assert.Equal(t, "PMM Network Status\n", cmdTest.ReadLine())
		assert.Equal(t, "\n", cmdTest.ReadLine())
		assert.Regexp(t, "Server Address | .*\n", cmdTest.ReadLine())
		assert.Regexp(t, "Client Address | .*\n", cmdTest.ReadLine())
		assert.Equal(t, "\n", cmdTest.ReadLine())
		assert.Equal(t, "* System Time\n", cmdTest.ReadLine())
		assert.Regexp(t, "NTP Server (0.pool.ntp.org)         | .*\n", cmdTest.ReadLine())
		assert.Regexp(t, "PMM Server                          | .*\n", cmdTest.ReadLine())
		assert.Regexp(t, "PMM Client                          | .*\n", cmdTest.ReadLine())
		assert.Equal(t, "PMM Server Time Drift               | OK\n", cmdTest.ReadLine())
		assert.Equal(t, "PMM Client Time Drift               | OK\n", cmdTest.ReadLine())
		assert.Equal(t, "PMM Client to PMM Server Time Drift | OK\n", cmdTest.ReadLine())
		assert.Equal(t, "\n", cmdTest.ReadLine())
		assert.Equal(t, "* Connection: Client --> Server\n", cmdTest.ReadLine())
		assert.Equal(t, "-------------------- -------      \n", cmdTest.ReadLine())
		assert.Equal(t, "SERVER SERVICE       STATUS       \n", cmdTest.ReadLine())
		assert.Equal(t, "-------------------- -------      \n", cmdTest.ReadLine())
		assert.Equal(t, "Consul API           OK           \n", cmdTest.ReadLine())
		assert.Equal(t, "Prometheus API       OK           \n", cmdTest.ReadLine())
		assert.Equal(t, "Query Analytics API  OK           \n", cmdTest.ReadLine())
		assert.Equal(t, "\n", cmdTest.ReadLine())
		assert.Regexp(t, "Connection duration | .*         \n", cmdTest.ReadLine())
		assert.Regexp(t, "Request duration    | .*         \n", cmdTest.ReadLine())
		assert.Regexp(t, "Full round trip     | .*         \n", cmdTest.ReadLine())
		assert.Equal(t, "\n", cmdTest.ReadLine())
		assert.Equal(t, "\n", cmdTest.ReadLine())
		assert.Equal(t, "* Connection: Client <-- Server\n", cmdTest.ReadLine())
		assert.Equal(t, "No metric endpoints registered.\n", cmdTest.ReadLine())
		assert.Equal(t, "\n", cmdTest.ReadLine())

		assert.Equal(t, []string{}, cmdTest.Output()) // No more data
	}
}

func testAddLinuxMetricsWithAdditionalArgsOk(t *testing.T, data pmmAdminData) {
	defer func() {
		err := os.RemoveAll(data.rootDir)
		assert.Nil(t, err)
	}()

	os.MkdirAll(data.rootDir+pmm.PMMBaseDir, 0777)
	os.MkdirAll(data.rootDir+pmm.AgentBaseDir+"/bin", 0777)
	os.MkdirAll(data.rootDir+pmm.AgentBaseDir+"/config", 0777)
	os.MkdirAll(data.rootDir+pmm.AgentBaseDir+"/instance", 0777)
	os.Create(data.rootDir + pmm.PMMBaseDir + "/node_exporter")
	os.Create(data.rootDir + pmm.PMMBaseDir + "/mysqld_exporter")
	os.Create(data.rootDir + pmm.PMMBaseDir + "/mongodb_exporter")
	os.Create(data.rootDir + pmm.PMMBaseDir + "/proxysql_exporter")
	os.Create(data.rootDir + pmm.AgentBaseDir + "/bin/percona-qan-agent")

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
	{
		// Create fake api server
		fapi := fakeapi.New()
		defer fapi.Close()
		fapi.AppendRoot()
		fapi.AppendConsulV1StatusLeader(fapi.Host())
		clientName, _ := os.Hostname()
		node := &api.CatalogNode{
			Node: &api.Node{},
		}
		fapi.AppendConsulV1CatalogNode(clientName, node)
		fapi.AppendConsulV1CatalogService()
		fapi.AppendConsulV1CatalogRegister()

		// Configure pmm
		cmd := exec.Command(
			data.bin,
			"config",
			"--server",
			fmt.Sprintf("%s:%s", fapi.Host(), fapi.Port()),
		)
		output, err := cmd.CombinedOutput()
		assert.Nil(t, err, string(output))
	}

	cmd := exec.Command(
		data.bin,
		"add",
		"linux:metrics",
		"host1",
		"--",
		"--some-additional-params",
		"--for-exporter",
	)

	cmdTest := cmdtest.New(cmd)
	if err := cmd.Start(); err != nil {
		log.Fatal(err)
	}
	err := cmd.Wait()
	assert.Nil(t, err)

	assert.Equal(t, fmt.Sprintln("OK, now monitoring this system."), cmdTest.ReadLine())

	assert.Equal(t, "", cmdTest.ReadLine()) // No more data
}

func testAddLinuxMetricsWithAdditionalArgsFail(t *testing.T, data pmmAdminData) {
	defer func() {
		err := os.RemoveAll(data.rootDir)
		assert.Nil(t, err)
	}()

	os.MkdirAll(data.rootDir+pmm.PMMBaseDir, 0777)
	os.MkdirAll(data.rootDir+pmm.AgentBaseDir+"/bin", 0777)
	os.MkdirAll(data.rootDir+pmm.AgentBaseDir+"/config", 0777)
	os.MkdirAll(data.rootDir+pmm.AgentBaseDir+"/instance", 0777)
	os.Create(data.rootDir + pmm.PMMBaseDir + "/node_exporter")
	os.Create(data.rootDir + pmm.PMMBaseDir + "/mysqld_exporter")
	os.Create(data.rootDir + pmm.PMMBaseDir + "/mongodb_exporter")
	os.Create(data.rootDir + pmm.PMMBaseDir + "/proxysql_exporter")
	os.Create(data.rootDir + pmm.AgentBaseDir + "/bin/percona-qan-agent")

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
	{
		// Create fake api server
		fapi := fakeapi.New()
		defer fapi.Close()
		fapi.AppendRoot()
		fapi.AppendConsulV1StatusLeader(fapi.Host())
		clientName, _ := os.Hostname()
		node := &api.CatalogNode{
			Node: &api.Node{},
		}
		fapi.AppendConsulV1CatalogNode(clientName, node)
		fapi.AppendConsulV1CatalogService()
		fapi.AppendConsulV1CatalogRegister()

		// Configure pmm
		cmd := exec.Command(
			data.bin,
			"config",
			"--server",
			fmt.Sprintf("%s:%s", fapi.Host(), fapi.Port()),
		)
		output, err := cmd.CombinedOutput()
		assert.Nil(t, err, string(output))
	}

	cmd := exec.Command(
		data.bin,
		"add",
		"linux:metrics",
		"host1",
		"too-many-params",
		"--",
		"--some-additional-params",
		"--for-exporter",
	)

	cmdTest := cmdtest.New(cmd)
	if err := cmd.Start(); err != nil {
		log.Fatal(err)
	}
	err := cmd.Wait()
	assert.Error(t, err)

	assert.Equal(t, fmt.Sprintln("Too many parameters. Only service name is allowed but got: host1, too-many-params."), cmdTest.ReadLine())

	assert.Equal(t, "", cmdTest.ReadLine()) // No more data
}

func testAddMongoDB(t *testing.T, data pmmAdminData) {
	defer func() {
		err := os.RemoveAll(data.rootDir)
		assert.Nil(t, err)
	}()

	os.MkdirAll(data.rootDir+pmm.PMMBaseDir, 0777)
	os.MkdirAll(data.rootDir+pmm.AgentBaseDir+"/bin", 0777)
	os.MkdirAll(data.rootDir+pmm.AgentBaseDir+"/config", 0777)
	os.MkdirAll(data.rootDir+pmm.AgentBaseDir+"/instance", 0777)
	os.Create(data.rootDir + pmm.PMMBaseDir + "/node_exporter")
	os.Create(data.rootDir + pmm.PMMBaseDir + "/mysqld_exporter")
	os.Create(data.rootDir + pmm.PMMBaseDir + "/mongodb_exporter")
	os.Create(data.rootDir + pmm.PMMBaseDir + "/proxysql_exporter")
	os.Create(data.rootDir + pmm.AgentBaseDir + "/bin/percona-qan-agent")

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
	{
		// Create fake api server
		fapi := fakeapi.New()
		defer fapi.Close()
		fapi.AppendRoot()
		fapi.AppendConsulV1StatusLeader(fapi.Host())
		clientName, _ := os.Hostname()
		node := &api.CatalogNode{
			Node: &api.Node{},
		}
		fapi.AppendConsulV1CatalogNode(clientName, node)
		fapi.AppendConsulV1CatalogService()
		fapi.AppendConsulV1CatalogRegister()
		mongodbInstance := &proto.Instance{
			Subsystem: "mongodb",
			UUID:      "13",
		}
		agentInstance := &proto.Instance{
			Subsystem: "agent",
			UUID:      "42",
		}
		fapi.AppendQanAPIInstancesId(agentInstance.UUID, agentInstance)
		fapi.AppendQanAPIAgents(agentInstance.UUID)
		fapi.AppendQanAPIInstances([]*proto.Instance{
			mongodbInstance,
		})

		// Configure pmm
		cmd := exec.Command(
			data.bin,
			"config",
			"--server",
			fmt.Sprintf("%s:%s", fapi.Host(), fapi.Port()),
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
	assert.Equal(t, fmt.Sprintln("[mongodb:queries] It is required for correct operation that profiling of monitored MongoDB databases be enabled."), cmdTest.ReadLine())
	assert.Equal(t, fmt.Sprintln("[mongodb:queries] Note that profiling is not enabled by default because it may reduce the performance of your MongoDB server."), cmdTest.ReadLine())
	assert.Equal(t, fmt.Sprintln("[mongodb:queries] For more information read PMM documentation (https://www.percona.com/doc/percona-monitoring-and-management/conf-mongodb.html)."), cmdTest.ReadLine())

	assert.Equal(t, "", cmdTest.ReadLine()) // No more data
}

func testAddMongoDBQueries(t *testing.T, data pmmAdminData) {
	defer func() {
		err := os.RemoveAll(data.rootDir)
		assert.Nil(t, err)
	}()

	os.MkdirAll(data.rootDir+pmm.PMMBaseDir, 0777)
	os.MkdirAll(data.rootDir+pmm.AgentBaseDir+"/bin", 0777)
	os.MkdirAll(data.rootDir+pmm.AgentBaseDir+"/config", 0777)
	os.MkdirAll(data.rootDir+pmm.AgentBaseDir+"/instance", 0777)
	os.Create(data.rootDir + pmm.PMMBaseDir + "/node_exporter")
	os.Create(data.rootDir + pmm.PMMBaseDir + "/mysqld_exporter")
	os.Create(data.rootDir + pmm.PMMBaseDir + "/mongodb_exporter")
	os.Create(data.rootDir + pmm.PMMBaseDir + "/proxysql_exporter")
	os.Create(data.rootDir + pmm.AgentBaseDir + "/bin/percona-qan-agent")

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
	{
		// Create fake api server
		fapi := fakeapi.New()
		defer fapi.Close()
		fapi.AppendRoot()
		fapi.AppendConsulV1StatusLeader(fapi.Host())
		clientName, _ := os.Hostname()
		node := &api.CatalogNode{
			Node: &api.Node{},
		}
		fapi.AppendConsulV1CatalogNode(clientName, node)
		fapi.AppendConsulV1CatalogService()
		fapi.AppendConsulV1CatalogRegister()
		mongodbInstance := &proto.Instance{
			Subsystem: "mongodb",
			UUID:      "13",
		}
		agentInstance := &proto.Instance{
			Subsystem: "agent",
			UUID:      "42",
		}
		fapi.AppendQanAPIInstancesId(agentInstance.UUID, agentInstance)
		fapi.AppendQanAPIAgents(agentInstance.UUID)
		fapi.AppendQanAPIInstances([]*proto.Instance{
			mongodbInstance,
		})

		// Configure pmm
		cmd := exec.Command(
			data.bin,
			"config",
			"--server",
			fmt.Sprintf("%s:%s", fapi.Host(), fapi.Port()),
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
	assert.Equal(t, fmt.Sprintln("It is required for correct operation that profiling of monitored MongoDB databases be enabled."), cmdTest.ReadLine())
	assert.Equal(t, fmt.Sprintln("Note that profiling is not enabled by default because it may reduce the performance of your MongoDB server."), cmdTest.ReadLine())
	assert.Equal(t, fmt.Sprintln("For more information read PMM documentation (https://www.percona.com/doc/percona-monitoring-and-management/conf-mongodb.html)."), cmdTest.ReadLine())

	assert.Equal(t, "", cmdTest.ReadLine()) // No more data
}
