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

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"time"

	"github.com/julienschmidt/httprouter"
	"github.com/percona/platform/proto"
)

var (
	AGENT_API_PORT string = "9000"
	QAN_API_PORT   string = "9001"
	PROM_API_PORT  string = "9003"
)

var (
	ErrNotFound = errors.New("resource not found")
	ErrNoServer = errors.New("PMM server address not set")
)

type API struct {
	addr string
}

func NewAPI(addr string) *API {
	a := &API{
		addr: addr,
	}
	return a
}

func (a *API) Run() {
	router := httprouter.New()
	router.GET("/", get)
	router.POST("/", post)
	router.DELETE("/:name/:port", del)

	log.Fatal(http.ListenAndServe(a.addr, router))
}

func get(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	JSONResponse(w, 200, exporters)
}

func post(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		ErrorResponse(w, err)
		return
	}
	if len(body) == 0 {
		JSONResponse(w, http.StatusBadRequest, nil)
		return
	}
	e := &Exporter{}
	if err := json.Unmarshal(body, e); err != nil {
		ErrorResponse(w, err)
		return
	}

	if err := add(e); err != nil {
		ErrorResponse(w, err)
	} else {
		JSONResponse(w, http.StatusOK, nil)
	}
	log.Printf("Added %+v", e)
}

func del(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
	name := p.ByName("name")
	port := p.ByName("port")
	if err := remove(name, port); err != nil {
		ErrorResponse(w, err)
	} else {
		JSONResponse(w, http.StatusOK, nil)
	}
}

func JSONResponse(w http.ResponseWriter, statusCode int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	w.WriteHeader(statusCode)
	if v != nil {
		if err := json.NewEncoder(w).Encode(v); err != nil {
			log.Println(err)
		}
	}
}

func ErrorResponse(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	w.WriteHeader(500)
	e := proto.Error{
		Error: err.Error(),
	}
	if err := json.NewEncoder(w).Encode(e); err != nil {
		log.Println(err)
	}
}

func GetInstance(uuid string) (proto.Instance, error) {
	var in proto.Instance

	serverAddr := serverAddr()
	if serverAddr == "" {
		return in, ErrNoServer
	}
	url := fmt.Sprintf("http://%s:%s/instances/%s", serverAddr, QAN_API_PORT, uuid)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return in, err
	}
	//if a.headers != nil {
	//	for k, v := range a.headers {
	//		req.Header.Add(k, v)
	//	}
	//}

	client := &http.Client{Timeout: time.Duration(1 * time.Second)}
	resp, err := client.Do(req)
	if err != nil {
		return in, err
	}
	defer resp.Body.Close()

	bytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return in, fmt.Errorf("GET %s: ioutil.ReadAll: %s", url, err)
	}
	if len(bytes) > 0 {
		if err := json.Unmarshal(bytes, &in); err != nil {
			return in, err
		}
	}

	if resp.StatusCode != http.StatusOK {
		switch resp.StatusCode {
		case http.StatusNotFound:
			return in, ErrNotFound
		default:
			return in, fmt.Errorf("got status code %d, expected 200", resp.StatusCode)
		}
	}

	return in, nil
}
