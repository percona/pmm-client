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
	"golang.org/x/net/websocket"
	"log"
	"net/http"
)

type WebsocketServer struct {
}

var SendChan chan interface{}
var RecvChan chan interface{}

// addr: http://127.0.0.1:8000
// endpoint: /agent
func (s *WebsocketServer) Run(addr string, endpoint string) {
	go run()
	http.Handle(endpoint, websocket.Handler(wsHandler))
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatal("ListenAndServe:", err)
	}
}

type client struct {
	ws       *websocket.Conn
	origin   string
	SendChan chan interface{} // data to client
	RecvChan chan interface{} // data from client
}

func wsHandler(ws *websocket.Conn) {
	c := &client{
		ws:       ws,
		origin:   ws.Config().Origin.String(),
		SendChan: make(chan interface{}, 5),
		RecvChan: make(chan interface{}, 5),
	}
	internalClientConnectChan <- c

	defer func() {
		ClientRmChan <- c
		ClientDisconnectChan <- c
	}()
	go c.send()
	c.recv()
}

func (c *client) recv() {
	defer c.ws.Close()
	for {
		var data interface{}
		err := websocket.JSON.Receive(c.ws, &data)
		if err != nil {
			//log.Printf("ERROR: recv: %s\n", err)
			break
		}
		//log.Printf("recv: %+v\n", data)
		c.RecvChan <- data
	}
}

func (c *client) send() {
	defer c.ws.Close()
	for data := range c.SendChan {
		// log.Printf("recv: %+v\n", data)
		err := websocket.JSON.Send(c.ws, data)
		if err != nil {
			break
		}
	}
}

var internalClientConnectChan = make(chan *client)
var ClientConnectChan = make(chan *client, 1)
var ClientDisconnectChan = make(chan *client)
var ClientRmChan = make(chan *client, 5)
var Clients = make(map[*client]*client)

func DisconnectClient(c *client) {
	c, ok := Clients[c]
	if ok {
		close(c.SendChan)
		c.ws.Close()
		//log.Printf("disconnect: %+v\n", c)
		<-ClientDisconnectChan
	}
}

func run() {
	for {
		select {
		case c := <-internalClientConnectChan:
			// todo: this is probably prone to deadlocks, not thread-safe
			Clients[c] = c
			// log.Printf("connect: %+v\n", c)
			select {
			case ClientConnectChan <- c:
			default:
			}
		case c := <-ClientRmChan:
			if _, ok := Clients[c]; ok {
				//log.Printf("remove : %+v\n", c)
				delete(Clients, c)
			}
		}
	}
}
