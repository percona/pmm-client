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
	"os"
	"strings"
)

type WebsocketServerWss struct {
}

var SendChanWss chan interface{}
var RecvChanWss chan interface{}

// addr: https://127.0.0.1:8443
// endpoint: /agent
func (s *WebsocketServerWss) RunWss(addr string, endpoint string) {
	go runWss()
	http.Handle(endpoint, websocket.Handler(wssHandler))
	curDir, _ := os.Getwd()
	curDir = strings.TrimSuffix(curDir, "client")
	if err := http.ListenAndServeTLS(addr, curDir+"test/keys/cert.pem", curDir+"test/keys/key.pem", nil); err != nil {
		log.Fatal("ListenAndServe:", err)
	}
}

type clientWss struct {
	wss         *websocket.Conn
	origin      string
	SendChanWss chan interface{} // data to client
	RecvChanWss chan interface{} // data from client
}

func wssHandler(wss *websocket.Conn) {
	c := &clientWss{
		wss:         wss,
		origin:      wss.Config().Origin.String(),
		SendChanWss: make(chan interface{}, 5),
		RecvChanWss: make(chan interface{}, 5),
	}
	internalClientConnectChanWss <- c

	defer func() {
		ClientRmChanWss <- c
		ClientDisconnectChanWss <- c
	}()
	go c.sendWss()
	c.recvWss()
}

func (c *clientWss) recvWss() {
	defer c.wss.Close()
	for {
		var data interface{}
		err := websocket.JSON.Receive(c.wss, &data)
		if err != nil {
			break
		}
		// log.Printf("recv: %+v\n", data)
		c.RecvChanWss <- data
	}
}

func (c *clientWss) sendWss() {
	defer c.wss.Close()
	for data := range c.SendChanWss {
		// log.Printf("recv: %+v\n", data)
		err := websocket.JSON.Send(c.wss, data)
		if err != nil {
			break
		}
	}
}

var internalClientConnectChanWss = make(chan *clientWss)
var ClientConnectChanWss = make(chan *clientWss, 1)
var ClientDisconnectChanWss = make(chan *clientWss)
var ClientRmChanWss = make(chan *clientWss, 5)
var ClientsWss = make(map[*clientWss]*clientWss)

func DisconnectClientWss(c *clientWss) {
	c, ok := ClientsWss[c]
	if ok {
		close(c.SendChanWss)
		c.wss.Close()
		//log.Printf("disconnect: %+v\n", c)
		<-ClientDisconnectChanWss
	}
}

func runWss() {
	for {
		select {
		case c := <-internalClientConnectChanWss:
			// todo: this is probably prone to deadlocks, not thread-safe
			ClientsWss[c] = c
			// log.Printf("connect: %+v\n", c)
			select {
			case ClientConnectChanWss <- c:
			default:
			}
		case c := <-ClientRmChanWss:
			if _, ok := ClientsWss[c]; ok {
				//log.Printf("remove : %+v\n", c)
				delete(ClientsWss, c)
			}
		}
	}
}
