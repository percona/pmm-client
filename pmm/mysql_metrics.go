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
	"strconv"
	"strings"

	consul "github.com/hashicorp/consul/api"
	"github.com/percona/kardianos-service"
)

// AddMySQLMetrics add mysql metrics service to monitoring.
func (a *Admin) AddMySQLMetrics(info map[string]string, mf MySQLFlags) error {
	// Check if we have already this service on Consul.
	consulSvc, err := a.getConsulService("mysql:metrics", a.ServiceName)
	if err != nil {
		return err
	}
	if consulSvc != nil {
		return ErrDuplicate
	}

	if err := a.checkGlobalDuplicateService("mysql:metrics", a.ServiceName); err != nil {
		return err
	}

	// Choose port.
	var port uint16
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
	count, _ := strconv.ParseUint(info["table_count"], 10, 16)
	if mf.DisableTableStats || uint16(count) > mf.DisableTableStatsLimit {
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

	// Add service to Consul.
	serviceID := fmt.Sprintf("mysql:metrics-%d", port)
	srv := consul.AgentService{
		ID:      serviceID,
		Service: "mysql:metrics",
		Tags:    []string{fmt.Sprintf("alias_%s", a.ServiceName), "scheme_https"},
		Port:    int(port),
	}
	reg := consul.CatalogRegistration{
		Node:    a.Config.ClientName,
		Address: a.Config.ClientAddress,
		Service: &srv,
	}
	if _, err := a.consulAPI.Catalog().Register(&reg, nil); err != nil {
		return err
	}

	// Disable exporter options if set so.
	args := mysqldExporterArgs
	for _, o := range optsToDisable {
		for _, f := range mysqldExporterDisableArgs[o] {
			for i, a := range mysqldExporterArgs {
				if strings.HasPrefix(a, f) {
					args[i] = fmt.Sprintf("%sfalse", f)
					break
				}
			}
		}

		// Add info to Consul KV.
		d := &consul.KVPair{Key: fmt.Sprintf("%s/%s/%s", a.Config.ClientName, serviceID, o),
			Value: []byte("OFF")}
		a.consulAPI.KV().Put(d, nil)
	}

	d := &consul.KVPair{Key: fmt.Sprintf("%s/%s/dsn", a.Config.ClientName, serviceID),
		Value: []byte(info["safe_dsn"])}
	a.consulAPI.KV().Put(d, nil)

	// Check and generate certificate if needed.
	if err := a.checkSSLCertificate(); err != nil {
		return err
	}

	args = append(args,
		fmt.Sprintf("-web.listen-address=%s:%d", a.Config.BindAddress, port),
		fmt.Sprintf("-web.auth-file=%s", ConfigFile),
		fmt.Sprintf("-web.ssl-cert-file=%s", SSLCertFile),
		fmt.Sprintf("-web.ssl-key-file=%s", SSLKeyFile),
	)

	// Install and start service via platform service manager.
	svcConfig := &service.Config{
		Name:        fmt.Sprintf("pmm-mysql-metrics-%d", port),
		DisplayName: fmt.Sprintf("PMM Prometheus mysqld_exporter %d", port),
		Description: fmt.Sprintf("PMM Prometheus mysqld_exporter %d", port),
		Executable:  fmt.Sprintf("%s/mysqld_exporter", PMMBaseDir),
		Arguments:   args,
		Environment: []string{fmt.Sprintf("DATA_SOURCE_NAME=%s", info["dsn"])},
	}
	if err := installService(svcConfig); err != nil {
		return err
	}

	return nil
}

// RemoveMySQLMetrics remove mysql metrics service from monitoring.
func (a *Admin) RemoveMySQLMetrics() error {
	// Check if we have this service on Consul.
	consulSvc, err := a.getConsulService("mysql:metrics", a.ServiceName)
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
	if err := uninstallService(fmt.Sprintf("pmm-mysql-metrics-%d", consulSvc.Port)); err != nil {
		return err
	}

	return nil
}
