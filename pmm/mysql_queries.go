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
	"os"
	"strings"
	"time"

	consul "github.com/hashicorp/consul/api"
	"github.com/percona/kardianos-service"
	"github.com/percona/pmm/proto"
	pc "github.com/percona/pmm/proto/config"
)

// MySQLQueriesFlags MySQL Queries specific flags.
type MySQLQueriesFlags struct {
	QuerySource string
	// slowlog specific options.
	RetainSlowLogs  int
	SlowLogRotation bool
}

// MySQLQueriesResult is result returned by AddMySQLQueries.
type MySQLQueriesResult struct {
	QuerySource string
}

// AddMySQLQueries add mysql instance to Query Analytics.
func (a *Admin) AddMySQLQueries(mi MySQLInfo, mf MySQLQueriesFlags, qf QueriesFlags) (mr *MySQLQueriesResult, err error) {
	mr = &MySQLQueriesResult{}

	serviceType := "mysql:queries"

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
	instance, err := a.getMySQLInstance(a.ServiceName, parentUUID)
	if err == errNoInstance {
		// Create new instance on QAN.
		instance, err = a.createMySQLInstance(mi, parentUUID)
		if err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}

	// Write instance config for qan-agent with real DSN.
	instance.DSN = mi.DSN
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
			Name:        fmt.Sprintf("pmm-mysql-queries-%d", port),
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
		if err := startService(fmt.Sprintf("pmm-mysql-queries-%d", port)); err != nil {
			return nil, err
		}
	}

	mr.QuerySource = mf.QuerySource
	if mr.QuerySource == "auto" {
		// MySQL is local if the server hostname == MySQL hostname.
		osHostname, _ := os.Hostname()
		if osHostname == mi.Hostname {
			mr.QuerySource = "slowlog"
		} else {
			mr.QuerySource = "perfschema"
		}
	}

	exampleQueries := !qf.DisableQueryExamples
	// Start QAN by associating instance with agent.
	qanConfig := pc.QAN{
		UUID:           instance.UUID,
		CollectFrom:    mr.QuerySource,
		Interval:       60,
		ExampleQueries: &exampleQueries,
		// "slowlog" specific options.
		SlowLogRotation: &mf.SlowLogRotation,
		RetainSlowLogs:  &mf.RetainSlowLogs,
	}
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
		return nil, err
	}

	// Add info to Consul KV.
	d := &consul.KVPair{
		Key:   fmt.Sprintf("%s/%s/%s/dsn", a.Config.ClientName, serviceID, a.ServiceName),
		Value: []byte(mi.SafeDSN),
	}
	a.consulAPI.KV().Put(d, nil)
	d = &consul.KVPair{
		Key:   fmt.Sprintf("%s/%s/%s/qan_mysql_uuid", a.Config.ClientName, serviceID, a.ServiceName),
		Value: []byte(instance.UUID),
	}
	a.consulAPI.KV().Put(d, nil)

	return mr, nil
}

// RemoveMySQLQueries remove mysql instance from QAN.
func (a *Admin) RemoveMySQLQueries() error {
	serviceType := "mysql:queries"

	// Check if we have this service on Consul.
	consulSvc, err := a.getConsulService(serviceType, a.ServiceName)
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
func (a *Admin) createMySQLInstance(mi MySQLInfo, parentUUID string) (proto.Instance, error) {
	in := proto.Instance{
		Subsystem:  "mysql",
		ParentUUID: parentUUID,
		Name:       a.ServiceName,
		DSN:        mi.SafeDSN,
		Distro:     mi.Distro,
		Version:    mi.Version,
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

// updateInstance updates instance on QAN API.
func (a *Admin) updateInstance(inUUID string, bytes []byte) error {
	url := a.qanAPI.URL(a.serverURL, qanAPIBasePath, "instances", inUUID)
	resp, content, err := a.qanAPI.Put(url, bytes)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusNoContent {
		return a.qanAPI.Error("PUT", url, resp.StatusCode, http.StatusNoContent, content)

	}
	return nil
}

// getQueriesOptions reads Queries options from QAN config file.
func getMySQLQueriesOptions(config *pc.QAN) (opts []string) {
	if config.CollectFrom == "slowlog" {
		opts = append(opts, fmt.Sprintf("slow_log_rotation=%t", boolValue(config.SlowLogRotation)))
		opts = append(opts, fmt.Sprintf("retain_slow_logs=%d", intValue(config.RetainSlowLogs)))
	}
	return opts
}
