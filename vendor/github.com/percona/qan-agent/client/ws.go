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

package client

import (
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/percona/pmm/proto"
	"github.com/percona/qan-agent/pct"
	"golang.org/x/net/websocket"
)

const (
	SEND_BUFFER_SIZE = 10
	RECV_BUFFER_SIZE = 10
)

var (
	ErrNoLink = errors.New("no link to API resource")
)

type WebsocketClient struct {
	logger  *pct.Logger
	api     pct.APIConnector
	link    string
	headers map[string]string
	// --
	conn      *websocket.Conn
	connected bool
	mux       *sync.Mutex // guard conn and connected
	// --
	started     bool
	recvChan    chan *proto.Cmd
	sendChan    chan *proto.Reply
	connectChan chan bool
	errChan     chan error
	backoff     *pct.Backoff
	sendSync    *pct.SyncChan
	recvSync    *pct.SyncChan
	status      *pct.Status
	name        string
}

func NewWebsocketClient(logger *pct.Logger, api pct.APIConnector, link string, headers map[string]string) (*WebsocketClient, error) {
	name := logger.Service()
	c := &WebsocketClient{
		logger:  logger,
		api:     api,
		link:    link,
		headers: headers,
		// --
		mux:  new(sync.Mutex),
		conn: nil,
		// --
		recvChan:    make(chan *proto.Cmd, RECV_BUFFER_SIZE),
		sendChan:    make(chan *proto.Reply, SEND_BUFFER_SIZE),
		connectChan: make(chan bool, 1),
		errChan:     make(chan error, 2),
		backoff:     pct.NewBackoff(60, 5*time.Minute),
		sendSync:    pct.NewSyncChan(),
		recvSync:    pct.NewSyncChan(),
		status:      pct.NewStatus([]string{name, name + "-link"}),
		name:        name,
	}
	return c, nil
}

func (c *WebsocketClient) Start() {
	// Start send() and recv() goroutines, but they wait for successful Connect().
	if !c.started {
		c.started = true
		go c.send()
		go c.recv()
	}
}

func (c *WebsocketClient) Stop() {
	if c.started {
		c.sendSync.Stop()
		c.recvSync.Stop()
		c.sendSync.Wait()
		c.recvSync.Wait()
		c.started = false
	}
}

func (c *WebsocketClient) Connect() {
	c.logger.Debug("Connect:call")
	defer c.logger.Debug("Connect:return")

	for {
		// Wait before attempt to avoid DDoS'ing the API
		// (there are many other agents in the world).
		c.logger.Debug("Connect:backoff.Wait")
		c.status.Update(c.name, "Connect wait")
		time.Sleep(c.backoff.Wait())

		if err := c.ConnectOnce(10); err != nil {
			if err != ErrNoLink {
				c.logger.Warn(err) // no API connection yet
			}
			continue
		}
		c.backoff.Success()

		// Start/resume send() and recv() goroutines if Start() was called.
		if c.started {
			c.recvSync.Start()
			c.sendSync.Start()
		}

		c.notifyConnect(true)
		return // success
	}
}

func (c *WebsocketClient) ConnectOnce(timeout uint) error {
	c.logger.Debug("ConnectOnce:call")
	defer c.logger.Debug("ConnectOnce:return")

	c.mux.Lock()
	defer c.mux.Unlock()

	// Make websocket connection.  If this fails, either API is down or the ws
	// address is wrong.
	link := c.api.AgentLink(c.link)
	if link == "" {
		return ErrNoLink
	}
	c.logger.Debug("ConnectOnce:link:" + link)
	config, err := websocket.NewConfig(link, c.api.Origin())
	if err != nil {
		return err
	}

	if c.headers != nil {
		for k, v := range c.headers {
			config.Header.Add(k, v)
		}
	}

	c.status.Update(c.name, "Connecting "+link)
	conn, err := c.dialTimeout(config, timeout)
	if err != nil {
		return err
	}

	c.conn = conn
	c.connected = true
	c.status.Update(c.name, "Connected "+link)

	return nil
}

func (c *WebsocketClient) dialTimeout(config *websocket.Config, timeout uint) (ws *websocket.Conn, err error) {
	c.logger.Debug("ConnectOnce:websocket.DialConfig:call")
	defer c.logger.Debug("ConnectOnce:websocket.DialConfig:return")

	// websocket.Dial() does not handle timeouts, so we use lower-level net package
	// to create connection with timeout, then create ws client with the net connection.

	if config.Location == nil {
		return nil, websocket.ErrBadWebSocketLocation
	}
	if config.Origin == nil {
		return nil, websocket.ErrBadWebSocketOrigin
	}

	var conn net.Conn
	switch config.Location.Scheme {
	case "ws":
		conn, err = net.DialTimeout("tcp", config.Location.Host, time.Duration(timeout)*time.Second)
	case "wss":
		dialer := &net.Dialer{
			Timeout: time.Duration(timeout) * time.Second,
		}
		if config.Location.Host == "localhost:8443" {
			// Test uses mock ws server which uses self-signed cert which causes Go to throw
			// an error like "x509: certificate signed by unknown authority".  This disables
			// the cert verification for testing.
			config.TlsConfig = &tls.Config{
				InsecureSkipVerify: true,
			}
		}
		conn, err = tls.DialWithDialer(dialer, "tcp", config.Location.Host, config.TlsConfig)
	default:
		err = websocket.ErrBadScheme
	}
	if err != nil {
		return nil, &websocket.DialError{Config: config, Err: err}
	}

	ws, err = websocket.NewClient(config, conn)
	if err != nil {
		return nil, err
	}

	return ws, nil
}

func (c *WebsocketClient) Disconnect() error {
	c.logger.DebugOffline("Disconnect:call")
	defer c.logger.DebugOffline("Disconnect:return")

	c.mux.Lock()
	defer c.mux.Unlock()
	if !c.connected {
		return nil
	}

	err := c.disconnect()
	c.notifyConnect(false)
	return err
}

func (c *WebsocketClient) DisconnectOnce() error {
	c.logger.DebugOffline("DisconnectOnce:call")
	defer c.logger.DebugOffline("DisconnectOnce:return")

	/**
	 * Must guard c.conn here to prevent duplicate notifyConnect() because Close()
	 * causes recv() to error which calls Disconnect(), and normally we want this:
	 * to call Disconnect() on recv error so that notifyConnect(false) is called
	 * to let user know that remote end hung up.  However, when user hangs up
	 * the Disconnect() call from recv() is duplicate and not needed.
	 */
	c.mux.Lock()
	defer c.mux.Unlock()
	if !c.connected {
		return nil
	}

	return c.disconnect()
}

func (c *WebsocketClient) disconnect() error {
	c.logger.DebugOffline("disconnect:call")
	defer c.logger.DebugOffline("disconnect:return")

	// Close() causes a write, therefore it's affected by the write timeout.
	// Since Send() also sets the write timeout, we must reset it here else
	// Close() can fail immediately due to previous timeout set for Send()
	// already having passed.
	// https://jira.percona.com/browse/PCT-1045
	c.conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
	defer c.conn.SetWriteDeadline(time.Time{})

	var err error
	if err = c.conn.Close(); err != nil {
		// Example: write tcp 127.0.0.1:8000: i/o timeout
		// That ^ can happen if remote end hangs up, then we call Close(),
		// or if there's a timeout (shouldn't happen afaik).
		// Since there's nothing we can do about errors here, we ignore them.
		c.logger.DebugOffline("disconnect:websocket.Conn.Close:err:" + err.Error())
	}

	/**
	 * Do not set c.conn = nil to indicate that connection is closed because
	 * unless we also guard c.conn in Send() and Recv() c.conn.Set*Deadline()
	 * will panic.  If the underlying websocket.Conn is closed, then
	 * Set*Deadline() will do nothing and websocket.JSON.Send/Receive() will
	 * just return an error, which is a lot better than a panic.
	 */
	c.connected = false

	c.logger.DebugOffline("disconnected")
	c.status.Update(c.name, "Disconnected")
	return err
}

func (c *WebsocketClient) send() {
	/**
	 * Send Reply from agent to API.
	 */

	c.logger.DebugOffline("send:call")
	defer c.logger.DebugOffline("send:return")
	defer c.sendSync.Done()
	defer func() {
		// todo: notify caller somehow so it can restart the ws client chans.
		if err := recover(); err != nil {
			log.Printf("ERROR: WebsocketClient.send crashed: %s\n", err)
		}
	}()

	for {
		// Wait to start (connect) or be told to stop.
		c.logger.DebugOffline("send:wait:start")
		select {
		case <-c.sendSync.StartChan:
			c.sendSync.StartChan <- true
		case <-c.sendSync.StopChan:
			return
		}

	SEND_LOOP:
		for {
			c.logger.DebugOffline("send:idle")
			select {
			case reply := <-c.sendChan:
				// Got Reply from agent, send to API.
				c.logger.DebugOffline("send:reply:", reply)
				if err := c.Send(reply, 10); err != nil {
					c.logger.DebugOffline("send:err:", err)
					select {
					case c.errChan <- err:
					default:
					}
					break SEND_LOOP
				}
			case <-c.sendSync.StopChan:
				c.logger.DebugOffline("send:stop")
				return
			}
		}

		c.logger.DebugOffline("send:Disconnect")
		c.Disconnect()
	}
}

func (c *WebsocketClient) recv() {
	/**
	 * Receive Cmd from API, forward to agent.
	 */

	c.logger.DebugOffline("recv:call")
	defer c.logger.DebugOffline("recv:return")
	defer c.recvSync.Done()
	defer func() {
		// todo: notify caller somehow so it can restart the ws client chans.
		if err := recover(); err != nil {
			log.Printf("ERROR: WebsocketClient.recv crashed: %s\n", err)
		}
	}()

	for {
		// Wait to start (connect) or be told to stop.
		c.logger.DebugOffline("recv:wait:start")
		select {
		case <-c.recvSync.StartChan:
			c.recvSync.StartChan <- true
		case <-c.recvSync.StopChan:
			return
		}

	RECV_LOOP:
		for {
			// Before blocking on Recv, see if we're supposed to stop.
			select {
			case <-c.recvSync.StopChan:
				c.logger.DebugOffline("recv:stop")
				return
			default:
			}

			// Wait for Cmd from API.
			cmd := &proto.Cmd{}
			if err := c.Recv(cmd, 0); err != nil {
				c.logger.DebugOffline("recv:err:", err)
				select {
				case c.errChan <- err:
				default:
				}
				break RECV_LOOP
			}

			// Forward Cmd to agent.
			c.logger.DebugOffline("recv:cmd:", cmd)
			c.recvChan <- cmd
		}

		c.logger.DebugOffline("recv:Disconnect")
		c.Disconnect()
	}
}

func (c *WebsocketClient) SendChan() chan *proto.Reply {
	return c.sendChan
}

func (c *WebsocketClient) RecvChan() chan *proto.Cmd {
	return c.recvChan
}

func (c *WebsocketClient) Send(data interface{}, timeout uint) error {
	// These make the debug output a little too verbose:
	// c.logger.DebugOffline("Send:call")
	// defer c.logger.DebugOffline("Send:return")
	if timeout > 0 {
		c.conn.SetWriteDeadline(time.Now().Add(time.Duration(timeout) * time.Second))
		defer c.conn.SetWriteDeadline(time.Time{})
	} else {
		c.conn.SetWriteDeadline(time.Time{})
	}
	return websocket.JSON.Send(c.conn, data)
}

func (c *WebsocketClient) SendBytes(data []byte, timeout uint) error {
	c.logger.DebugOffline("SendBytes:call")
	defer c.logger.DebugOffline("SendBytes:return")
	if timeout > 0 {
		c.conn.SetWriteDeadline(time.Now().Add(time.Duration(timeout) * time.Second))
	} else {
		c.conn.SetWriteDeadline(time.Time{})
	}
	defer c.conn.SetWriteDeadline(time.Time{})
	return websocket.Message.Send(c.conn, data)
}

func (c *WebsocketClient) Recv(data interface{}, timeout uint) error {
	c.logger.DebugOffline("Recv:call")
	defer c.logger.DebugOffline("Recv:return")
	if timeout > 0 {
		c.conn.SetReadDeadline(time.Now().Add(time.Duration(timeout) * time.Second))
		defer c.conn.SetReadDeadline(time.Time{})
	} else {
		c.conn.SetReadDeadline(time.Time{})
	}
	return websocket.JSON.Receive(c.conn, data)
}

func (c *WebsocketClient) ConnectChan() chan bool {
	return c.connectChan
}

func (c *WebsocketClient) ErrorChan() chan error {
	return c.errChan
}

func (c *WebsocketClient) Conn() *websocket.Conn {
	return c.conn
}

func (c *WebsocketClient) Status() map[string]string {
	c.status.Update(c.name+"-link", c.api.AgentLink(c.link))
	return c.status.All()
}

func (c *WebsocketClient) notifyConnect(state bool) {
	c.logger.DebugOffline(fmt.Sprintf("notifyConnect:call:%t", state))
	defer c.logger.DebugOffline("notifyConnect:return")
	select {
	case c.connectChan <- state:
	case <-time.After(20 * time.Second):
		c.logger.Error("notifyConnect timeout")
	}
}
