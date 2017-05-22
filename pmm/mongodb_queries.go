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
	"net/http"
	"strings"
	"time"

	consul "github.com/hashicorp/consul/api"
	"github.com/percona/kardianos-service"
	"github.com/percona/pmm/proto"
	"gopkg.in/mgo.v2"
)

// AddMongoDBQueries add mongodb instance to Query Analytics.
func (a *Admin) AddMongoDBQueries(buildInfo mgo.BuildInfo, uri string) error {
	serviceType := "mongodb:queries"
	dsn := uri
	safeDSN := SanitizeDSN(uri)

	// Check if we have already this service on Consul.
	consulSvc, err := a.getConsulService(serviceType, a.ServiceName)
	if err != nil {
		return err
	}
	if consulSvc != nil {
		return ErrDuplicate
	}

	if err := a.checkGlobalDuplicateService(serviceType, a.ServiceName); err != nil {
		return err
	}

	// Now check if there are any existing services of given service type.
	consulSvc, err = a.getConsulService(serviceType, "")
	if err != nil {
		return err
	}

	// Register agent if config file does not exist.
	agentConfigFile := fmt.Sprintf("%s/config/agent.conf", AgentBaseDir)
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

	// Check if related instance exists or try to re-use the existing one.
	instance, err := a.getMongoDBInstance(a.ServiceName, parentUUID)
	if err == errNoInstance {
		// Create new instance on QAN.
		instance, err = a.createMongoDBInstance(buildInfo, safeDSN, parentUUID)
		if err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	// Write instance config for qan-agent with real DSN.
	instance.DSN = dsn
	bytes, _ := json.MarshalIndent(instance, "", "    ")
	if err := ioutil.WriteFile(fmt.Sprintf("%s/instance/%s.json", AgentBaseDir, instance.UUID), bytes, 0600); err != nil {
		return err
	}

	// Choose port.
	port := 0
	// Don't install service if we have already another one.
	// 1 agent handles multiple instances for QAN.
	if consulSvc == nil {
		// Install and start service via platform service manager.
		// We have to run agent before adding it to QAN.
		svcConfig := &service.Config{
			Name:        fmt.Sprintf("pmm-mongodb-queries-%d", port),
			DisplayName: "PMM MongoDB Query Analytics agent",
			Description: "PMM MongoDB Query Analytics agent",
			Executable:  fmt.Sprintf("%s/bin/percona-qan-agent", AgentBaseDir),
		}
		if err := installService(svcConfig); err != nil {
			return err
		}
	} else {
		port = consulSvc.Port
		// Ensure qan-agent is started if service exists, otherwise it won't be enabled for QAN.
		if err := startService(fmt.Sprintf("pmm-mongodb-queries-%d", port)); err != nil {
			return err
		}
	}

	// Start QAN by associating instance with agent.
	qanConfig := map[string]interface{}{
		"UUID":           instance.UUID,
		"Interval":       60,
		"ExampleQueries": true,
	}
	if err := a.startQAN(agentID, qanConfig); err != nil {
		return err
	}

	tags := []string{
		fmt.Sprintf("alias_%s", a.ServiceName),
		"scheme_https", // What this do?
	}
	// For existing service, we append a new alias_ tag.
	if consulSvc != nil {
		tags = append(consulSvc.Tags, tags...)
	}

	// Add or update service to Consul.
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
		return err
	}

	// Add info to Consul KV.
	d := &consul.KVPair{
		Key:   fmt.Sprintf("%s/%s/%s/dsn", a.Config.ClientName, serviceID, a.ServiceName),
		Value: []byte(safeDSN),
	}
	a.consulAPI.KV().Put(d, nil)
	d = &consul.KVPair{
		Key:   fmt.Sprintf("%s/%s/%s/qan_mongodb_uuid", a.Config.ClientName, serviceID, a.ServiceName),
		Value: []byte(instance.UUID),
	}
	a.consulAPI.KV().Put(d, nil)

	return nil
}

// RemoveMongoDBQueries remove mongodb instance from QAN.
func (a *Admin) RemoveMongoDBQueries() error {
	serviceType := "mongodb:queries"

	// Check if we have this service on Consul.
	consulSvc, err := a.getConsulService(serviceType, a.ServiceName)
	if err != nil {
		return err
	}
	if consulSvc == nil {
		return ErrNoService
	}

	// Ensure qan-agent is started, otherwise it will be an error to stop QAN.
	if err := startService(fmt.Sprintf("pmm-mongodb-queries-%d", consulSvc.Port)); err != nil {
		return err
	}

	// Get UUID of instance the agent is monitoring from KV.
	key := fmt.Sprintf("%s/%s/%s/qan_mongodb_uuid", a.Config.ClientName, consulSvc.ID, a.ServiceName)
	data, _, err := a.consulAPI.KV().Get(key, nil)
	if err != nil {
		return err
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
		if err := uninstallService(fmt.Sprintf("pmm-mongodb-queries-%d", consulSvc.Port)); err != nil {
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

// getMongoDBInstance get or re-use mongodb instance from QAN API and return it.
func (a *Admin) getMongoDBInstance(name, parentUUID string) (proto.Instance, error) {
	var in proto.Instance
	url := a.qanAPI.URL(a.serverURL, qanAPIBasePath, "instances",
		fmt.Sprintf("?type=mongo&name=%s&parent_uuid=%s", name, parentUUID))
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

// createMongoDBInstance create mongodb instance on QAN API and return it.
func (a *Admin) createMongoDBInstance(buildInfo mgo.BuildInfo, safeDSN, parentUUID string) (proto.Instance, error) {
	in := proto.Instance{
		Subsystem:  "mongo",
		ParentUUID: parentUUID,
		Name:       a.ServiceName,
		DSN:        safeDSN,
		Distro:     "MongoDB",
		Version:    buildInfo.Version,
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
