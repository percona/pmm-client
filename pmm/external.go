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

	"github.com/prometheus/common/model"

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
		d, err := model.ParseDuration(cfg.ScrapeInterval)
		if err != nil {
			return nil, err
		}
		interval := time.Duration(d)
		d, err = model.ParseDuration(cfg.ScrapeTimeout)
		if err != nil {
			return nil, err
		}
		timeout := time.Duration(d)

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

func (a *Admin) AddExternalService(ctx context.Context, ext *ExternalMetrics, force bool) error {
	resp, err := a.managedAPI.ScrapeConfigsGet(ctx, ext.JobName)
	var found bool
	switch e := err.(type) {
	case nil:
		found = true

		d, err := model.ParseDuration(resp.ScrapeConfig.ScrapeInterval)
		if err != nil {
			return err
		}
		interval := time.Duration(d)
		d, err = model.ParseDuration(resp.ScrapeConfig.ScrapeTimeout)
		if err != nil {
			return err
		}
		timeout := time.Duration(d)

		// if values are not given explicitly , use existing values
		if ext.ScrapeInterval == 0 {
			ext.ScrapeInterval = interval
		}
		if ext.ScrapeTimeout == 0 {
			ext.ScrapeTimeout = timeout
		}
		if ext.MetricsPath == "" {
			ext.MetricsPath = resp.ScrapeConfig.MetricsPath
		}
		if ext.Scheme == "" {
			ext.Scheme = resp.ScrapeConfig.Scheme
		}

		// check if values changed
		if !force {
			if ext.ScrapeInterval != interval {
				return fmt.Errorf("scrape interval changed (requested %s, was %s). Omit --interval flag, or use --force flag.", ext.ScrapeInterval, interval)
			}
			if ext.ScrapeTimeout != timeout {
				return fmt.Errorf("scrape timeout changed (requested %s, was %s). Omit --timeout flag, or use --force flag.", ext.ScrapeTimeout, timeout)
			}
			if ext.MetricsPath != resp.ScrapeConfig.MetricsPath {
				return fmt.Errorf("scrape metrics path changed (requested %q, was %q). Omit --path flag, or use --force flag.", ext.MetricsPath, resp.ScrapeConfig.MetricsPath)
			}
			if ext.Scheme != resp.ScrapeConfig.Scheme {
				return fmt.Errorf("scrapes protocol schema changed (requested %q, was %q). Omit --scheme flag, or use --force flag.", ext.Scheme, resp.ScrapeConfig.Scheme)
			}
		}

	case *managed.Error:
		if e.Code != managed.ErrNotFound {
			return err
		}
	default:
		return err
	}

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

	cfg := &managed.APIScrapeConfig{
		JobName:        ext.JobName,
		ScrapeInterval: model.Duration(ext.ScrapeInterval).String(),
		ScrapeTimeout:  model.Duration(ext.ScrapeTimeout).String(),
		MetricsPath:    ext.MetricsPath,
		Scheme:         ext.Scheme,
		StaticConfigs:  staticConfigs,
	}
	if found {
		return a.managedAPI.ScrapeConfigsUpdate(ctx, &managed.APIScrapeConfigsUpdateRequest{
			ScrapeConfig:      cfg,
			CheckReachability: !force,
		})
	}
	return a.managedAPI.ScrapeConfigsCreate(ctx, &managed.APIScrapeConfigsCreateRequest{
		ScrapeConfig:      cfg,
		CheckReachability: !force,
	})

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

	return a.managedAPI.ScrapeConfigsCreate(ctx, &managed.APIScrapeConfigsCreateRequest{
		ScrapeConfig: &managed.APIScrapeConfig{
			JobName:        ext.JobName,
			ScrapeInterval: model.Duration(ext.ScrapeInterval).String(),
			ScrapeTimeout:  model.Duration(ext.ScrapeTimeout).String(),
			MetricsPath:    ext.MetricsPath,
			Scheme:         ext.Scheme,
			StaticConfigs:  staticConfigs,
		},
		CheckReachability: checkReachability,
	})
}

// RemoveExternalMetrics removes external Prometheus scrape job and targets.
func (a *Admin) RemoveExternalMetrics(ctx context.Context, name string) error {
	return a.managedAPI.ScrapeConfigsDelete(ctx, name)
}

// AddExternalInstances adds targets to existing scrape job.
func (a *Admin) AddExternalInstances(ctx context.Context, name string, targets []ExternalTarget, checkReachability bool) error {
	resp, err := a.managedAPI.ScrapeConfigsGet(ctx, name)
	if err != nil {
		return err
	}

	cfg := resp.ScrapeConfig
	staticConfigs := cfg.StaticConfigs
	for _, t := range targets {
		labels := make([]*managed.APILabelPair, len(t.Labels))
		for i, p := range t.Labels {
			labels[i] = &managed.APILabelPair{Name: p.Name, Value: p.Value}
		}
		staticConfigs = append(staticConfigs, &managed.APIStaticConfig{
			Labels:  labels,
			Targets: []string{t.Target},
		})
	}

	cfg.StaticConfigs = staticConfigs
	return a.managedAPI.ScrapeConfigsUpdate(ctx, &managed.APIScrapeConfigsUpdateRequest{
		ScrapeConfig:      cfg,
		CheckReachability: checkReachability,
	})
}

// RemoveExternalInstances removes targets from existing scrape job.
func (a *Admin) RemoveExternalInstances(ctx context.Context, name string, targets []string) error {
	resp, err := a.managedAPI.ScrapeConfigsGet(ctx, name)
	if err != nil {
		return err
	}

	cfg := resp.ScrapeConfig
	for _, removeT := range targets {
		var newConfigs []*managed.APIStaticConfig
		for i, staticConfig := range cfg.StaticConfigs {
			var newTargets []string
			for _, t := range staticConfig.Targets {
				if removeT != t {
					newTargets = append(newTargets, t)
				}
			}
			cfg.StaticConfigs[i].Targets = newTargets
			if len(newTargets) > 0 {
				newConfigs = append(newConfigs, cfg.StaticConfigs[i])
			}
		}
		cfg.StaticConfigs = newConfigs
	}
	if len(cfg.StaticConfigs) == 0 {
		return a.managedAPI.ScrapeConfigsDelete(ctx, name)
	}
	return a.managedAPI.ScrapeConfigsUpdate(ctx, &managed.APIScrapeConfigsUpdateRequest{
		ScrapeConfig:      cfg,
		CheckReachability: false,
	})
}
