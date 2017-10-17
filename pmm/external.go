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
	"time"

	"github.com/percona/pmm-client/pmm/managed"
)

type ExternalMetrics struct {
	Name          string
	Interval      time.Duration
	Timeout       time.Duration
	Path          string
	Scheme        string
	StaticTargets []string
}

// ListExternalMetrics returns external Prometheus exporters.
func (a *Admin) ListExternalMetrics(ctx context.Context) ([]ExternalMetrics, error) {
	resp, err := a.managedAPI.ScrapeConfigsList()
	if err != nil {
		return nil, err
	}

	res := make([]ExternalMetrics, len(resp.ScrapeConfigs))
	for i, sc := range resp.ScrapeConfigs {
		interval, err := time.ParseDuration(sc.ScrapeInterval)
		if err != nil {
			return nil, err
		}
		timeout, err := time.ParseDuration(sc.ScrapeTimeout)
		if err != nil {
			return nil, err
		}

		var targets []string
		for _, c := range sc.StaticConfigs {
			for _, t := range c.Targets {
				targets = append(targets, t)
			}
		}
		res[i] = ExternalMetrics{
			Name:          sc.JobName,
			Interval:      interval,
			Timeout:       timeout,
			Path:          sc.MetricsPath,
			Scheme:        sc.Scheme,
			StaticTargets: targets,
		}
	}
	return res, nil
}

// AddExternalMetrics adds external Prometheus scrape job and targets.
func (a *Admin) AddExternalMetrics(ctx context.Context, ext *ExternalMetrics) error {
	sc := []*managed.APIStaticConfig{{}}
	for _, t := range ext.StaticTargets {
		sc[0].Targets = append(sc[0].Targets, t)
	}

	return a.managedAPI.ScrapeConfigsCreate(&managed.APIScrapeConfigsCreateRequest{
		ScrapeConfig: &managed.APIScrapeConfig{
			JobName:        ext.Name,
			ScrapeInterval: ext.Interval.String(),
			ScrapeTimeout:  ext.Timeout.String(),
			MetricsPath:    ext.Path,
			Scheme:         ext.Scheme,
			StaticConfigs:  sc,
		},
	})
}

// RemoveExternalMetrics removes external Prometheus scrape job and targets.
func (a *Admin) RemoveExternalMetrics(ctx context.Context, name string) error {
	return a.managedAPI.ScrapeConfigsDelete(name)
}
