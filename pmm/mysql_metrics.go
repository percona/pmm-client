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
	"database/sql"
	"fmt"
	"strings"

	"context"

	consul "github.com/hashicorp/consul/api"
	"github.com/percona/kardianos-service"
)

// MySQLMetricsFlags MySQL Metrics specific flags.
type MySQLMetricsFlags struct {
	DisableTableStats      bool
	DisableTableStatsLimit uint16
	DisableUserStats       bool
	DisableBinlogStats     bool
	DisableProcesslist     bool
}

// AddMySQLMetrics add mysql metrics service to monitoring.
func (a *Admin) AddMySQLMetrics(ctx context.Context, mi MySQLInfo, mf MySQLMetricsFlags) error {
	serviceType := "mysql:metrics"

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
		port, err = a.choosePort(42002, false)
	}
	// We consider the first port available as okay despite 3 mysql services.
	// @todo What above comments means?
	if err != nil {
		return err
	}

	// Opts to disable.
	var optsToDisable []string
	if !mf.DisableTableStats {
		tableCount, err := tableStatsTableCount(ctx, mi.DSN)
		if err != nil {
			return err
		}
		// Disable table stats if number of tables is higher than limit.
		if uint16(tableCount) > mf.DisableTableStatsLimit {
			mf.DisableTableStats = true
		}
	}
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
	tags := []string{fmt.Sprintf("alias_%s", a.ServiceName), "scheme_https"}

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
		Value: []byte(mi.SafeDSN)}
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
	// Add additional args passed to pmm-admin
	args = append(args, a.Args...)

	// Install and start service via platform service manager.
	svcConfig := &service.Config{
		Name:        fmt.Sprintf("pmm-mysql-metrics-%d", port),
		DisplayName: fmt.Sprintf("PMM Prometheus mysqld_exporter %d", port),
		Description: fmt.Sprintf("PMM Prometheus mysqld_exporter %d", port),
		Executable:  fmt.Sprintf("%s/mysqld_exporter", PMMBaseDir),
		Arguments:   args,
		Environment: []string{fmt.Sprintf("DATA_SOURCE_NAME=%s", mi.DSN)},
	}
	if err := installService(svcConfig); err != nil {
		return err
	}

	return nil
}

// RemoveMySQLMetrics remove mysql metrics service from monitoring.
func (a *Admin) RemoveMySQLMetrics() error {
	serviceType := "mysql:metrics"

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
	if err := uninstallService(fmt.Sprintf("pmm-mysql-metrics-%d", consulSvc.Port)); err != nil {
		return err
	}

	return nil
}

func tableStatsTableCount(ctx context.Context, userDSN string) (int, error) {
	db, err := sql.Open("mysql", userDSN)
	if err != nil {
		return 0, err
	}
	defer db.Close()

	tableCount := 0
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.tables").Scan(&tableCount)
	return tableCount, err
}
