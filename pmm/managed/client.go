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
	"context"
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

const (
	ErrCanceled           = 1
	ErrUnknown            = 2
	ErrInvalidArgument    = 3
	ErrDeadlineExceeded   = 4
	ErrNotFound           = 5
	ErrAlreadyExists      = 6
	ErrPermissionDenied   = 7
	ErrUnauthenticated    = 16
	ErrResourceExhausted  = 8
	ErrFailedPrecondition = 9
	ErrAborted            = 10
	ErrOutOfRange         = 11
	ErrUnimplemented      = 12
	ErrInternal           = 13
	ErrUnavailable        = 14
	ErrDataLoss           = 15
)

type Error struct {
	Err  string `json:"error"`
	Code int    `json:"code"`
}

func (e *Error) Error() string {
	return e.Err
}

type Client struct {
	client   *http.Client
	host     string
	scheme   string
	user     *url.Userinfo
	basePath string
}

func NewClient(host string, scheme string, user *url.Userinfo, insecureSSL bool, verbose bool) *Client {
	transport := &http.Transport{}
	if insecureSSL {
		transport.TLSClientConfig = &tls.Config{
			InsecureSkipVerify: true,
		}
	}
	client := &http.Client{
		Transport: transport,
	}
	if verbose {
		client.Transport = utils.NewVerboseRoundTripper(client.Transport)
	}

	return &Client{
		client:   client,
		host:     host,
		scheme:   scheme,
		user:     user,
		basePath: "/managed",
	}
}

func (c *Client) do(ctx context.Context, method string, urlPath string, body interface{}, res interface{}) error {
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
	req = req.WithContext(ctx)

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
			// Do not dump HTML from nginx by default, but give user an idea that something is very wrong.
			// They can retry with --verbose to see the gory details.
			return fmt.Errorf("status code %d (%s)", resp.StatusCode, resp.Header.Get("Content-Type"))
		}
		return &e
	}

	if res == nil {
		return nil
	}
	return json.Unmarshal(b, res)
}

func (c *Client) ScrapeConfigsList(ctx context.Context) (*APIScrapeConfigsListResponse, error) {
	res := new(APIScrapeConfigsListResponse)
	if err := c.do(ctx, "GET", "/v0/scrape-configs", nil, res); err != nil {
		return nil, err
	}
	return res, nil
}

func (c *Client) ScrapeConfigsGet(ctx context.Context, jobName string) (*APIScrapeConfigsGetResponse, error) {
	res := new(APIScrapeConfigsGetResponse)
	if err := c.do(ctx, "GET", "/v0/scrape-configs/"+jobName, nil, res); err != nil {
		return nil, err
	}
	return res, nil
}

func (c *Client) ScrapeConfigsCreate(ctx context.Context, req *APIScrapeConfigsCreateRequest) error {
	return c.do(ctx, "POST", "/v0/scrape-configs", req, nil)
}

func (c *Client) ScrapeConfigsUpdate(ctx context.Context, req *APIScrapeConfigsUpdateRequest) error {
	return c.do(ctx, "PUT", "/v0/scrape-configs/"+req.ScrapeConfig.JobName, req, nil)
}

func (c *Client) ScrapeConfigsDelete(ctx context.Context, jobName string) error {
	return c.do(ctx, "DELETE", "/v0/scrape-configs/"+jobName, nil, nil)
}

// AnnotationCreate posts annotation to managed API.
func (c *Client) AnnotationCreate(ctx context.Context, req *APIAnnotationCreateRequest) error {
	return c.do(ctx, "POST", "/v0/annotations", req, nil)
}

// VersionGet returns version of the managed API.
func (c *Client) VersionGet(ctx context.Context) (*VersionResponse, error) {
	res := new(VersionResponse)
	if err := c.do(ctx, "GET", "/v1/version", nil, res); err != nil {
		return nil, err
	}
	return res, nil
}
