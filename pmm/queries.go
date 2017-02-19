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
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"time"

	"github.com/percona/pmm/proto"
	protocfg "github.com/percona/pmm/proto/config"
)

// deleteInstance delete instance on QAN API.
func (a *Admin) deleteInstance(uuid string) error {
	// Remove MySQL instance from QAN.
	url := a.qanAPI.URL(a.serverURL, qanAPIBasePath, "instances", uuid)
	resp, content, err := a.qanAPI.Delete(url)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusNoContent {
		return a.qanAPI.Error("DELETE", url, resp.StatusCode, http.StatusNoContent, content)
	}

	return nil
}

// getAgentInstance get agent instance from QAN API and return its parent_uuid.
func (a *Admin) getAgentInstance(agentID string) (string, error) {
	var in proto.Instance
	url := a.qanAPI.URL(a.serverURL, qanAPIBasePath, "instances", agentID)
	resp, bytes, err := a.qanAPI.Get(url)
	if err != nil {
		return "", err
	}
	if resp.StatusCode == http.StatusNotFound {
		// No agent instance on QAN API - orphaned agent installation.
		return "", errNoInstance
	}
	if resp.StatusCode != http.StatusOK {
		return "", a.qanAPI.Error("GET", url, resp.StatusCode, http.StatusOK, bytes)
	}

	if err := json.Unmarshal(bytes, &in); err != nil {
		return "", err
	}

	return in.ParentUUID, nil
}

// getAgentID read agent UUID from agent QAN config file.
func getAgentID(configFile string) (string, error) {
	jsonData, err := ioutil.ReadFile(configFile)
	if err != nil {
		return "", err
	}

	config := &protocfg.Agent{}
	if err := json.Unmarshal(jsonData, &config); err != nil {
		return "", err
	}

	return config.UUID, nil
}

// manageQAN enable/disable QAN on agent through QAN API.
func (a *Admin) manageQAN(agentID, cmdName, UUID string, config map[string]interface{}) error {
	var data []byte
	if cmdName == "StartTool" {
		data, _ = json.Marshal(config)
	} else if cmdName == "StopTool" {
		data = []byte(UUID)
	}
	cmd := proto.Cmd{
		User:    fmt.Sprintf("pmm-admin@%s", a.qanAPI.Hostname()),
		Service: "qan",
		Cmd:     cmdName,
		Data:    data,
	}
	cmdBytes, _ := json.Marshal(cmd)

	// Send the command to the API which relays it to the agent, then relays the agent's reply back to here.
	url := a.qanAPI.URL(a.serverURL, qanAPIBasePath, "agents", agentID, "cmd")

	// It takes a few seconds for agent to connect to QAN API once it is started via service manager.
	// QAN API fails to start/stop unconnected agent for QAN, so we retry the request when getting 404 response.
	for i := 0; i < 10; i++ {
		resp, content, err := a.qanAPI.Put(url, cmdBytes)
		if err != nil {
			return err
		}
		if resp.StatusCode == http.StatusNotFound {
			time.Sleep(time.Second)
			continue
		}
		if resp.StatusCode == http.StatusOK {
			return nil
		}
		return a.qanAPI.Error("PUT", url, resp.StatusCode, http.StatusOK, content)
	}
	return errors.New("timeout 10s waiting on agent to connect to API.")
}

// registerAgent register agent on QAN API using agent installer.
func (a *Admin) registerAgent() error {
	// Remove agent dirs to ensure clean installation. Using full paths to avoid unexpected removals.
	os.RemoveAll(fmt.Sprintf("%s/%s", AgentBaseDir, "config"))
	os.RemoveAll(fmt.Sprintf("%s/%s", AgentBaseDir, "data"))
	os.RemoveAll(fmt.Sprintf("%s/%s", AgentBaseDir, "instance"))

	path := fmt.Sprintf("%s/bin/percona-qan-agent-installer", AgentBaseDir)
	args := []string{"-basedir", AgentBaseDir, "-mysql=false"}
	if a.Config.ServerSSL {
		args = append(args, "-use-ssl")
	}
	if a.Config.ServerInsecureSSL {
		args = append(args, "-use-insecure-ssl")
	}
	if a.Config.ServerPassword != "" {
		args = append(args, fmt.Sprintf("-server-user=%s", a.Config.ServerUser),
			fmt.Sprintf("-server-pass=%s", a.Config.ServerPassword))
	}
	args = append(args, fmt.Sprintf("%s/%s", a.serverURL, qanAPIBasePath))
	if _, err := exec.Command(path, args...).Output(); err != nil {
		return fmt.Errorf("problem with agent registration on QAN API: %s", err)
	}
	return nil
}
