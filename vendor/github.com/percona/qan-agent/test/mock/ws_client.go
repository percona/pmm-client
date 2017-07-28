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
	"github.com/percona/pmm/proto"
	"golang.org/x/net/websocket"
	"sync"
)

type WebsocketClient struct {
	testSendChan     chan *proto.Cmd
	userRecvChan     chan *proto.Cmd
	userSendChan     chan *proto.Reply
	testRecvChan     chan *proto.Reply
	testSendDataChan chan interface{}
	testRecvDataChan chan interface{}
	conn             *websocket.Conn
	ErrChan          chan error
	SendError        chan error
	RecvError        chan error
	ConnectError     error
	connectChan      chan bool
	testConnectChan  chan bool
	connected        bool
	started          bool
	RecvBytes        chan []byte
	TraceChan        chan string
	mux              *sync.Mutex
	mux2             *sync.Mutex
}

func NewWebsocketClient(sendChan chan *proto.Cmd, recvChan chan *proto.Reply, sendDataChan chan interface{}, recvDataChan chan interface{}) *WebsocketClient {
	c := &WebsocketClient{
		testSendChan:     sendChan,
		userRecvChan:     make(chan *proto.Cmd, 10),
		userSendChan:     make(chan *proto.Reply, 10),
		testRecvChan:     recvChan,
		testSendDataChan: sendDataChan,
		testRecvDataChan: recvDataChan,
		conn:             new(websocket.Conn),
		SendError:        make(chan error, 1),
		RecvError:        make(chan error),
		connectChan:      make(chan bool, 1),
		RecvBytes:        make(chan []byte, 1),
		TraceChan:        make(chan string, 100),
		mux:              &sync.Mutex{},
		mux2:             &sync.Mutex{},
	}
	return c
}

func (c *WebsocketClient) Connect() {
	c.TraceChan <- "Connect"
	c.mux2.Lock()
	unlocked := false
	if c.testConnectChan != nil {
		c.mux2.Unlock()
		unlocked = true
		// Wait for test to let user/agent connect.
		c.testConnectChan <- true
		<-c.testConnectChan
	}
	if !unlocked {
		c.mux2.Unlock()
	}
	c.connectChan <- true // to SUT
	c.mux.Lock()
	defer c.mux.Unlock()
	c.connected = true
}

func (c *WebsocketClient) ConnectOnce(timeout uint) error {
	c.TraceChan <- "ConnectOnce"
	c.mux.Lock()
	defer c.mux.Unlock()
	c.connected = true
	return c.ConnectError
}

func (c *WebsocketClient) Disconnect() error {
	c.TraceChan <- "Disconnect"
	c.connectChan <- false // to SUT
	c.mux.Lock()
	defer c.mux.Unlock()
	c.connected = false
	return nil
}

func (c *WebsocketClient) DisconnectOnce() error {
	c.TraceChan <- "DisconnectOnce"
	c.mux.Lock()
	defer c.mux.Unlock()
	c.connected = false
	return nil
}

func (c *WebsocketClient) Start() {
	c.TraceChan <- "Start"
	if c.started {
		return
	}

	go func() {
		for cmd := range c.testSendChan { // test sends cmd
			c.userRecvChan <- cmd // user receives cmd
		}
	}()

	go func() {
		for reply := range c.userSendChan { // user sends reply
			c.testRecvChan <- reply // test receives reply
		}
	}()

	c.started = true
}

func (c *WebsocketClient) Stop() {
}

func (c *WebsocketClient) SendChan() chan *proto.Reply {
	return c.userSendChan
}

func (c *WebsocketClient) RecvChan() chan *proto.Cmd {
	return c.userRecvChan
}

func (c *WebsocketClient) Send(data interface{}, timeout uint) error {
	c.TraceChan <- "Send"
	select {
	case err := <-c.SendError:
		return err
	default:
	}
	// Relay data from user to test.
	c.testRecvDataChan <- data
	return nil
}

func (c *WebsocketClient) SendBytes(data []byte, timeout uint) error {
	c.RecvBytes <- data
	return nil
}

func (c *WebsocketClient) Recv(data interface{}, timeout uint) error {
	// Relay data from test to user.
	select {
	case data = <-c.testSendDataChan:
	case err := <-c.RecvError:
		return err
	}
	return nil
}

func (c *WebsocketClient) ConnectChan() chan bool {
	return c.connectChan
}

func (c *WebsocketClient) ErrorChan() chan error {
	return c.ErrChan
}

func (c *WebsocketClient) Conn() *websocket.Conn {
	return c.conn
}

func (c *WebsocketClient) SetConnectChan(connectChan chan bool) {
	c.mux2.Lock()
	defer c.mux2.Unlock()
	c.testConnectChan = connectChan
}

func (c *WebsocketClient) Status() map[string]string {
	c.mux.Lock()
	defer c.mux.Unlock()
	wsStatus := ""
	if c.connected {
		wsStatus = "Connected"
	} else {
		wsStatus = "Disconnected"
	}
	return map[string]string{
		"ws":      wsStatus,
		"ws-link": "http://localhost",
	}
}
