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
	"time"

	"github.com/percona/pmm-client/pmm/managed"
)

type ExternalLabelPair struct {
	Name  string
	Value string
}

type ExternalTarget struct {
	Target string
	Labels []ExternalLabelPair
	Health string
}

// ExternalMetrics represents external Prometheus exporter configuration: job and targets.
// Field names are used for JSON output, so do not rename them.
// JSON output uses Prometheus and pmm-managed API terms; TUI uses terms aligned with other commands.
type ExternalMetrics struct {
	JobName        string
	ScrapeInterval time.Duration // nanoseconds in JSON
	ScrapeTimeout  time.Duration // nanoseconds in JSON
	MetricsPath    string
	Scheme         string
	Targets        []ExternalTarget
}

// ListExternalMetrics returns external Prometheus exporters.
func (a *Admin) ListExternalMetrics(ctx context.Context) ([]ExternalMetrics, error) {
	resp, err := a.managedAPI.ScrapeConfigsList(ctx)
	if err != nil {
		msg := fmt.Sprintf("Error getting a list of external metrics: %s.", err)
		if _, ok := err.(*managed.Error); !ok {
			msg += "\nPlease check versions of your PMM Server and PMM Client."
		}
		return nil, fmt.Errorf("%s", msg)
	}

	res := make([]ExternalMetrics, len(resp.ScrapeConfigs))
	for i, cfg := range resp.ScrapeConfigs {
		interval, err := time.ParseDuration(cfg.ScrapeInterval)
		if err != nil {
			return nil, err
		}
		timeout, err := time.ParseDuration(cfg.ScrapeTimeout)
		if err != nil {
			return nil, err
		}

		var targets []ExternalTarget
		for _, sc := range cfg.StaticConfigs {
			labels := make([]ExternalLabelPair, len(sc.Labels))
			for i, p := range sc.Labels {
				labels[i] = ExternalLabelPair{Name: p.Name, Value: p.Value}
			}
			for _, t := range sc.Targets {
				health := ""
				for _, h := range resp.ScrapeTargetsHealth {
					if h.JobName == cfg.JobName && h.Target == t {
						health = string(h.Health)
					}
				}

				targets = append(targets, ExternalTarget{
					Target: t,
					Labels: labels,
					Health: health,
				})
			}
		}
		res[i] = ExternalMetrics{
			JobName:        cfg.JobName,
			ScrapeInterval: interval,
			ScrapeTimeout:  timeout,
			MetricsPath:    cfg.MetricsPath,
			Scheme:         cfg.Scheme,
			Targets:        targets,
		}
	}
	return res, nil
}

// AddExternalMetrics adds external Prometheus scrape job and targets.
func (a *Admin) AddExternalMetrics(ctx context.Context, ext *ExternalMetrics, checkReachability bool) error {
	var staticConfigs []*managed.APIStaticConfig
	for _, t := range ext.Targets {
		labels := make([]*managed.APILabelPair, len(t.Labels))
		for i, p := range t.Labels {
			labels[i] = &managed.APILabelPair{Name: p.Name, Value: p.Value}
		}
		staticConfigs = append(staticConfigs, &managed.APIStaticConfig{
			Labels:  labels,
			Targets: []string{t.Target},
		})
	}

	err := a.managedAPI.ScrapeConfigsCreate(ctx, &managed.APIScrapeConfigsCreateRequest{
		ScrapeConfig: &managed.APIScrapeConfig{
			JobName:        ext.JobName,
			ScrapeInterval: ext.ScrapeInterval.String(),
			ScrapeTimeout:  ext.ScrapeTimeout.String(),
			MetricsPath:    ext.MetricsPath,
			Scheme:         ext.Scheme,
			StaticConfigs:  staticConfigs,
		},
		CheckReachability: checkReachability,
	})
	if _, ok := err.(*managed.Error); err != nil && !ok {
		return fmt.Errorf("%s\nPlease check versions of your PMM Server and PMM Client.", err)
	}
	return err
}

// RemoveExternalMetrics removes external Prometheus scrape job and targets.
func (a *Admin) RemoveExternalMetrics(ctx context.Context, name string) error {
	err := a.managedAPI.ScrapeConfigsDelete(ctx, name)
	if _, ok := err.(*managed.Error); err != nil && !ok {
		return fmt.Errorf("%s\nPlease check versions of your PMM Server and PMM Client.", err)
	}
	return err
}

// AddExternalInstances adds targets to existing scrape job.
func (a *Admin) AddExternalInstances(ctx context.Context, name string, targets []string, checkReachability bool) error {
	err := a.managedAPI.ScrapeConfigsAddStaticTargets(ctx, &managed.APIScrapeConfigsAddStaticTargetsRequest{
		JobName:           name,
		Targets:           targets,
		CheckReachability: checkReachability,
	})
	if _, ok := err.(*managed.Error); err != nil && !ok {
		return fmt.Errorf("%s\nPlease check versions of your PMM Server and PMM Client.", err)
	}
	return err
}

// RemoveExternalInstances removes targets from existing scrape job.
func (a *Admin) RemoveExternalInstances(ctx context.Context, name string, targets []string) error {
	err := a.managedAPI.ScrapeConfigsRemoveStaticTargets(ctx, &managed.APIScrapeConfigsRemoveStaticTargetsRequest{
		JobName: name,
		Targets: targets,
	})
	if _, ok := err.(*managed.Error); err != nil && !ok {
		return fmt.Errorf("%s\nPlease check versions of your PMM Server and PMM Client.", err)
	}
	return err
}
