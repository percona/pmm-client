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
	"fmt"
	"path/filepath"

	consul "github.com/hashicorp/consul/api"
	service "github.com/percona/kardianos-service"
	"github.com/percona/pmm-client/pmm/plugin"
)

// AddMetrics add metrics service to monitoring.
func (a *Admin) AddMetrics(ctx context.Context, m plugin.Metrics, force bool, disableSSL bool) (*plugin.Info, error) {
	info, err := m.Init(ctx, a.Config.MySQLPassword)
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

	serviceType := fmt.Sprintf("%s:metrics", m.Name())

	// Check if we have already this service on Consul.
	// When using force, we allow adding another service with different name.
	name := ""
	if m.Multiple() || force {
		name = a.ServiceName
	}
	consulSvc, err := a.getConsulService(serviceType, name)
	if err != nil {
		return nil, err
	}
	if consulSvc != nil {
		return nil, ErrDuplicate
	}

	if err := a.checkGlobalDuplicateService(serviceType, a.ServiceName); err != nil {
		return nil, err
	}

	// Choose port.
	defaultPort := m.DefaultPort()
	port, err := a.choosePort(a.ServicePort, defaultPort)
	if err != nil {
		return nil, err
	}

	scheme := "scheme_https"
	if disableSSL {
		scheme = "scheme_http"
	}
	tags := []string{
		fmt.Sprintf("alias_%s", a.ServiceName),
		scheme,
	}
	if m.Cluster() != "" {
		tags = append(tags, fmt.Sprintf("cluster_%s", m.Cluster()))
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
	for i, v := range m.KV() {
		d := &consul.KVPair{
			Key:   fmt.Sprintf("%s/%s/%s", a.Config.ClientName, serviceID, i),
			Value: v,
		}
		_, err = a.consulAPI.KV().Put(d, nil)
		if err != nil {
			return nil, err
		}
	}

	args := []string{
		fmt.Sprintf("-web.listen-address=%s:%d", a.Config.BindAddress, port),
		fmt.Sprintf("-web.auth-file=%s", ConfigFile),
	}

	if !disableSSL {
		// Check and generate certificate if needed.
		if err := a.checkSSLCertificate(); err != nil {
			return nil, err
		}
		args = append(args,
			fmt.Sprintf("-web.ssl-key-file=%s", SSLKeyFile),
			fmt.Sprintf("-web.ssl-cert-file=%s", SSLCertFile),
		)
	}

	// Add additional args passed by plugin.
	args = append(args, m.Args()...)
	// Add additional args passed to pmm-admin.
	args = append(args, a.Args...)

	_, executable := filepath.Split(m.Executable())
	if executable == "" {
		return nil, fmt.Errorf("%s: invalid executable name: %s", m.Name(), m.Executable())
	}

	// Install and start service via platform service manager.
	svcConfig := &service.Config{
		Name:        fmt.Sprintf("pmm-%s-metrics-%d", m.Name(), port),
		DisplayName: fmt.Sprintf("PMM Prometheus %s on port %d", m.Executable(), port),
		Description: fmt.Sprintf("PMM Prometheus %s on port %d", m.Executable(), port),
		Executable:  filepath.Join(PMMBaseDir, executable),
		Arguments:   args,
	}
	svcConfig.Environment = append(svcConfig.Environment, m.Environment()...)
	if err := installService(svcConfig); err != nil {
		return nil, err
	}

	return info, nil
}

// RemoveMetrics remove metrics service from monitoring.
func (a *Admin) RemoveMetrics(name string) error {
	serviceType := fmt.Sprintf("%s:metrics", name)

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
	_, err = a.consulAPI.KV().DeleteTree(prefix, nil)
	if err != nil {
		return err
	}

	// Stop and uninstall service.
	serviceName := fmt.Sprintf("pmm-%s-metrics-%d", name, consulSvc.Port)
	if err := uninstallService(serviceName); err != nil {
		return err
	}

	return nil
}
