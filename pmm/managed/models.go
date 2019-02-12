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

package managed

// Copied from pmm-managed Swagger code.

type APIBasicAuth struct {
	// username
	Username string `json:"username,omitempty"`

	// password
	Password string `json:"password,omitempty"`
}

type APITLSConfig struct {
	// insecure skip verify
	InsecureSkipVerify bool `json:"insecure_skip_verify,omitempty"`
}

type APILabelPair struct {
	// Label name
	Name string `json:"name,omitempty"`

	// Label value
	Value string `json:"value,omitempty"`
}

type APIStaticConfig struct {
	// Labels assigned to all metrics scraped from the targets
	Labels []*APILabelPair `json:"labels"`

	// Hostnames or IPs followed by an optional port number: "1.2.3.4:9090"
	Targets []string `json:"targets"`
}

type APIScrapeConfig struct {
	// The job name assigned to scraped metrics by default: "example-job" (required)
	JobName string `json:"job_name,omitempty"`

	// How frequently to scrape targets from this job: "10s"
	ScrapeInterval string `json:"scrape_interval,omitempty"`

	// Per-scrape timeout when scraping this job: "5s"
	ScrapeTimeout string `json:"scrape_timeout,omitempty"`

	// The HTTP resource path on which to fetch metrics from targets: "/metrics"
	MetricsPath string `json:"metrics_path,omitempty"`

	// Configures the protocol scheme used for requests: "http" or "https"
	Scheme string `json:"scheme,omitempty"`

	// Sets the `Authorization` header on every scrape request with the configured username and password
	BasicAuth *APIBasicAuth `json:"basic_auth,omitempty"`

	// Configures the scrape request's TLS settings
	TLSConfig *APITLSConfig `json:"tls_config,omitempty"`

	// List of labeled statically configured targets for this job
	StaticConfigs []*APIStaticConfig `json:"static_configs"`
}

type ScrapeTargetHealthHealth string

const (
	// ScrapeTargetHealthHealthUNKNOWN captures enum value "UNKNOWN"
	ScrapeTargetHealthHealthUNKNOWN ScrapeTargetHealthHealth = "UNKNOWN"
	// ScrapeTargetHealthHealthDOWN captures enum value "DOWN"
	ScrapeTargetHealthHealthDOWN ScrapeTargetHealthHealth = "DOWN"
	// ScrapeTargetHealthHealthUP captures enum value "UP"
	ScrapeTargetHealthHealthUP ScrapeTargetHealthHealth = "UP"
)

type APIScrapeTargetHealth struct {
	// Original scrape job name
	JobName string `json:"job_name,omitempty"`

	// "job" label value, may be different from job_name due to relabeling
	Job string `json:"job,omitempty"`

	// Original target
	Target string `json:"target,omitempty"`

	// "instance" label value, may be different from target due to relabeling
	Instance string `json:"instance,omitempty"`

	// health
	Health ScrapeTargetHealthHealth `json:"health,omitempty"`
}

type APIScrapeConfigsListResponse struct {
	// scrape configs
	ScrapeConfigs []*APIScrapeConfig `json:"scrape_configs"`

	// Scrape targets health for all managed scrape jobs
	ScrapeTargetsHealth []*APIScrapeTargetHealth `json:"scrape_targets_health"`
}

type APIScrapeConfigsGetResponse struct {
	// scrape config
	ScrapeConfig *APIScrapeConfig `json:"scrape_config,omitempty"`

	// Scrape targets health for this scrape job
	ScrapeTargetsHealth []*APIScrapeTargetHealth `json:"scrape_targets_health"`
}

type APIScrapeConfigsCreateRequest struct {
	// scrape config
	ScrapeConfig *APIScrapeConfig `json:"scrape_config,omitempty"`

	// Check that added targets can be scraped from PMM Server
	CheckReachability bool `json:"check_reachability,omitempty"`
}

type APIScrapeConfigsUpdateRequest struct {
	// scrape config
	ScrapeConfig *APIScrapeConfig `json:"scrape_config,omitempty"`

	// Check that added targets can be scraped from PMM Server
	CheckReachability bool `json:"check_reachability,omitempty"`
}

// APIAnnotationCreateRequest request to create an annotation at managed.
type APIAnnotationCreateRequest struct {
	// list of tags (optional)
	Tags []string `json:"tags,omitempty"`
	// description of annotation
	Text string `json:"text"`
}

// VersionResponse response server version.
type VersionResponse struct {
	Version string
}
