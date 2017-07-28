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
)

type NullClient struct {
	conn        *websocket.Conn
	connectChan chan bool
	errChan     chan error
}

func NewNullClient() *NullClient {
	c := &NullClient{
		conn:        new(websocket.Conn),
		connectChan: make(chan bool),
		errChan:     make(chan error),
	}
	return c
}

func (c *NullClient) Connect() {
}

func (c *NullClient) ConnectOnce() error {
	return nil
}

func (c *NullClient) Disconnect() error {
	return nil
}

func (c *NullClient) Start() {
}

func (c *NullClient) Stop() {
}

func (c *NullClient) SendChan() chan *proto.Reply {
	return nil
}

func (c *NullClient) RecvChan() chan *proto.Cmd {
	return nil
}

func (c *NullClient) SendBytes(data []byte, timeout uint) error {
	return nil
}

func (c *NullClient) Send(data interface{}, timeout uint) error {
	return nil
}

func (c *NullClient) Recv(data interface{}, timeout uint) error {
	return nil
}

func (c *NullClient) ErrorChan() chan error {
	return c.errChan
}

func (c *NullClient) ConnectChan() chan bool {
	return c.connectChan
}

func (c *NullClient) Conn() *websocket.Conn {
	return c.conn
}
