/*
   Copyright (c) 2014-2015, Percona LLC and/or its affiliates. All rights reserved.

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
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/hashicorp/consul/api"
	"github.com/percona/pmm/proto"
)

func (f *FakeApi) AppendRoot() {
	f.Append("/", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "GET":
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(600)
		}
	})
}

func (f *FakeApi) AppendPing() {
	f.Append("/ping", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "GET":
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(600)
		}
	})
}

func (f *FakeApi) AppendQanAPIInstancesId(id uint, protoInstance *proto.Instance) {
	f.Append(fmt.Sprintf("/qan-api/instances/%d", id), func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		data, _ := json.Marshal(&protoInstance)
		w.Write(data)
	})
}

func (f *FakeApi) AppendConsulV1StatusLeader(xRemoteIP string) {
	f.Append("/v1/status/leader", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("X-Remote-IP", xRemoteIP)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("\"127.0.0.1:8300\""))
	})
}

func (f *FakeApi) AppendConsulV1CatalogNode() {
	f.Append("/v1/catalog/node/", func(w http.ResponseWriter, r *http.Request) {
		out := api.CatalogNode{
			Node: &api.Node{},
		}
		data, _ := json.Marshal(out)
		w.WriteHeader(http.StatusOK)
		w.Write(data)
	})
}

func (f *FakeApi) AppendConsulV1CatalogService() {
	f.Append("/v1/catalog/service/", func(w http.ResponseWriter, r *http.Request) {
		out := []api.CatalogService{}
		data, _ := json.Marshal(out)
		w.WriteHeader(http.StatusOK)
		w.Write(data)
	})
}

func (f *FakeApi) AppendConsulV1CatalogRegister() {
	f.Append("/v1/catalog/register", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "PUT":
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(600)
		}
	})
}

func (f *FakeApi) AppendQanAPIInstances(protoInstances []*proto.Instance) {
	instances := map[string]*proto.Instance{}
	for i := range protoInstances {
		instances[protoInstances[i].Subsystem] = protoInstances[i]
	}
	f.Append("/qan-api/instances/", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "PUT":
			w.WriteHeader(http.StatusNoContent)
		case "GET":
			t := r.URL.Query().Get("type")
			w.WriteHeader(http.StatusOK)
			data, _ := json.Marshal(instances[t])
			w.Write(data)
		default:
			w.WriteHeader(600)
		}

	})
}

func (f *FakeApi) AppendQanAPIAgents(id uint) {
	f.Append(fmt.Sprintf("/qan-api/agents/%d/cmd", id), func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "PUT":
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(600)
		}

	})
}
