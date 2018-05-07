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

package fakeapi

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
)

type FakeApi struct {
	testServer          *httptest.Server
	serveMux            *http.ServeMux
	ctx                 context.Context
	baseURL, host, port string
	sync.RWMutex
}

func New() *FakeApi {
	fakeApi := &FakeApi{}
	fakeApi.serveMux = http.NewServeMux()
	return fakeApi
}

// Start new FakeApi server and return it's URL, host and port
func (f *FakeApi) Start() (string, string, string) {
	f.Lock()
	defer f.Unlock()
	f.testServer = httptest.NewServer(f.serveMux)
	f.baseURL = f.testServer.URL
	u, _ := url.Parse(f.baseURL)
	f.host, f.port, _ = net.SplitHostPort(u.Host)
	return f.baseURL, f.host, f.port
}

// Close shutdowns FakeApi server.
func (f *FakeApi) Close() {
	f.ctx = nil
	if f.testServer != nil {
		f.testServer.Close()
		f.testServer = nil
	}
}

func (f *FakeApi) Append(pattern string, handler func(http.ResponseWriter, *http.Request)) {
	f.serveMux.HandleFunc(pattern, handler)
}
