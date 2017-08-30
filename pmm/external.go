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

	"github.com/percona/pmm-managed/api"
)

type ExternalMetrics struct {
	Name          string
	Interval      time.Duration
	Timeout       time.Duration
	Path          string
	Scheme        string
	StatisTargets []string
}

// AddExternalMetrics adds external Prometheus metrics exporter to monitoring.
func (a *Admin) AddExternalMetrics(ctx context.Context, ext *ExternalMetrics) error {
	_, err := a.managedAPI.ScrapeJobsClient.Create(ctx, &api.ScrapeJobsCreateRequest{
		ScrapeJob: &api.ScrapeJob{
			Name:          ext.Name,
			Interval:      ext.Interval.String(),
			Timeout:       ext.Timeout.String(),
			Path:          ext.Path,
			Scheme:        ext.Scheme,
			StatisTargets: ext.StatisTargets,
		},
	})
	return err
}

// RemoveExternalMetrics removes external Prometheus metrics exporter from monitoring.
func (a *Admin) RemoveExternalMetrics(ctx context.Context, name string) error {
	_, err := a.managedAPI.ScrapeJobsClient.Delete(ctx, &api.ScrapeJobsDeleteRequest{
		Name: name,
	})
	return err
}

// ListExternalMetrics lists external Prometheus metrics exporters from monitoring.
func (a *Admin) ListExternalMetrics(ctx context.Context) ([]ExternalMetrics, error) {
	resp, err := a.managedAPI.ScrapeJobsClient.List(ctx, &api.ScrapeJobsListRequest{})
	if err != nil {
		return nil, err
	}

	res := make([]ExternalMetrics, len(resp.ScrapeJobs))
	for i, j := range resp.ScrapeJobs {
		interval, err := time.ParseDuration(j.Interval)
		if err != nil {
			return nil, err
		}
		timeout, err := time.ParseDuration(j.Timeout)
		if err != nil {
			return nil, err
		}
		res[i] = ExternalMetrics{
			Name:          j.Name,
			Interval:      interval,
			Timeout:       timeout,
			Path:          j.Path,
			Scheme:        j.Scheme,
			StatisTargets: j.StatisTargets,
		}
	}
	return res, nil
}
