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

package client_test

import (
	"log"
	"net"
	"testing"
	"time"

	"github.com/percona/pmm/proto"
	"github.com/percona/qan-agent/client"
	"github.com/percona/qan-agent/pct"
	"github.com/percona/qan-agent/test"
	"github.com/percona/qan-agent/test/mock"
	. "gopkg.in/check.v1"
)

// Hook gocheck into the "go test" runner.
func Test(t *testing.T) { TestingT(t) }

type TestSuite struct {
	logChan   chan *proto.LogEntry
	logger    *pct.Logger
	server    *mock.WebsocketServer
	api       *mock.API
	serverWss *mock.WebsocketServerWss
	apiWss    *mock.API
}

var _ = Suite(&TestSuite{})

const (
	ADDR        = "localhost:8000" // make sure this port is free
	URL         = "ws://" + ADDR
	ENDPOINT    = "/"
	WSSADDR     = "localhost:8443" // make sure this port is free
	WSSENDPOINT = "/wss/"
	WSSURL      = "wss://" + WSSADDR + WSSENDPOINT
)

func (s *TestSuite) SetUpSuite(t *C) {
	s.logChan = make(chan *proto.LogEntry, 10)
	s.logger = pct.NewLogger(s.logChan, "ws")

	mock.SendChan = make(chan interface{}, 5)
	mock.RecvChan = make(chan interface{}, 5)
	s.server = new(mock.WebsocketServer)
	go s.server.Run(ADDR, ENDPOINT)

	time.Sleep(1 * time.Second)

	links := map[string]string{"agent": URL}
	s.api = mock.NewAPI("http://localhost", ADDR, "uuid", links)

	mock.SendChanWss = make(chan interface{}, 5)
	mock.RecvChanWss = make(chan interface{}, 5)
	s.serverWss = new(mock.WebsocketServerWss)
	go s.serverWss.RunWss(WSSADDR, WSSENDPOINT)

	time.Sleep(1 * time.Second)

	linksWss := map[string]string{"agent": WSSURL}
	s.apiWss = mock.NewAPI("http://localhost", WSSADDR, "uuid", linksWss)
}

func (s *TestSuite) TearDownTest(t *C) {
	// Disconnect all clients.
	for _, c := range mock.Clients {
		mock.DisconnectClient(c)
	}
	for _, c := range mock.ClientsWss {
		mock.DisconnectClientWss(c)
	}
}

// --------------------------------------------------------------------------

func (s *TestSuite) TestSend(t *C) {
	// LogRelay (logrelay/) uses "direct" interface, not send/recv chans.

	ws, err := client.NewWebsocketClient(s.logger, s.api, "agent", nil)
	t.Assert(err, IsNil)

	// Client sends state of connection (true=connected, false=disconnected)
	// on its ConnectChan.
	connected := false
	doneChan := make(chan bool)
	go func() {
		connected = <-ws.ConnectChan()
		doneChan <- true
	}()

	// Wait for connection in mock ws server.
	ws.Connect()
	c := <-mock.ClientConnectChan

	<-doneChan
	t.Check(connected, Equals, true)

	// Send a log entry.
	logEntry := &proto.LogEntry{
		Level:   2,
		Service: "qan",
		Msg:     "Hello",
	}
	err = ws.Send(logEntry, 5)
	t.Assert(err, IsNil)

	// Recv what we just sent.
	got := test.WaitData(c.RecvChan)
	t.Assert(len(got), Equals, 1)

	// We're dealing with generic data.
	m := got[0].(map[string]interface{})
	t.Check(m["Level"], Equals, float64(2))
	t.Check(m["Service"], Equals, "qan")
	t.Check(m["Msg"], Equals, "Hello")

	// Quick check that Conn() works.
	conn := ws.Conn()
	t.Check(conn, NotNil)

	// Status should report connected to the proper link.
	status := ws.Status()
	t.Check(status, DeepEquals, map[string]string{
		"ws":      "Connected " + URL,
		"ws-link": URL,
	})

	ws.Disconnect()

	select {
	case connected = <-ws.ConnectChan():
	case <-time.After(1 * time.Second):
		t.Error("No connected=false notify on Disconnect()")
	}

	// Status should report disconnected and still the proper link.
	status = ws.Status()
	t.Check(status, DeepEquals, map[string]string{
		"ws":      "Disconnected",
		"ws-link": URL,
	})
}

func (s *TestSuite) TestChannels(t *C) {
	// Agent uses send/recv channels instead of "direct" interface.

	ws, err := client.NewWebsocketClient(s.logger, s.api, "agent", nil)
	t.Assert(err, IsNil)

	// Start send/recv chans, but idle until successful Connect.
	ws.Start()
	defer ws.Stop()

	ws.Connect()
	c := <-mock.ClientConnectChan
	<-ws.ConnectChan()

	// API sends Cmd to client.
	cmd := &proto.Cmd{
		User: "daniel",
		Ts:   time.Now(),
		Cmd:  "Status",
	}
	c.SendChan <- cmd

	// If client's recvChan is working, it will receive the Cmd.
	got := test.WaitCmd(ws.RecvChan())
	t.Assert(len(got), Equals, 1)
	t.Assert(got[0], DeepEquals, *cmd)

	// Client sends Reply in response to Cmd.
	reply := cmd.Reply(nil, nil)
	ws.SendChan() <- reply

	// If client's sendChan is working, we/API will receive the Reply.
	data := test.WaitData(c.RecvChan)
	t.Assert(len(data), Equals, 1)

	// We're dealing with generic data again.
	m := data[0].(map[string]interface{})
	t.Assert(m["Cmd"], Equals, "Status")
	t.Assert(m["Error"], Equals, "")

	ws.Disconnect()
}

func (s *TestSuite) TestApiDisconnect(t *C) {
	// If using direct interface, Recv() should return error if API disconnects.

	ws, err := client.NewWebsocketClient(s.logger, s.api, "agent", nil)
	t.Assert(err, IsNil)

	ws.Connect()
	c := <-mock.ClientConnectChan
	<-ws.ConnectChan()

	// No error yet.
	got := test.WaitErr(ws.ErrorChan())
	t.Assert(len(got), Equals, 0)

	mock.DisconnectClient(c)

	/**
	 * I cannot provoke an error on websocket.Send(), only Receive().
	 * Perhaps errors (e.g. ws closed) are only reported on recv?
	 * This only affect the logger since it's ws send-only: it will
	 * need a goroutine blocking on Recieve() that, upon error, notifies
	 * the sending goroutine to reconnect.
	 */
	var data interface{}
	err = ws.Recv(data, 5)
	t.Assert(err, NotNil) // EOF due to disconnect.
}

func (s *TestSuite) TestChannelsApiDisconnect(t *C) {
	// If using chnanel interface, ErrorChan() should return error if API disconnects.

	ws, err := client.NewWebsocketClient(s.logger, s.api, "agent", nil)
	t.Assert(err, IsNil)

	var gotErr error
	doneChan := make(chan bool)
	go func() {
		gotErr = <-ws.ErrorChan()
		doneChan <- true
	}()

	ws.Start()
	defer ws.Stop()
	defer ws.Disconnect()

	ws.Connect()
	c := <-mock.ClientConnectChan
	<-ws.ConnectChan() // connect ack

	// No error yet.
	select {
	case <-doneChan:
		t.Error("No error yet")
	default:
	}

	mock.DisconnectClient(c)

	// Wait for error.
	select {
	case <-doneChan:
		t.Check(gotErr, NotNil) // EOF due to disconnect.
	case <-time.After(1 * time.Second):
		t.Error("Get error")
	}
}

func (s *TestSuite) TestErrorChan(t *C) {
	// When client disconnects due to send or recv error,
	// it should send the error on its ErrorChan().

	ws, err := client.NewWebsocketClient(s.logger, s.api, "agent", nil)
	t.Assert(err, IsNil)

	ws.Start()
	defer ws.Stop()

	ws.Connect()
	c := <-mock.ClientConnectChan
	<-ws.ConnectChan()

	// No error yet.
	got := test.WaitErr(ws.ErrorChan())
	t.Assert(len(got), Equals, 0)

	// API sends Cmd to client.
	cmd := &proto.Cmd{
		User: "daniel",
		Ts:   time.Now(),
		Cmd:  "Status",
	}
	c.SendChan <- cmd

	// No error yet.
	got = test.WaitErr(ws.ErrorChan())
	t.Assert(len(got), Equals, 0)

	// Disconnect the client.
	mock.DisconnectClient(c)

	// Client should send error from disconnect.
	got = test.WaitErr(ws.ErrorChan())
	t.Assert(len(got), Equals, 1)
	t.Assert(got[0], NotNil)

	ws.Disconnect()
}

func (s *TestSuite) TestConnectBackoff(t *C) {
	// Connect() should wait between attempts, using pct.Backoff (pct/backoff.go).

	ws, err := client.NewWebsocketClient(s.logger, s.api, "agent", nil)
	t.Assert(err, IsNil)

	ws.Connect()
	c := <-mock.ClientConnectChan
	<-ws.ConnectChan()
	defer ws.Disconnect()

	// 0s wait, connect, err="Lost connection",
	// 1s wait, connect, err="Lost connection",
	// 3s wait, connect, ok
	t0 := time.Now()
	for i := 0; i < 2; i++ {
		mock.DisconnectClient(c)
		ws.Connect()
		c = <-mock.ClientConnectChan
		<-ws.ConnectChan() // connect ack
	}
	d := time.Now().Sub(t0)
	if d < time.Duration(3*time.Second) {
		t.Errorf("Exponential backoff wait time between connect attempts: %s\n", d)
	}
}

func (s *TestSuite) TestChannelsAfterReconnect(t *C) {
	/**
	 * Client send/recv chans should work after disconnect and reconnect.
	 */

	ws, err := client.NewWebsocketClient(s.logger, s.api, "agent", nil)
	t.Assert(err, IsNil)

	ws.Start()
	defer ws.Stop()
	defer ws.Disconnect()

	ws.Connect()
	c := <-mock.ClientConnectChan
	<-ws.ConnectChan() // connect ack

	// Send cmd and wait for reply to ensure we're fully connected.
	cmd := &proto.Cmd{
		User: "daniel",
		Ts:   time.Now(),
		Cmd:  "Status",
	}
	c.SendChan <- cmd
	got := test.WaitCmd(ws.RecvChan())
	t.Assert(len(got), Equals, 1)
	reply := cmd.Reply(nil, nil)
	ws.SendChan() <- reply
	data := test.WaitData(c.RecvChan)
	t.Assert(len(data), Equals, 1)

	// Disconnect client.
	mock.DisconnectClient(c)
	<-ws.ConnectChan() // disconnect ack

	// Reconnect client and send/recv again.
	ws.Connect()
	c = <-mock.ClientConnectChan
	<-ws.ConnectChan() // connect ack

	c.SendChan <- cmd
	got = test.WaitCmd(ws.RecvChan())
	t.Assert(len(got), Equals, 1)
	reply = cmd.Reply(nil, nil)
	ws.SendChan() <- reply
	data = test.WaitData(c.RecvChan)
	t.Assert(len(data), Equals, 1)
}

func (s *TestSuite) TestDialTimeout(t *C) {
	/**
	 * This test simulates a dial timeout by listening on a port that does nothing.
	 * The TCP connection completes, but the TLS handshake times out because the
	 * little goroutine below does nothing after net.Listen() (normally code would
	 * net.Accept() after listening).  To simulate a lower-level dial timeout would
	 * require a very low-level handling of the network socket: having the port open
	 * but not completing the TCP syn-syn+ack-ack handshake; this is too complicate,
	 * so breaking the TLS handshake is close enough.
	 */
	addr := "localhost:9443"
	url := "wss://" + addr + "/"
	links := map[string]string{"agent": url}
	api := mock.NewAPI("http://localhost", url, "uuid", links)
	wss, err := client.NewWebsocketClient(s.logger, api, "agent", nil)
	t.Assert(err, IsNil)

	doneChan := make(chan bool, 1)
	go func() {
		l, err := net.Listen("tcp", addr)
		if err != nil {
			log.Fatal(err)
		}
		defer l.Close()
		<-doneChan
	}()
	time.Sleep(1 * time.Second)

	err = wss.ConnectOnce(2)
	t.Check(err, NotNil)

	doneChan <- true
}

func (s *TestSuite) TestWssConnection(t *C) {
	/**
	 * This test ensures our slighly customized wss connectio handling works,
	 * i.e. that TLS works.  Only drawback is: client disables cert verification
	 * because the mock ws server uses a self-signed cert, but this only happens
	 * when the remote addr is localhost:8443, so it shouldn't affect real connections.
	 */
	ws, err := client.NewWebsocketClient(s.logger, s.apiWss, "agent", nil)
	t.Assert(err, IsNil)

	// Client sends state of connection (true=connected, false=disconnected)
	// on its ConnectChan.
	connected := false
	doneChan := make(chan bool)
	go func() {
		connected = <-ws.ConnectChan()
		doneChan <- true
	}()

	// Wait for connection in mock ws server.
	ws.Connect()
	c := <-mock.ClientConnectChanWss

	<-doneChan
	t.Check(connected, Equals, true)

	// Send a log entry.
	logEntry := &proto.LogEntry{
		Level:   2,
		Service: "qan",
		Msg:     "Hello",
	}
	err = ws.Send(logEntry, 5)
	t.Assert(err, IsNil)

	// Recv what we just sent.
	got := test.WaitData(c.RecvChanWss)
	t.Assert(len(got), Equals, 1)

	ws.Conn().Close()
}

func (s *TestSuite) TestSendBytes(t *C) {
	ws, err := client.NewWebsocketClient(s.logger, s.api, "agent", nil)
	t.Assert(err, IsNil)

	ws.ConnectOnce(5)
	c := <-mock.ClientConnectChan

	data := []byte(`["Hello"]`)
	err = ws.SendBytes(data, 5)
	t.Assert(err, IsNil)

	// Recv what we just sent.
	got := test.WaitData(c.RecvChan)
	t.Assert(len(got), Equals, 1)
	gotData := got[0].([]interface{})
	t.Check(gotData[0].(string), Equals, "Hello")

	ws.DisconnectOnce()
}

func (s *TestSuite) TestCloseTimeout(t *C) {
	// https://jira.percona.com/browse/PCT-1045

	ws, err := client.NewWebsocketClient(s.logger, s.api, "agent", nil)
	t.Assert(err, IsNil)

	connected := false
	doneChan := make(chan bool)
	go func() {
		connected = <-ws.ConnectChan()
		doneChan <- true
	}()

	// Wait for connection in mock ws server.
	ws.Connect()
	c := <-mock.ClientConnectChan

	<-doneChan
	t.Check(connected, Equals, true)

	// Send a log entry.
	logEntry := &proto.LogEntry{
		Level:   2,
		Service: "qan",
		Msg:     "Hello",
	}
	err = ws.Send(logEntry, 1)
	t.Assert(err, IsNil)

	// Recv what we just sent.
	got := test.WaitData(c.RecvChan)
	t.Assert(len(got), Equals, 1)

	// Wait 1s for that ^ 1s timeout to pass.
	time.Sleep(1400 * time.Millisecond)

	err = ws.Disconnect()
	t.Check(err, IsNil)
}
