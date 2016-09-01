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
	"fmt"
	"time"

	consul "github.com/hashicorp/consul/api"
	"github.com/percona/kardianos-service"
	"gopkg.in/mgo.v2"
)

// AddMongoDBMetrics add mongodb metrics service to monitoring.
func (a *Admin) AddMongoDBMetrics(uri, nodetype, replset, cluster string) error {
	// Check if we have already this service on Consul.
	consulSvc, err := a.getConsulService("mongodb:metrics", a.ServiceName)
	if err != nil {
		return err
	}
	if consulSvc != nil {
		return ErrDuplicate
	}

	// Choose port.
	var port uint
	if a.ServicePort > 0 {
		// The port is user defined.
		port, err = a.choosePort(a.ServicePort, true)
	} else {
		// Choose first port available starting the given default one.
		port, err = a.choosePort(42003, false)
	}
	if err != nil {
		return err
	}

	tags := []string{fmt.Sprintf("alias_%s", a.ServiceName)}
	if nodetype != "" {
		tags = append(tags, fmt.Sprintf("nodetype_%s", nodetype))
	}
	if replset != "" {
		tags = append(tags, fmt.Sprintf("replset_%s", replset))
	}
	if cluster != "" {
		tags = append(tags, fmt.Sprintf("cluster_%s", cluster))
	}

	// Add service to Consul.
	srv := consul.AgentService{
		ID:      fmt.Sprintf("mongodb:metrics-%d", port),
		Service: "mongodb:metrics",
		Tags:    tags,
		Port:    int(port),
	}
	reg := consul.CatalogRegistration{
		Node:    a.Config.ClientName,
		Address: a.Config.ClientAddress,
		Service: &srv,
	}
	if _, err := a.consulapi.Catalog().Register(&reg, nil); err != nil {
		return err
	}

	// Add info to Consul KV.
	d := &consul.KVPair{Key: fmt.Sprintf("%s/mongodb:metrics-%d/dsn", a.Config.ClientName, port),
		Value: []byte(SanitizeDSN(uri))}
	a.consulapi.KV().Put(d, nil)

	// Install and start service via platform service manager.
	svcConfig := &service.Config{
		Name:        fmt.Sprintf("pmm-mongodb-metrics-%d", port),
		DisplayName: fmt.Sprintf("PMM Prometheus mongodb_exporter %d", port),
		Description: fmt.Sprintf("PMM Prometheus mongodb_exporter %d", port),
		Executable:  fmt.Sprintf("%s/mongodb_exporter", PMMBaseDir),
		Arguments: []string{fmt.Sprintf("-web.listen-address=%s:%d", a.Config.ClientAddress, port),
			fmt.Sprintf("-mongodb.uri=%s", uri)},
	}
	if err := installService(svcConfig); err != nil {
		return err
	}

	return nil
}

// RemoveMongoDBMetrics remove mongodb metrics service from monitoring.
func (a *Admin) RemoveMongoDBMetrics() error {
	// Check if we have this service on Consul.
	consulSvc, err := a.getConsulService("mongodb:metrics", a.ServiceName)
	if err != nil {
		return err
	}
	if consulSvc == nil {
		return ErrNoService
	}

	if err := a.checkGlobalDuplicateService("mongodb:metrics", a.ServiceName); err != nil {
		return err
	}

	// Remove service from Consul.
	dereg := consul.CatalogDeregistration{
		Node:      a.Config.ClientName,
		ServiceID: consulSvc.ID,
	}
	if _, err := a.consulapi.Catalog().Deregister(&dereg, nil); err != nil {
		return err
	}

	prefix := fmt.Sprintf("%s/%s/", a.Config.ClientName, consulSvc.ID)
	a.consulapi.KV().DeleteTree(prefix, nil)

	// Stop and uninstall service.
	if err := uninstallService(fmt.Sprintf("pmm-mongodb-metrics-%d", consulSvc.Port)); err != nil {
		return err
	}

	return nil
}

// DetectMongoDB verify MongoDB connection.
func (a *Admin) DetectMongoDB(uri, nodetype string) error {
	// Check --nodetype flag.
	if nodetype != "" && nodetype != "mongod" && nodetype != "mongos" && nodetype != "config" && nodetype != "arbiter" {
		return fmt.Errorf("Flag --nodetype can take the following values: mongod, mongos, config, arbiter.")
	}

	dialInfo, err := mgo.ParseURL(uri)
	if err != nil {
		return fmt.Errorf("Bad MongoDB uri %s: %s", uri, err)
	}

	dialInfo.Direct = true
	dialInfo.Timeout = 10 * time.Second
	session, err := mgo.DialWithInfo(dialInfo)
	if err != nil {
		return fmt.Errorf("Cannot connect to MongoDB using uri %s: %s", uri, err)
	}
	defer session.Close()

	return nil
}
