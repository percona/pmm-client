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
	"strings"

	consul "github.com/hashicorp/consul/api"
	"github.com/roman-vynar/service"
)

// AddMongoDB add mongodb service to monitoring.
func (a *Admin) AddMongoDB(uri, nodetype, replset, cluster string) error {
	// Check if we have already this service on Consul.
	consulSvc, err := a.getConsulService("mongodb", a.ServiceName)
	if err != nil {
		return err
	}
	if consulSvc != nil {
		return errDuplicate
	}

	// Choose port.
	var port uint
	if a.ServicePort > 0 {
		// The port is user defined.
		port, err = a.choosePort(a.ServicePort, true)
	} else {
		// Choose first port available starting the given default one.
		port, err = a.choosePort(42005, false)
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
		ID:      fmt.Sprintf("mongodb-%d", port),
		Service: "mongodb",
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

	// Update os service with mongodb specific tags if exists preserving the port.
	consulSvc, err = a.getConsulService("os", "")
	if err != nil {
		return err
	}
	if consulSvc != nil {
		consulSvc.Tags = tags
		reg = consul.CatalogRegistration{
			Node:    a.Config.ClientName,
			Address: a.Config.ClientAddress,
			Service: consulSvc,
		}
		if _, err := a.consulapi.Catalog().Register(&reg, nil); err != nil {
			return err
		}
	}

	// Add info to Consul KV.
	d := &consul.KVPair{Key: fmt.Sprintf("%s/mongodb-%d/dsn", a.Config.ClientName, port),
		Value: []byte(SanitizeURI(uri))}
	a.consulapi.KV().Put(d, nil)

	// Install and start service via platform service manager.
	svcConfig := &service.Config{
		Name:        fmt.Sprintf("pmm-mongodb-exporter-%d", port),
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

// RemoveMongoDB remove mongodb service from monitoring.
func (a *Admin) RemoveMongoDB(name string) error {
	// Check if we have this service on Consul.
	consulSvc, err := a.getConsulService("mongodb", name)
	if err != nil {
		return err
	}
	if consulSvc == nil {
		return errNoService
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
	if err := uninstallService(fmt.Sprintf("pmm-mongodb-exporter-%d", consulSvc.Port)); err != nil {
		return err
	}

	return nil
}

// SanitizeURI remove password from MongoDB uri
func SanitizeURI(uri string) string {
	if strings.HasPrefix(uri, "mongodb://") {
		uri = uri[10:]
	}

	if strings.Index(uri, "@") > 0 {
		dsnParts := strings.Split(uri, "@")
		userPart := dsnParts[0]
		hostPart := ""
		if len(dsnParts) > 1 {
			hostPart = dsnParts[1]
		}
		userPasswordParts := strings.Split(userPart, ":")
		uri = fmt.Sprintf("%s:***@%s", userPasswordParts[0], hostPart)
	}
	return uri
}
