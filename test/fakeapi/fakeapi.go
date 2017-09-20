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
)

type FakeApi struct {
	testServer *httptest.Server
	serveMux   *http.ServeMux
	ctx        context.Context
}

func New() *FakeApi {
	fakeApi := &FakeApi{}
	fakeApi.serveMux = http.NewServeMux()
	fakeApi.testServer = httptest.NewServer(fakeApi.serveMux)
	return fakeApi
}

func (f *FakeApi) Close() {
	f.ctx = nil
	f.testServer.Close()
}

// Host of the fake api
func (f *FakeApi) Host() string {
	u, _ := url.Parse(f.URL())
	host, _, _ := net.SplitHostPort(u.Host)
	return host
}

// Port of the fake api
func (f *FakeApi) Port() string {
	u, _ := url.Parse(f.URL())
	_, port, _ := net.SplitHostPort(u.Host)
	return port
}

// URL for the fake api
func (f *FakeApi) URL() string {
	return f.testServer.URL
}

func (f *FakeApi) Append(pattern string, handler func(http.ResponseWriter, *http.Request)) {
	f.serveMux.HandleFunc(pattern, handler)
}
