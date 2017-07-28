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
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/percona/pmm/proto"
)

type InstanceStatus struct {
	instance     *proto.Instance
	status       int
	maxInstances uint
}

func NewInstanceStatus(inst *proto.Instance, status int, maxInstances uint) *InstanceStatus {
	return &InstanceStatus{
		instance:     inst,
		status:       status,
		maxInstances: maxInstances,
	}
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

func swapHTTPScheme(url, newScheme string) string {
	splittedUrl := strings.Split(url, "://")
	if len(splittedUrl) != 2 {
		return url
	}
	return newScheme + splittedUrl[1]
}

// Appends handler for /intances.
// If maxAgents != 0 this method will return HTTP Forbidden and X-Percona-Agent-Limit header in case of an agent
// instance POST request
func (f *FakeApi) AppendInstances(treeInst *proto.Instance, postInsts []*InstanceStatus) {
	// POST /instances will be sent more than once, handlers for URL can only be registered once, hence the need of
	// a queue
	f.Append("/instances", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "GET":
			if treeInst == nil {
				panic(errors.New("Tried to GET /instances but handler had no data to serve"))
			}
			w.WriteHeader(http.StatusOK)
			data, _ := json.Marshal(&treeInst)
			w.Write(data)
		case "POST":
			body, err := ioutil.ReadAll(r.Body)
			if err != nil {
				panic(err)
			}
			var inst *proto.Instance
			err = json.Unmarshal(body, &inst)
			if err != nil {
				panic(err)
			}

			if len(postInsts) == 0 {
				panic(errors.New("Tried to POST /instances but handler doesn't have queued instances to return a valid Location"))
			}

			// Dequeue one instance
			instStatus := postInsts[0]
			postInsts = postInsts[1:]

			newInst := instStatus.instance

			if instStatus.maxInstances != 0 {
				w.Header().Set("X-Percona-Limit-Err", fmt.Sprintf("%d", instStatus.maxInstances))
				w.WriteHeader(instStatus.status)
				return
			}
			w.Header().Set("Location", fmt.Sprintf("%s/instances/%s", f.URL(), newInst.UUID))
			w.WriteHeader(instStatus.status)
		}
	})
}

func (f *FakeApi) AppendInstancesUUID(inst *proto.Instance) {
	f.Append(fmt.Sprintf("/instances/%s", inst.UUID), func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "GET":
			w.WriteHeader(http.StatusOK)
			data, _ := json.Marshal(inst)
			w.Write(data)
		case "PUT":
			body, err := ioutil.ReadAll(r.Body)
			if err != nil {
				panic(err)
			}
			var newInst *proto.Instance
			err = json.Unmarshal(body, &newInst)
			if err != nil {
				panic(err)
			}
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(600)
		}
	})
}

func (f *FakeApi) AppendConfigsQanDefault() {
	f.Append("/configs/qan/default", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{ "UUID": "0", "Interval": 60}`))
	})
}
