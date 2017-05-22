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

	consul "github.com/hashicorp/consul/api"
	"github.com/percona/kardianos-service"
)

// AddMongoDBMetrics add mongodb metrics service to monitoring.
func (a *Admin) AddMongoDBMetrics(uri, cluster string) error {
	serviceType := "mongodb:metrics"

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

	// Choose port.
	port := 0
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

	tags := []string{fmt.Sprintf("alias_%s", a.ServiceName), "scheme_https"}
	if cluster != "" {
		tags = append(tags, fmt.Sprintf("cluster_%s", cluster))
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
		return err
	}

	// Add info to Consul KV.
	d := &consul.KVPair{Key: fmt.Sprintf("%s/%s/dsn", a.Config.ClientName, serviceID),
		Value: []byte(SanitizeDSN(uri))}
	a.consulAPI.KV().Put(d, nil)

	// Check and generate certificate if needed.
	if err := a.checkSSLCertificate(); err != nil {
		return err
	}

	args := []string{
		fmt.Sprintf("-web.listen-address=%s:%d", a.Config.BindAddress, port),
		fmt.Sprintf("-web.auth-file=%s", ConfigFile),
		fmt.Sprintf("-web.ssl-cert-file=%s", SSLCertFile),
		fmt.Sprintf("-web.ssl-key-file=%s", SSLKeyFile),
	}

	// Install and start service via platform service manager.
	svcConfig := &service.Config{
		Name:        fmt.Sprintf("pmm-mongodb-metrics-%d", port),
		DisplayName: fmt.Sprintf("PMM Prometheus mongodb_exporter %d", port),
		Description: fmt.Sprintf("PMM Prometheus mongodb_exporter %d", port),
		Executable:  fmt.Sprintf("%s/mongodb_exporter", PMMBaseDir),
		Arguments:   args,
		Environment: []string{fmt.Sprintf("MONGODB_URI=%s", uri)},
	}
	if err := installService(svcConfig); err != nil {
		return err
	}

	return nil
}

// RemoveMongoDBMetrics remove mongodb metrics service from monitoring.
func (a *Admin) RemoveMongoDBMetrics() error {
	serviceType := "mongodb:metrics"

	// Check if we have this service on Consul.
	consulSvc, err := a.getConsulService(serviceType, a.ServiceName)
	if err != nil {
		return err
	}
	if consulSvc == nil {
		return ErrNoService
	}

	// Remove service from Consul.
	dereg := consul.CatalogDeregistration{
		Node:      a.Config.ClientName,
		ServiceID: consulSvc.ID,
	}
	if _, err := a.consulAPI.Catalog().Deregister(&dereg, nil); err != nil {
		return err
	}

	prefix := fmt.Sprintf("%s/%s/", a.Config.ClientName, consulSvc.ID)
	a.consulAPI.KV().DeleteTree(prefix, nil)

	// Stop and uninstall service.
	if err := uninstallService(fmt.Sprintf("pmm-mongodb-metrics-%d", consulSvc.Port)); err != nil {
		return err
	}

	return nil
}
