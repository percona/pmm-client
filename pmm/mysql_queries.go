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
	"strings"
	"time"
	"strconv"

	consul "github.com/hashicorp/consul/api"
	"github.com/percona/kardianos-service"
	"github.com/percona/pmm/proto"
	protocfg "github.com/percona/pmm/proto/config"
)

// AddMySQLQueries add mysql instance to QAN.
func (a *Admin) AddMySQLQueries(info map[string]string) error {
	// Check if we have already this service on Consul.
	consulSvc, err := a.getConsulService("mysql:queries", a.ServiceName)
	if err != nil {
		return err
	}
	if consulSvc != nil {
		return ErrDuplicate
	}

	if err := a.checkGlobalDuplicateService("mysql:queries", a.ServiceName); err != nil {
		return err
	}

	// Now check if there are any mysql:queries services.
	consulSvc, err = a.getConsulService("mysql:queries", "")
	if err != nil {
		return err
	}

	// Register agent if config file does not exist.
	agentConfigFile := fmt.Sprintf("%s/config/agent.conf", agentBaseDir)
	if !FileExists(agentConfigFile) {
		if err := a.registerAgent(); err != nil {
			return err
		}
	}

	agentID, err := getAgentID(agentConfigFile)
	if err != nil {
		return err
	}
	// Get parent_uuid of agent instance.
	parentUUID, err := a.getAgentInstance(agentID)
	if err == errNoInstance {
		// If agent is orphaned, let's re-register it.
		if err := a.registerAgent(); err != nil {
			return err
		}
		// Get new agent id.
		agentID, err = getAgentID(agentConfigFile)
		if err != nil {
			return err
		}
		// Get parent_uuid again.
		parentUUID, err = a.getAgentInstance(agentID)
		if err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	// Check if related MySQL instance exists or try to re-use the existing one.
	mysqlInstance, err := a.getMySQLInstance(a.ServiceName, parentUUID)
	if err == errNoInstance {
		// Create new MySQL instance on QAN.
		mysqlInstance, err = a.createMySQLInstance(info, parentUUID)
		if err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	// Write mysql instance config for qan-agent with real DSN.
	mysqlInstance.DSN = info["dsn"]
	bytes, _ := json.MarshalIndent(mysqlInstance, "", "    ")
	if err := ioutil.WriteFile(fmt.Sprintf("%s/instance/%s.json", agentBaseDir, mysqlInstance.UUID), bytes, 0600); err != nil {
		return err
	}

	// Don't install service if we have already another "mysql:queries".
	// 1 agent handles multiple MySQL instances for QAN.
	port := 0
	if consulSvc == nil {
		// Install and start service via platform service manager.
		// We have to run agent before adding it to QAN.
		svcConfig := &service.Config{
			Name:        fmt.Sprintf("pmm-mysql-queries-%d", port),
			DisplayName: "PMM Query Analytics agent",
			Description: "PMM Query Analytics agent",
			Executable:  fmt.Sprintf("%s/bin/percona-qan-agent", agentBaseDir),
		}
		if err := installService(svcConfig); err != nil {
			return err
		}
	} else {
		port = consulSvc.Port
		// Ensure qan-agent is started if service exists, otherwise it won't be enabled for QAN.
		if err := startService(fmt.Sprintf("pmm-mysql-queries-%d", port)); err != nil {
			return err
		}
	}

	// Start QAN by associating MySQL instance with agent.
	query_examples, _ := strconv.ParseBool(info["query_examples"])
	qanConfig := map[string]interface{}{
		"UUID":           mysqlInstance.UUID,
		"CollectFrom":    info["query_source"],
		"Interval":       60,
		"ExampleQueries": query_examples,
	}
	if err := a.manageQAN(agentID, "StartTool", "", qanConfig); err != nil {
		return err
	}

	tags := []string{fmt.Sprintf("alias_%s", a.ServiceName)}
	// For existing service, we append a new alias_ tag.
	if consulSvc != nil {
		tags = append(consulSvc.Tags, tags...)
	}

	// Add or update service to Consul.
	serviceID := fmt.Sprintf("mysql:queries-%d", port)
	srv := consul.AgentService{
		ID:      serviceID,
		Service: "mysql:queries",
		Tags:    tags,
		Port:    port,
	}
	reg := consul.CatalogRegistration{
		Node:    a.Config.ClientName,
		Address: a.Config.ClientAddress,
		Service: &srv,
	}
	if _, err := a.consulAPI.Catalog().Register(&reg, nil); err != nil {
		return err
	}

	// Add info to Consul KV.
	d := &consul.KVPair{Key: fmt.Sprintf("%s/%s/%s/dsn", a.Config.ClientName, serviceID, a.ServiceName),
		Value: []byte(info["safe_dsn"])}
	a.consulAPI.KV().Put(d, nil)
	d = &consul.KVPair{Key: fmt.Sprintf("%s/%s/%s/qan_mysql_uuid", a.Config.ClientName, serviceID, a.ServiceName),
		Value: []byte(mysqlInstance.UUID)}
	a.consulAPI.KV().Put(d, nil)

	return nil
}

// RemoveMySQLQueries remove mysql instance from QAN.
func (a *Admin) RemoveMySQLQueries() error {
	// Check if we have this service on Consul.
	consulSvc, err := a.getConsulService("mysql:queries", a.ServiceName)
	if err != nil {
		return err
	}
	if consulSvc == nil {
		return ErrNoService
	}

	// Ensure qan-agent is started, otherwise it will be an error to stop QAN.
	if err := startService(fmt.Sprintf("pmm-mysql-queries-%d", consulSvc.Port)); err != nil {
		return err
	}

	// Get UUID of MySQL instance the agent is monitoring from KV.
	key := fmt.Sprintf("%s/%s/%s/qan_mysql_uuid", a.Config.ClientName, consulSvc.ID, a.ServiceName)
	data, _, err := a.consulAPI.KV().Get(key, nil)
	if err != nil {
		return err
	}
	mysqlUUID := string(data.Value)

	// Stop QAN for this MySQL instance on the local agent.
	agentConfigFile := fmt.Sprintf("%s/config/agent.conf", agentBaseDir)
	agentID, err := getAgentID(agentConfigFile)
	if err != nil {
		return err
	}
	if err := a.manageQAN(agentID, "StopTool", mysqlUUID, nil); err != nil {
		return err
	}

	// Delete MySQL instance.
	if err := a.deleteMySQLinstance(mysqlUUID); err != nil {
		return err
	}

	prefix := fmt.Sprintf("%s/%s/%s/", a.Config.ClientName, consulSvc.ID, a.ServiceName)
	a.consulAPI.KV().DeleteTree(prefix, nil)

	// Remove queries service from Consul only if we have only 1 tag alias_ (the instance in question).
	var tags []string
	for _, tag := range consulSvc.Tags {
		if strings.HasPrefix(tag, "alias_") {
			if tag != fmt.Sprintf("alias_%s", a.ServiceName) {
				tags = append(tags, tag)
			}
		}
	}
	if len(tags) == 0 {
		// Remove service from Consul.
		dereg := consul.CatalogDeregistration{
			Node:      a.Config.ClientName,
			ServiceID: consulSvc.ID,
		}
		if _, err := a.consulAPI.Catalog().Deregister(&dereg, nil); err != nil {
			return err
		}

		// Stop and uninstall service.
		if err := uninstallService(fmt.Sprintf("pmm-mysql-queries-%d", consulSvc.Port)); err != nil {
			return err
		}
	} else {
		// Remove tag from service.
		consulSvc.Tags = tags
		reg := consul.CatalogRegistration{
			Node:    a.Config.ClientName,
			Address: a.Config.ClientAddress,
			Service: consulSvc,
		}
		if _, err := a.consulAPI.Catalog().Register(&reg, nil); err != nil {
			return err
		}
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

// getMySQLInstance get or re-use mysql instance from QAN API and return it.
func (a *Admin) getMySQLInstance(name, parentUUID string) (proto.Instance, error) {
	var in proto.Instance
	url := a.qanAPI.URL(a.serverURL, qanAPIBasePath, "instances",
		fmt.Sprintf("?type=mysql&name=%s&parent_uuid=%s", name, parentUUID))
	resp, bytes, err := a.qanAPI.Get(url)
	if err != nil {
		return in, err
	}
	if resp.StatusCode == http.StatusNotFound {
		return in, errNoInstance
	}
	if resp.StatusCode != http.StatusOK {
		return in, a.qanAPI.Error("GET", url, resp.StatusCode, http.StatusOK, bytes)
	}

	if err := json.Unmarshal(bytes, &in); err != nil {
		return in, err
	}
	// Ensure this is the right instance.
	// QAN API 1.0.4 didn't support filtering on parent_uuid, thus returning first record found.
	if in.ParentUUID != parentUUID {
		return in, errNoInstance
	}

	// Instance exists, let's undelete it.
	in.Deleted = time.Unix(1, 0)
	cmdBytes, _ := json.Marshal(in)
	url = a.qanAPI.URL(a.serverURL, qanAPIBasePath, "instances", in.UUID)
	resp, content, err := a.qanAPI.Put(url, cmdBytes)
	if err != nil {
		return in, err
	}
	if resp.StatusCode != http.StatusNoContent {
		return in, a.qanAPI.Error("PUT", url, resp.StatusCode, http.StatusNoContent, content)

	}

	// Ensure it was undeleted.
	// QAN API 1.0.4 didn't support changing "deleted" field.
	url = a.qanAPI.URL(a.serverURL, qanAPIBasePath, "instances", in.UUID)
	resp, bytes, err = a.qanAPI.Get(url)
	if err != nil {
		return in, err
	}
	if resp.StatusCode != http.StatusOK {
		return in, a.qanAPI.Error("GET", url, resp.StatusCode, http.StatusOK, bytes)
	}

	if err := json.Unmarshal(bytes, &in); err != nil {
		return in, err
	}
	// If it's not "1970-01-01 00:00:00 +0000 UTC", it was left deleted.
	if in.Deleted.Year() != 1970 {
		return in, errNoInstance
	}

	return in, nil
}

// createMySQLInstance create mysql instance on QAN API and return it.
func (a *Admin) createMySQLInstance(info map[string]string, parentUUID string) (proto.Instance, error) {
	in := proto.Instance{
		Subsystem:  "mysql",
		ParentUUID: parentUUID,
		Name:       a.ServiceName,
		DSN:        info["safe_dsn"],
		Distro:     info["distro"],
		Version:    info["version"],
	}
	inBytes, _ := json.Marshal(in)
	url := a.qanAPI.URL(a.serverURL, qanAPIBasePath, "instances")
	resp, content, err := a.qanAPI.Post(url, inBytes)
	if err != nil {
		return in, err
	}
	if resp.StatusCode != http.StatusCreated {
		return in, a.qanAPI.Error("POST", url, resp.StatusCode, http.StatusCreated, content)
	}

	// The URI of the new instance is reported in the Location header, fetch it to get UUID assigned.
	// Do not call the returned URL as QAN API returns an invalid one.
	var bytes []byte
	t := strings.Split(resp.Header.Get("Location"), "/")
	url = a.qanAPI.URL(url, t[len(t)-1])
	resp, bytes, err = a.qanAPI.Get(url)
	if err != nil {
		return in, err
	}
	if resp.StatusCode != http.StatusOK {
		return in, a.qanAPI.Error("GET", url, resp.StatusCode, http.StatusOK, bytes)
	}

	if err := json.Unmarshal(bytes, &in); err != nil {
		return in, err
	}

	return in, err
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
	os.RemoveAll("/usr/local/percona/qan-agent/config")
	os.RemoveAll("/usr/local/percona/qan-agent/data")
	os.RemoveAll("/usr/local/percona/qan-agent/instance")

	path := fmt.Sprintf("%s/bin/percona-qan-agent-installer", agentBaseDir)
	args := []string{"-basedir", agentBaseDir, "-mysql=false"}
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

// deleteMySQLinstance delete mysql instance on QAN API.
func (a *Admin) deleteMySQLinstance(mysqlUUID string) error {
	// Remove MySQL instance from QAN.
	url := a.qanAPI.URL(a.serverURL, qanAPIBasePath, "instances", mysqlUUID)
	resp, content, err := a.qanAPI.Delete(url)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusNoContent {
		return a.qanAPI.Error("DELETE", url, resp.StatusCode, http.StatusNoContent, content)
	}

	return nil
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

// getQuerySource read CollectFrom from mysql instance QAN config file.
func getQuerySource(configFile string) (string, error) {
	jsonData, err := ioutil.ReadFile(configFile)
	if err != nil {
		return "", err
	}

	config := &protocfg.QAN{}
	if err := json.Unmarshal(jsonData, &config); err != nil {
		return "", err
	}

	return config.CollectFrom, nil
}

// getQueryExamples read ExampleQueries from mysql instance QAN config file.
func getQueryExamples(configFile string) (string, error) {
	jsonData, err := ioutil.ReadFile(configFile)
	if err != nil {
		return "", err
	}

	config := &protocfg.QAN{}
	if err := json.Unmarshal(jsonData, &config); err != nil {
		return "", err
	}

	return strconv.FormatBool(config.ExampleQueries), nil
}
