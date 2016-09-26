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

// AddLinuxMetrics add linux service to monitoring.
func (a *Admin) AddLinuxMetrics(force bool) error {
	// Check if we have already this service on Consul.
	// When using force, we allow adding another service with different name.
	name := ""
	if force {
		name = a.ServiceName
	}
	consulSvc, err := a.getConsulService("linux:metrics", name)
	if err != nil {
		return err
	}
	if consulSvc != nil {
		if force {
			return ErrDuplicate
		}
		return ErrOneLinux
	}

	if err := a.checkGlobalDuplicateService("linux:metrics", a.ServiceName); err != nil {
		return err
	}

	// Choose port.
	var port uint16
	if a.ServicePort > 0 {
		// The port is user defined.
		port, err = a.choosePort(a.ServicePort, true)
	} else {
		// Choose first port available starting the given default one.
		port, err = a.choosePort(42000, false)
	}
	if err != nil {
		return err
	}

	// Add service to Consul.
	srv := consul.AgentService{
		ID:      fmt.Sprintf("linux:metrics-%d", port),
		Service: "linux:metrics",
		Tags:    []string{fmt.Sprintf("alias_%s", a.ServiceName)},
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

	// Install and start service via platform service manager.
	svcConfig := &service.Config{
		Name:        fmt.Sprintf("pmm-linux-metrics-%d", port),
		DisplayName: "PMM Prometheus node_exporter",
		Description: "PMM Prometheus node_exporter",
		Executable:  fmt.Sprintf("%s/node_exporter", PMMBaseDir),
		Arguments: []string{fmt.Sprintf("-web.listen-address=%s:%d", a.Config.ClientAddress, port),
			nodeExporterArgs},
	}
	if err := installService(svcConfig); err != nil {
		return err
	}

	return nil
}

// RemoveLinuxMetrics remove linux service from monitoring.
func (a *Admin) RemoveLinuxMetrics() error {
	// Check if we have this service on Consul.
	consulSvc, err := a.getConsulService("linux:metrics", a.ServiceName)
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
	if _, err := a.consulapi.Catalog().Deregister(&dereg, nil); err != nil {
		return err
	}

	// Stop and uninstall service.
	if err := uninstallService(fmt.Sprintf("pmm-linux-metrics-%d", consulSvc.Port)); err != nil {
		return err
	}

	return nil
}
