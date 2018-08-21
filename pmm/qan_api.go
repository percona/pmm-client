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
	"bytes"
	"compress/gzip"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/percona/pmm-client/pmm/utils"
)

type API struct {
	headers     map[string]string
	hostname    string
	insecureSSL bool
	apiTimeout  time.Duration
	debug       bool // turns on logging requests with std logger
}

type apiError struct {
	Error string
}

func NewAPI(insecureFlag bool, timeout time.Duration, debug bool) *API {
	hostname, _ := os.Hostname()
	a := &API{
		headers:     nil,
		hostname:    hostname,
		insecureSSL: insecureFlag,
		apiTimeout:  timeout,
		debug:       debug,
	}
	return a
}

func (a *API) Hostname() string {
	return a.hostname
}

func (a *API) Ping(url string) error {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	if a.headers != nil {
		for k, v := range a.headers {
			req.Header.Add(k, v)
		}
	}

	client := a.NewClient()
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	_, err = ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return err
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("got status code %d, expected 200", resp.StatusCode)
	}
	return nil // success
}

func (a *API) URL(paths ...string) string {
	return strings.Join(paths, "/")
}

func (a *API) Get(url string) (*http.Response, []byte, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, nil, err
	}
	if a.headers != nil {
		for k, v := range a.headers {
			req.Header.Add(k, v)
		}
	}

	client := a.NewClient()
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	var data []byte
	if resp.Header.Get("Content-Type") == "application/x-gzip" {
		buf := new(bytes.Buffer)
		gz, err := gzip.NewReader(resp.Body)
		if err != nil {
			return nil, nil, fmt.Errorf("GET %s: gzip.NewReader: %s", url, err)
		}
		if _, err := io.Copy(buf, gz); err != nil {
			return resp, nil, fmt.Errorf("GET %s: io.Copy: %s", url, err)
		}
		data = buf.Bytes()
	} else {
		data, err = ioutil.ReadAll(resp.Body)
		if err != nil {
			return resp, nil, fmt.Errorf("GET %s: ioutil.ReadAll: %s", url, err)
		}
	}

	return resp, data, nil
}

func (a *API) Post(url string, data []byte) (*http.Response, []byte, error) {
	return a.send("POST", url, data)
}

func (a *API) Put(url string, data []byte) (*http.Response, []byte, error) {
	return a.send("PUT", url, data)
}

func (a *API) Delete(url string) (*http.Response, []byte, error) {
	return a.send("DELETE", url, nil)
}

func (a *API) Error(method, url string, gotStatusCode, expectedStatusCode int, content []byte) error {
	errMsg := fmt.Sprintf("%s %s: API returned HTTP status code %d, expected %d",
		method, url, gotStatusCode, expectedStatusCode)
	if len(content) > 0 {
		var apiErr apiError
		if err := json.Unmarshal(content, &apiErr); err != nil {
			errMsg += ": " + string(content)
		} else {
			errMsg += ": " + apiErr.Error
		}
	}
	return fmt.Errorf(errMsg)
}

// NewClient creates new *http.Client tailored for this API
func (a *API) NewClient() *http.Client {
	transport := &http.Transport{}
	if a.insecureSSL {
		transport.TLSClientConfig = &tls.Config{
			InsecureSkipVerify: true,
		}
	}
	client := &http.Client{
		Timeout:   a.apiTimeout,
		Transport: transport,
	}
	if a.debug {
		// if api is in debug mode we should log every request and response
		client.Transport = utils.NewVerboseRoundTripper(client.Transport)
	}
	return client
}

// --------------------------------------------------------------------------

func (a *API) send(method, url string, data []byte) (*http.Response, []byte, error) {
	var req *http.Request
	var err error
	var body io.Reader

	if data != nil {
		body = bytes.NewReader(data)
	}

	req, err = http.NewRequest(method, url, body)
	if err != nil {
		return nil, nil, err
	}
	if a.headers != nil {
		for k, v := range a.headers {
			req.Header.Add(k, v)
		}
	}

	client := a.NewClient()
	resp, err := client.Do(req)
	if err != nil {
		return resp, nil, err
	}

	content, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return resp, nil, err
	}

	return resp, content, nil
}
