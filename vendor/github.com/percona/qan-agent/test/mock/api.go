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

package mock

import (
	"net/http"
)

type APIResponse struct {
	Code  int
	Data  []byte
	Error error
}

type API struct {
	origin    string
	hostname  string
	agentUuid string
	links     map[string]string
	GetCode   []int
	GetData   [][]byte
	GetError  []error
	GetResp   []APIResponse
	PutResp   []APIResponse
}

func NewAPI(origin, hostname, agentUuid string, links map[string]string) *API {
	a := &API{
		origin:    origin,
		hostname:  hostname,
		agentUuid: agentUuid,
		links:     links,
		PutResp:   []APIResponse{},
	}
	return a
}

func PingAPI(hostname string) (bool, *http.Response) {
	return true, nil
}

func (a *API) Init(hostname string, headers map[string]string) (code int, err error) {
	return http.StatusOK, nil
}

func (a *API) Connect(hostname, agentUuid string) error {
	a.hostname = hostname
	a.agentUuid = agentUuid
	return nil
}

func (a *API) AgentLink(resource string) string {
	return a.links[resource]
}

func (a *API) EntryLink(resource string) string {
	return a.links[resource]
}

func (a *API) Origin() string {
	return a.origin
}

func (a *API) Hostname() string {
	return a.hostname
}

func (a *API) AgentUuid() string {
	return a.agentUuid
}

func (a *API) Get(url string) (int, []byte, error) {
	n := len(a.GetResp)
	if n > 0 {
		var resp APIResponse
		resp, a.GetResp = a.GetResp[0], a.GetResp[1:]
		return resp.Code, resp.Data, resp.Error
	}

	code := 200
	var data []byte
	var err error
	if len(a.GetCode) > 0 {
		code = a.GetCode[0]
		a.GetCode = a.GetCode[1:len(a.GetCode)]
	}
	if len(a.GetData) > 0 {
		data = a.GetData[0]
		a.GetData = a.GetData[1:len(a.GetData)]
	}
	if len(a.GetError) > 0 {
		err = a.GetError[0]
		a.GetError = a.GetError[1:len(a.GetError)]
	}
	return code, data, err
}

func (a *API) Post(url string, data []byte) (*http.Response, []byte, error) {
	return nil, nil, nil
}

func (a *API) Put(url string, data []byte) (*http.Response, []byte, error) {
	n := len(a.PutResp)
	if n > 0 {
		var resp APIResponse
		resp, a.PutResp = a.PutResp[0], a.PutResp[1:]
		return &http.Response{StatusCode: resp.Code}, resp.Data, resp.Error
	}
	return nil, nil, nil
}

func (a *API) CreateInstance(url string, it interface{}) (bool, error) {
	return true, nil
}

func (a *API) URL(paths ...string) string {
	return ""
}
