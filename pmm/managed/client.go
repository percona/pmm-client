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

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"path"

	"github.com/percona/pmm-client/pmm/utils"
)

type Error struct {
	Err  string `json:"error"`
	Code int    `json:"code"`
}

func (e Error) Error() string {
	return e.Err
}

type Client struct {
	client   *http.Client
	host     string
	scheme   string
	user     *url.Userinfo
	basePath string
}

const debug = false

func NewClient(host string, scheme string, user *url.Userinfo, insecureSSL bool) *Client {
	transport := &http.Transport{}
	if insecureSSL {
		transport.TLSClientConfig = &tls.Config{
			InsecureSkipVerify: true,
		}
	}
	client := &http.Client{
		Transport: transport,
	}
	if debug {
		client.Transport = utils.NewDebugRoundTripper(client.Transport)
	}

	return &Client{
		client:   client,
		host:     host,
		scheme:   scheme,
		user:     user,
		basePath: "/managed",
	}
}

func (c *Client) do(method string, urlPath string, body interface{}, res interface{}) error {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reqBody = bytes.NewReader(b)
	}

	u := url.URL{
		Scheme: c.scheme,
		User:   c.user,
		Host:   c.host,
		Path:   path.Join(c.basePath, urlPath),
	}
	req, err := http.NewRequest(method, u.String(), reqBody)
	if err != nil {
		return err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	b, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("%d (%s)", resp.StatusCode, b)
	}

	if resp.StatusCode >= 400 {
		var e Error
		if err = json.Unmarshal(b, &e); err != nil {
			return fmt.Errorf("%d: %s (%s)", resp.StatusCode, e.Error(), b)
		}
		return e
	}

	if res == nil {
		return nil
	}
	return json.Unmarshal(b, res)
}

func (c *Client) ScrapeConfigsList() (*APIScrapeConfigsListResponse, error) {
	res := new(APIScrapeConfigsListResponse)
	if err := c.do("GET", "/v0/scrape-configs", nil, res); err != nil {
		return nil, err
	}
	return res, nil
}

func (c *Client) ScrapeConfigsCreate(req *APIScrapeConfigsCreateRequest) error {
	return c.do("POST", "/v0/scrape-configs", req, nil)
}

func (c *Client) ScrapeConfigsDelete(jobName string) error {
	u := url.URL{Path: path.Join("/v0/scrape-configs", jobName)}
	return c.do("DELETE", u.String(), nil, nil)
}

func (c *Client) ScrapeConfigsAddStaticTargets() error {
	return nil
}

func (c *Client) ScrapeConfigsRemoveStaticTargets() error {
	return nil
}
