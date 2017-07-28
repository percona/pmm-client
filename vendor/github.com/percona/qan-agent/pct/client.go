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

package pct

import (
	"github.com/percona/pmm/proto"
	"golang.org/x/net/websocket"
)

type WebsocketClient interface {
	Conn() *websocket.Conn
	Status() map[string]string

	// Cmd/Reply chans:
	Start()                      // start the send/recv chans
	Stop()                       // stop the send/recv chans manually
	RecvChan() chan *proto.Cmd   // get the (recv) cmdChan
	SendChan() chan *proto.Reply // get the (send) replyChan

	// Async connect:
	Connect()               // try forever to connect, notify via ConnectChan
	Disconnect() error      // disconnect manually, notify via ConnectChan
	ConnectChan() chan bool // true on connect, false on disconnect
	ErrorChan() chan error  // get err from send/recv chans

	// Sync connect:
	ConnectOnce(timeout uint) error
	DisconnectOnce() error

	// Data transfer:
	SendBytes(data []byte, timeout uint) error // send data (data/sender)
	Recv(data interface{}, timeout uint) error // recv proto.Response (data/sender)
	Send(data interface{}, timeout uint) error // send proto.LogEntry (log/relay)
}
