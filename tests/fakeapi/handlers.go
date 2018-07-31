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
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"path"
	"time"

	"net"
	"net/url"

	"github.com/hashicorp/consul/agent/structs"
	"github.com/hashicorp/consul/api"
	"github.com/percona/pmm/proto"
)

// AppendRoot adds "/" route to API.
func (f *FakeApi) AppendRoot() {
	f.Append("/", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "GET":
			switch r.URL.Path {
			case "/":
				w.WriteHeader(http.StatusOK)
			default:
				panic(fmt.Sprintf("fakeapi: unknown path %s", r.URL.Path))
			}
		default:
			w.WriteHeader(600)
			panic(fmt.Sprintf("fakeapi: unknown method %s for path %s", r.Method, r.URL.Path))
		}
	})
}

func (f *FakeApi) AppendPrometheusAPIV1Query() {
	f.Append("/prometheus/api/v1/query", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "GET":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[]},"error":"","errorType":""}`))
		default:
			w.WriteHeader(600)
			panic(fmt.Sprintf("fakeapi: unknown method %s for path %s", r.Method, r.URL.Path))
		}
	})
}

// AppendQanAPIPing adds "/qan-api/ping" route to API.
func (f *FakeApi) AppendQanAPIPing() {
	f.Append("/qan-api/ping", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "GET":
			w.Header().Add("Content-Type", "text/plain; charset=utf-8")
			w.Header().Add("X-Percona-Qan-Api-Version", "gotest")
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(600)
			panic(fmt.Sprintf("fakeapi: unknown method %s for path %s", r.Method, r.URL.Path))
		}
	})
}

func (f *FakeApi) AppendQanAPIInstancesId(id string, protoInstance *proto.Instance) {
	f.Append(fmt.Sprintf("/qan-api/instances/%s", id), func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		data, _ := json.Marshal(&protoInstance)
		w.Write(data)
	})
}

// AppendConsulV1StatusLeader adds "/v1/status/leader" route to API.
func (f *FakeApi) AppendConsulV1StatusLeader() {
	f.Append("/v1/status/leader", func(w http.ResponseWriter, r *http.Request) {
		f.RLock()
		defer f.RUnlock()
		u, _ := url.Parse(f.baseURL)
		xRemoteIP, _, _ := net.SplitHostPort(u.Host)
		w.Header().Add("X-Remote-IP", xRemoteIP)
		w.Header().Add("X-Server-Time", fmt.Sprintf("%d", time.Now().Unix()))
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("\"127.0.0.1:8300\""))
	})
}

// AppendConsulV1CatalogNode adds "/v1/catalog/node/<name>" route to API.
func (f *FakeApi) AppendConsulV1CatalogNode(name string, node api.CatalogNode) {
	f.Append("/v1/catalog/node/"+name, func(w http.ResponseWriter, r *http.Request) {
		data, _ := json.Marshal(node)
		w.WriteHeader(http.StatusOK)
		w.Write(data)
	})
}

func (f *FakeApi) AppendConsulV1CatalogService() {
	f.Append("/v1/catalog/service/", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "GET":
			out := []api.CatalogService{}
			rrs := f.ctx.Value("/v1/catalog/register").(map[string]structs.RegisterRequest)
			p := path.Base(r.URL.Path)
			rrsToAdd := map[string]structs.RegisterRequest{}
			if p == "register" {
				rrsToAdd = rrs
			} else if rr, ok := rrs[p]; ok {
				rrsToAdd = map[string]structs.RegisterRequest{
					p: rr,
				}
			}
			for _, rr := range rrsToAdd {
				cs := api.CatalogService{}
				cs.ServiceID = rr.Service.ID
				cs.ServiceName = rr.Service.Service
				cs.ServiceAddress = rr.Service.Address
				cs.ServiceTags = rr.Service.Tags
				cs.ServicePort = rr.Service.Port
				cs.ServiceEnableTagOverride = rr.Service.EnableTagOverride
				out = append(out, cs)
			}
			data, _ := json.Marshal(out)
			w.WriteHeader(http.StatusOK)
			w.Write(data)
		default:
			w.WriteHeader(600)
			panic(fmt.Sprintf("fakeapi: unknown method %s for path %s", r.Method, r.URL.Path))
		}
	})
}

func (f *FakeApi) AppendConsulV1CatalogRegister() {
	v := map[string]structs.RegisterRequest{}
	f.ctx = context.WithValue(f.ctx, "/v1/catalog/register", v)
	f.Append("/v1/catalog/register", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "PUT":
			body, err := ioutil.ReadAll(r.Body)
			if err != nil {
				panic(fmt.Sprintf("error reading body: %s", err))
			}
			defer r.Body.Close()

			rr := structs.RegisterRequest{}
			err = json.Unmarshal(body, &rr)
			if err != nil {
				panic(fmt.Sprintf("error unmarshaling body: %s", err))
			}
			v[rr.Service.Service] = rr

			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(600)
			panic(fmt.Sprintf("fakeapi: unknown method %s for path %s", r.Method, r.URL.Path))
		}
	})
}

func (f *FakeApi) AppendConsulV1KV() {
	f.Append("/v1/kv/", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "GET":
			out := api.KVPairs{}
			data, _ := json.Marshal(out)
			w.WriteHeader(http.StatusOK)
			w.Write(data)
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
			panic(fmt.Sprintf("fakeapi: unknown method %s for path %s", r.Method, r.URL.Path))
		}

	})
}

func (f *FakeApi) AppendQanAPIAgents(id string) {
	f.Append(fmt.Sprintf("/qan-api/agents/%s/cmd", id), func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "PUT":
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(600)
			panic(fmt.Sprintf("fakeapi: unknown method %s for path %s", r.Method, r.URL.Path))
		}
	})
}

func (f *FakeApi) AppendManaged() {
	f.Append("/managed/v0/scrape-configs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("{}"))
	})
}

// AddAnnotation allows test annotation endpoint.
func (f *FakeApi) AddAnnotation() {
	f.Append("/managed/v0/annotations", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "POST":
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
}
