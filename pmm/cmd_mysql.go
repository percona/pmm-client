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

// AddMySQL add mysql services to monitoring.
func (a *Admin) AddMySQL(info map[string]string, mf MySQLFlags) error {
	// Check if we have already this service on Consul.
	consulSvc, err := a.getConsulService("mysql-hr", a.ServiceName)
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
		port, err = a.choosePort(42002, false)
	}
	// We consider the first port available as okay despite 3 mysql services.
	if err != nil {
		return err
	}

	// Opts to disable.
	var optsToDisable []string
	if mf.DisableTableStats {
		optsToDisable = append(optsToDisable, "tablestats")
	}
	if mf.DisableUserStats {
		optsToDisable = append(optsToDisable, "userstats")
	}
	if mf.DisableBinlogStats {
		optsToDisable = append(optsToDisable, "binlogstats")
	}
	if mf.DisableProcesslist {
		optsToDisable = append(optsToDisable, "processlist")
	}

	for job, port := range map[string]uint{"mysql-hr": port, "mysql-mr": port + 1, "mysql-lr": port + 2} {
		// Add service to Consul.
		serviceID := fmt.Sprintf("%s-%d", job, port)
		srv := consul.AgentService{
			ID:      serviceID,
			Service: job,
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

		// Add info to Consul KV.
		d := &consul.KVPair{Key: fmt.Sprintf("%s/%s/dsn", a.Config.ClientName, serviceID),
			Value: []byte(info["safe_dsn"])}
		a.consulapi.KV().Put(d, nil)

		// Disable exporter options if set so.
		args := mysqldExporterArgs[job]
		for _, o := range optsToDisable {
			for _, f := range mysqldExporterDisableArgs[o] {
				for i, a := range args {
					if strings.HasPrefix(a, f) {
						args[i] = fmt.Sprintf("%sfalse", f)
						break
					}
				}
			}
			d := &consul.KVPair{Key: fmt.Sprintf("%s/%s/%s", a.Config.ClientName, serviceID, o),
				Value: []byte("OFF")}
			a.consulapi.KV().Put(d, nil)
		}

		// Install and start service via platform service manager.
		svcConfig := &service.Config{
			Name:        fmt.Sprintf("pmm-mysql-exporter-%d", port),
			DisplayName: fmt.Sprintf("PMM Prometheus mysqld_exporter %d", port),
			Description: fmt.Sprintf("PMM Prometheus mysqld_exporter %d", port),
			Executable:  fmt.Sprintf("%s/mysqld_exporter", PMMBaseDir),
			Arguments:   append(args, fmt.Sprintf("-web.listen-address=%s:%d", a.Config.ClientAddress, port)),
			Option:      service.KeyValue{"Environment": fmt.Sprintf("DATA_SOURCE_NAME=%s", info["dsn"])},
		}
		if err := installService(svcConfig); err != nil {
			return err
		}
	}

	return nil
}

// RemoveMySQL remove mysql services from monitoring.
func (a *Admin) RemoveMySQL(name string) error {
	// Check if we have this service on Consul.
	consulSvc, err := a.getConsulService("mysql-hr", name)
	if err != nil {
		return err
	}
	if consulSvc == nil {
		return errNoService
	}

	for job, port := range map[string]int{"mysql-hr": consulSvc.Port, "mysql-mr": consulSvc.Port + 1,
		"mysql-lr": consulSvc.Port + 2} {
		serviceID := fmt.Sprintf("%s-%d", job, port)
		// Remove service from Consul.
		dereg := consul.CatalogDeregistration{
			Node:      a.Config.ClientName,
			ServiceID: serviceID,
		}
		if _, err := a.consulapi.Catalog().Deregister(&dereg, nil); err != nil {
			return err
		}

		prefix := fmt.Sprintf("%s/%s/", a.Config.ClientName, serviceID)
		a.consulapi.KV().DeleteTree(prefix, nil)

		// Stop and uninstall service.
		if err := uninstallService(fmt.Sprintf("pmm-mysql-exporter-%d", port)); err != nil {
			return err
		}
	}

	return nil
}
