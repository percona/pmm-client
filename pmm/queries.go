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
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	consul "github.com/hashicorp/consul/api"
	"github.com/percona/kardianos-service"
	"github.com/percona/pmm-client/pmm/plugin"
	"github.com/percona/pmm-client/pmm/utils"
	"github.com/percona/pmm/proto"
	pc "github.com/percona/pmm/proto/config"
)

// AddQueries add instance to Query Analytics.
func (a *Admin) AddQueries(ctx context.Context, q plugin.Queries) (*plugin.Info, error) {
	info, err := q.Init(ctx, a.Config.MySQLPassword)
	if err != nil {
		return nil, err
	}

	if info.PMMUserPassword != "" {
		a.Config.MySQLPassword = info.PMMUserPassword
		err := a.writeConfig()
		if err != nil {
			return nil, err
		}
	}

	serviceType := fmt.Sprintf("%s:queries", q.Name())

	// Check if we have already this service on Consul.
	consulSvc, err := a.getConsulService(serviceType, a.ServiceName)
	if err != nil {
		return nil, err
	}
	if consulSvc != nil {
		return nil, ErrDuplicate
	}

	if err := a.checkGlobalDuplicateService(serviceType, a.ServiceName); err != nil {
		return nil, err
	}

	// Now check if there are any existing services of given service type.
	consulSvc, err = a.getConsulService(serviceType, "")
	if err != nil {
		return nil, err
	}

	// Register agent if config file does not exist.
	agentConfigFile := fmt.Sprintf("%s/config/agent.conf", AgentBaseDir)
	if !FileExists(agentConfigFile) {
		if err := a.registerAgent(); err != nil {
			return nil, err
		}
	}

	agentID, err := getAgentID(agentConfigFile)
	if err != nil {
		return nil, err
	}
	// Get parent_uuid of agent instance.
	parentUUID, err := a.getAgentInstance(agentID)
	if err == errNoInstance {
		// If agent is orphaned, let's re-register it.
		if err := a.registerAgent(); err != nil {
			return nil, err
		}
		// Get new agent id.
		agentID, err = getAgentID(agentConfigFile)
		if err != nil {
			return nil, err
		}
		// Get parent_uuid again.
		parentUUID, err = a.getAgentInstance(agentID)
		if err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}

	// Check if related instance exists or try to re-use the existing one.
	instance, err := a.getInstance(q.InstanceTypeName(), a.ServiceName, parentUUID)
	if err == errNoInstance {
		// Create new instance on QAN.
		instance, err = a.createInstance(q.InstanceTypeName(), *info, parentUUID)
		if err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}

	// Write instance config for qan-agent with real DSN.
	instance.DSN = info.DSN
	bytes, _ := json.MarshalIndent(instance, "", "    ")
	if err := ioutil.WriteFile(fmt.Sprintf("%s/instance/%s.json", AgentBaseDir, instance.UUID), bytes, 0600); err != nil {
		return nil, err
	}

	// Choose port.
	port := 0
	// Don't install service if we have already another one.
	// 1 agent handles multiple instances for QAN.
	if consulSvc == nil {
		// Install and start service via platform service manager.
		// We have to run agent before adding it to QAN.
		svcConfig := &service.Config{
			Name:        fmt.Sprintf("pmm-%s-queries-%d", q.Name(), port),
			DisplayName: "PMM Query Analytics agent",
			Description: "PMM Query Analytics agent",
			Executable:  fmt.Sprintf("%s/bin/percona-qan-agent", AgentBaseDir),
			Arguments:   a.Args,
		}
		if err := installService(svcConfig); err != nil {
			return nil, err
		}
	} else {
		port = consulSvc.Port
		// Ensure qan-agent is started if service exists, otherwise it won't be enabled for QAN.
		if err := startService(fmt.Sprintf("pmm-%s-queries-%d", q.Name(), port)); err != nil {
			return nil, err
		}
	}

	// Start QAN by associating instance with agent.
	qanConfig := q.Config()
	qanConfig.UUID = instance.UUID
	qanConfig.Interval = 60
	if err := a.startQAN(agentID, qanConfig); err != nil {
		return nil, err
	}

	tags := []string{
		fmt.Sprintf("alias_%s", a.ServiceName),
	}
	// For existing service, we append a new alias_ tag.
	if consulSvc != nil {
		tags = append(consulSvc.Tags, tags...)
	}

	// Add service to Consul.
	serviceID := fmt.Sprintf("%s-%d", serviceType, port)
	srv := consul.AgentService{
		ID:      serviceID,
		Service: serviceType,
		Tags:    tags,
		Port:    port,
	}
	reg := consul.CatalogRegistration{
		Node:    a.Config.ClientName,
		Address: a.Config.ClientAddress,
		Service: &srv,
	}
	if _, err := a.consulAPI.Catalog().Register(&reg, nil); err != nil {
		return nil, err
	}

	// Add info to Consul KV.
	d := &consul.KVPair{
		Key:   fmt.Sprintf("%s/%s/%s/dsn", a.Config.ClientName, serviceID, a.ServiceName),
		Value: []byte(utils.SanitizeDSN(info.DSN)),
	}
	_, err = a.consulAPI.KV().Put(d, nil)
	if err != nil {
		return nil, err
	}
	d = &consul.KVPair{
		Key:   fmt.Sprintf("%s/%s/%s/qan_%s_uuid", a.Config.ClientName, serviceID, a.ServiceName, q.Name()),
		Value: []byte(instance.UUID),
	}
	_, err = a.consulAPI.KV().Put(d, nil)
	if err != nil {
		return nil, err
	}

	return info, nil
}

// RemoveQueries remove instance from QAN.
func (a *Admin) RemoveQueries(name string) error {
	serviceType := fmt.Sprintf("%s:queries", name)

	// Check if we have this service on Consul.
	consulSvc, err := a.getConsulService(serviceType, a.ServiceName)
	if err != nil {
		return err
	}
	if consulSvc == nil {
		return ErrNoService
	}

	// Ensure qan-agent is started, otherwise it will be an error to stop QAN.
	if err := startService(fmt.Sprintf("pmm-%s-queries-%d", name, consulSvc.Port)); err != nil {
		return err
	}

	// Get UUID of MySQL instance the agent is monitoring from KV.
	key := fmt.Sprintf("%s/%s/%s/qan_%s_uuid", a.Config.ClientName, consulSvc.ID, a.ServiceName, name)
	data, _, err := a.consulAPI.KV().Get(key, nil)
	if err != nil {
		return err
	}
	if data == nil {
		return fmt.Errorf("can't get key %s", key)
	}
	uuid := string(data.Value)

	// Stop QAN for this instance on the local agent.
	agentConfigFile := fmt.Sprintf("%s/config/agent.conf", AgentBaseDir)
	agentID, err := getAgentID(agentConfigFile)
	if err != nil {
		return err
	}
	if err := a.stopQAN(agentID, uuid); err != nil {
		return err
	}

	// Delete instance.
	if err := a.deleteInstance(uuid); err != nil {
		return err
	}

	prefix := fmt.Sprintf("%s/%s/%s/", a.Config.ClientName, consulSvc.ID, a.ServiceName)
	_, err = a.consulAPI.KV().DeleteTree(prefix, nil)
	if err != nil {
		return err
	}

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
		if err := uninstallService(fmt.Sprintf("pmm-%s-queries-%d", name, consulSvc.Port)); err != nil {
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

// getInstance get or re-use instance from QAN API and return it.
func (a *Admin) getInstance(subsystem, name, parentUUID string) (proto.Instance, error) {
	var in proto.Instance
	url := a.qanAPI.URL(a.serverURL, qanAPIBasePath, "instances",
		fmt.Sprintf("?type=%s&name=%s&parent_uuid=%s", subsystem, name, parentUUID))
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

// createInstance create instance on QAN API and return it.
func (a *Admin) createInstance(subsystem string, info plugin.Info, parentUUID string) (proto.Instance, error) {
	in := proto.Instance{
		Subsystem:  subsystem,
		ParentUUID: parentUUID,
		Name:       a.ServiceName,
		DSN:        utils.SanitizeDSN(info.DSN),
		Distro:     info.Distro,
		Version:    info.Version,
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

	config := &pc.Agent{}
	if err := json.Unmarshal(jsonData, &config); err != nil {
		return "", err
	}

	if config.UUID == "" {
		return "", fmt.Errorf("missing agent UUID in config file %s", configFile)
	}

	return config.UUID, nil
}

// startQAN enable QAN on agent through QAN API.
func (a *Admin) startQAN(agentID string, config pc.QAN) error {
	cmdName := "StartTool"
	data, err := json.Marshal(config)
	if err != nil {
		return err
	}

	return a.sendQANCmd(agentID, cmdName, data)
}

// stopQAN disable QAN on agent through QAN API.
func (a *Admin) stopQAN(agentID, UUID string) error {
	cmdName := "StopTool"
	data := []byte(UUID)

	return a.sendQANCmd(agentID, cmdName, data)
}

// sendQANCmd sends cmd to agent throughq QAN API.
func (a *Admin) sendQANCmd(agentID, cmdName string, data []byte) error {
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
	return errors.New("timeout 10s waiting on agent to connect to API")
}

// registerAgent register agent on QAN API using agent installer.
func (a *Admin) registerAgent() error {
	// Remove agent dirs to ensure clean installation. Using full paths to avoid unexpected removals.
	os.RemoveAll(fmt.Sprintf("%s/%s", AgentBaseDir, "config"))
	os.RemoveAll(fmt.Sprintf("%s/%s", AgentBaseDir, "data"))
	os.RemoveAll(fmt.Sprintf("%s/%s", AgentBaseDir, "instance"))

	path := fmt.Sprintf("%s/bin/percona-qan-agent-installer", AgentBaseDir)
	args := []string{"-basedir", AgentBaseDir}
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
		if exitErr, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("problem with agent registration on QAN API: %s\n%s", err, exitErr.Stderr)
		}
		return fmt.Errorf("problem with agent registration on QAN API: %s", err)
	}
	return nil
}

// getProtoQAN reads instance from QAN config file.
func getProtoQAN(configFile string) (*pc.QAN, error) {
	jsonData, err := ioutil.ReadFile(configFile)
	if err != nil {
		return nil, err
	}

	config := pc.NewQAN()
	if err := json.Unmarshal(jsonData, &config); err != nil {
		return nil, err
	}

	return &config, nil
}

// boolValue returns the value of the bool pointer passed in or
// false if the pointer is nil.
func boolValue(v *bool) bool {
	if v != nil {
		return *v
	}
	return false
}

// intValue returns the value of the int pointer passed in or
// 0 if the pointer is nil.
func intValue(v *int) int {
	if v != nil {
		return *v
	}
	return 0
}
