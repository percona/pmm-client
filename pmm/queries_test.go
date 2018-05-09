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
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"testing"
	"time"

	"github.com/percona/pmm-client/tests/fakeapi"
	pc "github.com/percona/pmm/proto/config"
	protocfg "github.com/percona/pmm/proto/config"
	"github.com/stretchr/testify/assert"
)

func TestGetAgentID(t *testing.T) {
	// create tmpfile
	f, err := ioutil.TempFile("", "")
	assert.Nil(t, err)

	// remove it after test finishes
	defer os.Remove(f.Name())

	t.Run("correct file", func(t *testing.T) {
		config := &protocfg.Agent{
			UUID: "qwe123",
		}

		bytes, err := json.Marshal(config)
		assert.Nil(t, err)
		err = ioutil.WriteFile(f.Name(), bytes, 0600)
		assert.Nil(t, err)

		uuid, err := getAgentID(f.Name())
		assert.Nil(t, err)
		assert.Equal(t, config.UUID, uuid)
	})

	t.Run("incorrect file", func(t *testing.T) {
		config := &protocfg.Agent{}

		bytes, err := json.Marshal(config)
		assert.Nil(t, err)
		err = ioutil.WriteFile(f.Name(), bytes, 0600)
		assert.Nil(t, err)

		uuid, err := getAgentID(f.Name())
		assert.Error(t, err)
		assert.Equal(t, config.UUID, uuid)
	})
}

func TestAdmin_StartStopQAN(t *testing.T) {
	t.Parallel()

	agentID := "123"

	// create fakeapi
	api := fakeapi.New()
	api.AppendQanAPIAgents(agentID)
	defer api.Close()
	_, host, port := api.Start()

	exampleQueries := true
	// create qan config
	qanConfig := pc.QAN{
		UUID:           "xyz",
		Interval:       60,
		ExampleQueries: &exampleQueries,
	}

	// create pmm-admin instance
	admin := &Admin{}
	insecureFlag := true
	timeout := 1 * time.Second
	debug := false
	admin.qanAPI = NewAPI(insecureFlag, timeout, debug)

	// point pmm-admin to fake http api
	admin.serverURL = fmt.Sprintf("%s:%s", host, port)
	scheme := "http"
	authStr := ""
	admin.serverURL = fmt.Sprintf("%s://%s%s:%s", scheme, authStr, host, port)

	t.Run("startQAN", func(t *testing.T) {
		err := admin.startQAN(agentID, qanConfig)
		assert.Nil(t, err)
	})

	t.Run("stopQAN", func(t *testing.T) {
		err := admin.stopQAN(agentID, "qwe")
		assert.Nil(t, err)
	})
}

func TestGetQueriesOptions(t *testing.T) {
	config, err := getProtoQAN("testdata/qan-2b6c3eb3669943c160502874036968ba.conf")
	assert.NoError(t, err)
	opts := getQueriesOptions(config)
	expected := []string{
		"query_source=perfschema",
		"query_examples=true",
	}
	assert.Equal(t, expected, opts)
}
