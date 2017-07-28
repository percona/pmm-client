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

package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/percona/pmm/proto"
	pc "github.com/percona/pmm/proto/config"
	"github.com/percona/qan-agent/agent/release"
	"github.com/percona/qan-agent/pct"
	pctCmd "github.com/percona/qan-agent/pct/cmd"
)

const (
	DEFAULT_KEEPALIVE = 76
	DEFAULT_PIDFILE   = "percona-agent.pid"
	DEFAULT_LISTEN    = "127.0.0.1:9000"
)

const (
	CMD_QUEUE_SIZE              = 10
	STATUS_QUEUE_SIZE           = 10
	MAX_ERRORS                  = 3
	MIN_SUPPORTED_MYSQL_VERSION = "5.1.0"
)

var (
	ErrRestart = errors.New("received Restart command")
	ErrStop    = errors.New("received Stop command")
)

type Agent struct {
	config    *pc.Agent
	configMux *sync.RWMutex
	configDir string
	logger    *pct.Logger
	client    pct.WebsocketClient
	services  map[string]pct.ServiceManager
	addr      string
	keepalive *time.Ticker
	// --
	cmdSync        *pct.SyncChan
	cmdChan        chan *proto.Cmd
	cmdHandlerSync *pct.SyncChan
	//
	statusSync        *pct.SyncChan
	status            *pct.Status
	statusChan        chan *proto.Cmd
	statusHandlerSync *pct.SyncChan
}

func NewAgent(config *pc.Agent, logger *pct.Logger, client pct.WebsocketClient, addr string, services map[string]pct.ServiceManager) *Agent {
	agent := &Agent{
		config:    config,
		configMux: &sync.RWMutex{},
		logger:    logger,
		client:    client,
		addr:      addr,
		services:  services,
		// --
		status:     pct.NewStatus([]string{"agent", "agent-cmd-handler"}),
		cmdChan:    make(chan *proto.Cmd, CMD_QUEUE_SIZE),
		statusChan: make(chan *proto.Cmd, STATUS_QUEUE_SIZE),
	}
	return agent
}

/////////////////////////////////////////////////////////////////////////////
// Interface
/////////////////////////////////////////////////////////////////////////////

func (agent *Agent) Run() error {
	logger := agent.logger
	logger.Debug("Run:call")
	defer logger.Debug("Run:return")

	// Start client goroutines for sending/receving cmd/reply via channels
	// so we can do non-blocking send/recv.  This only needs to be done once.
	// The chans are buffered, so they work for awhile if not connected.
	client := agent.client
	client.Start()
	cmdChan := client.RecvChan()
	connected := false
	go agent.connect()

	/*
	 * Start the status and cmd handlers.  Most messages must be serialized because,
	 * for example, handling start-service and stop-service at the same
	 * time would cause weird problems.  The cmdChan serializes messages,
	 * so it's "first come, first serve" (i.e. fifo).  Concurrency has
	 * consequences: e.g. if user1 sends a start-service and it succeeds
	 * and user2 send the same start-service, user2 will get a ServiceIsRunningError.
	 * Status requests are handled concurrently so the user can always see what
	 * the agent is doing even if it's busy processing commands.
	 */
	agent.cmdHandlerSync = pct.NewSyncChan()
	go agent.cmdHandler()

	agent.statusHandlerSync = pct.NewSyncChan()
	go agent.statusHandler()

	// Allow those ^ goroutines to crash up to MAX_ERRORS.  Any more and it's
	// probably a code bug rather than  bad input, network error, etc.
	cmdHandlerErrors := 0
	statusHandlerErrors := 0

	// Send Pong to API to keep cmd ws open or detect if API end is closed.
	// https://jira.percona.com/browse/PCT-765
	agent.keepalive = time.NewTicker(time.Duration(agent.config.Keepalive) * time.Second)

	logger.Info("Started version " + release.VERSION)

	for {
		logger.Debug("idle")
		agent.status.Update("agent", "Idle")

		select {
		case cmd := <-cmdChan: // from API
			if cmd.Cmd == "Abort" {
				panic(cmd)
			}
			switch cmd.Cmd {
			case "Restart":
				logger.Debug("cmd:restart")
				agent.status.UpdateRe("agent", "Restarting", cmd)

				// Secure the start-lock file.  This lets us start our self but
				// wait until this process has exited, at which time the start-lock
				// is removed and the 2nd self continues starting.
				if err := pct.MakeStartLock(); err != nil {
					agent.reply(cmd.Reply(nil, err))
					continue
				}

				// Start our self with the same args this process was started with.
				cwd, err := os.Getwd()
				if err != nil {
					agent.reply(cmd.Reply(nil, err))
				}
				comment := fmt.Sprintf(
					"This script was created by percona-agent in response to this Restart command:\n"+
						"# %s\n"+
						"# It is safe to delete.", cmd)
				sh := fmt.Sprintf("#!/bin/sh\n# %s\ncd %s\n%s %s >> %s/percona-agent.log 2>&1 &\n",
					comment,
					cwd,
					os.Args[0],
					strings.Join(os.Args[1:len(os.Args)], " "),
					pct.Basedir.Path(),
				)
				startScript := pct.Basedir.File("start-script")
				if err := ioutil.WriteFile(startScript, []byte(sh), os.FileMode(0754)); err != nil {
					agent.reply(cmd.Reply(nil, err))
				}
				logger.Debug("Restart:sh")
				self := pctCmd.Factory.Make(startScript)
				output, err := self.Run()
				agent.reply(cmd.Reply(output, err))
				logger.Debug("Restart:done")
				return ErrRestart
			case "Stop":
				logger.Debug("cmd:stop")
				logger.Info("Stopping", cmd)
				agent.status.UpdateRe("agent", "Stopping", cmd)
				agent.stop()
				agent.reply(cmd.Reply(nil))
				logger.Info("Stopped", cmd)
				agent.status.UpdateRe("agent", "Stopped", cmd)
				return ErrStop
			case "Status":
				logger.Debug("cmd:status")
				agent.status.UpdateRe("agent", "Queueing", cmd)
				select {
				case agent.statusChan <- cmd: // to statusHandler
				default:
					err := pct.QueueFullError{Cmd: cmd.Cmd, Name: "statusQueue", Size: STATUS_QUEUE_SIZE}
					agent.reply(cmd.Reply(nil, err))
				}
			default:
				logger.Debug("cmd")
				agent.status.UpdateRe("agent", "Queueing", cmd)
				select {
				case agent.cmdChan <- cmd: // to cmdHandler
				default:
					err := pct.QueueFullError{Cmd: cmd.Cmd, Name: "cmdQueue", Size: CMD_QUEUE_SIZE}
					agent.reply(cmd.Reply(nil, err))
				}
			}
		case <-agent.cmdHandlerSync.CrashChan:
			cmdHandlerErrors++
			if cmdHandlerErrors < MAX_ERRORS {
				logger.Error("cmdHandler crashed, restarting")
				go agent.cmdHandler()
			} else {
				logger.Fatal("Too many cmdHandler errors")
				// todo: return or exit?
			}
		case <-agent.statusHandlerSync.CrashChan:
			statusHandlerErrors++
			if statusHandlerErrors < MAX_ERRORS {
				logger.Error("statusHandler crashed, restarting")
				go agent.statusHandler()
			} else {
				logger.Fatal("Too many statusHandler errors")
				// todo: return or exit?
			}
		case err := <-client.ErrorChan():
			logger.Warn("ws error:", err)
		case connected = <-client.ConnectChan():
			if connected {
				logger.Info("Connected to API")
				cmdHandlerErrors = 0
				statusHandlerErrors = 0
			} else {
				// websocket closed/crashed/err
				logger.Warn("Lost connection to API")
				go agent.connect()
			}
		case <-agent.keepalive.C:
			// Send keepalive (i.e. check if ws cmd chan is still open on API end).
			logger.Debug("pong")
			if connected {
				cmd := &proto.Cmd{Cmd: "Pong"}
				agent.reply(cmd.Reply(nil, nil))
			}
		}
	}
}

func (agent *Agent) connect() {
	defer func() {
		if err := recover(); err != nil {
			agent.logger.Error("Agent websocket client crashed: ", err)
		}
	}()
	agent.logger.Info("Connecting to API")
	agent.client.Connect()
}

func (agent *Agent) stop() {
	cmd := &proto.Cmd{Ts: time.Now().UTC(), User: "agent"}
	agent.logger.Info("Stopping cmdHandler")
	agent.status.UpdateRe("agent", "Stopping cmdHandler", cmd)
	agent.cmdHandlerSync.Stop()
	agent.cmdHandlerSync.Wait()

	for service, manager := range agent.services {
		if service == "log" {
			continue
		}
		agent.logger.Info("Stopping " + service)
		agent.status.UpdateRe("agent", "Stopping "+service, cmd)
		if err := manager.Stop(); err != nil {
			agent.logger.Warn(err)
		}
	}

	agent.logger.Info("Stopping statusHandler")
	agent.status.UpdateRe("agent", "Stopping statusHandler", cmd)
	agent.statusHandlerSync.Stop()
	agent.statusHandlerSync.Wait()
}

func LoadConfig() ([]byte, error) {
	config := &pc.Agent{}
	_, err := pct.Basedir.ReadConfig("agent", config)
	if err != nil {
		return nil, err
	}

	if config.ApiHostname == "" {
		return nil, errors.New("ApiHostname is not set")
	}
	if config.UUID == "" {
		return nil, errors.New("UUID is not set")
	}
	if config.Keepalive == 0 {
		config.Keepalive = DEFAULT_KEEPALIVE
	}
	data, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func (agent *Agent) GetConfig() ([]proto.AgentConfig, []error) {
	agent.logger.Debug("GetConfig:call")
	defer agent.logger.Debug("GetConfig:return")

	agent.configMux.RLock()
	defer agent.configMux.RUnlock()

	// Copy config so we can clear the Links which are internal only,
	// not really part of the agent config.  Then convert to JSON string.
	config := *agent.config
	config.Links = nil
	bytes, err := json.Marshal(config)
	if err != nil {
		return nil, []error{err}
	}

	// todo: refactor to fix this hack
	set, _ := pct.Basedir.ReadConfig("agent", config)

	// Configs are always returned as array of AgentConfig resources.
	agentConfig := proto.AgentConfig{
		Service: "agent",
		Set:     set,
		Running: string(bytes),
	}
	return []proto.AgentConfig{agentConfig}, []error{}
}

// --------------------------------------------------------------------------
// Command handler
// --------------------------------------------------------------------------

func (agent *Agent) cmdHandler() {
	cmdReply := make(chan *proto.Reply, 1)

	defer func() {
		if err := recover(); err != nil {
			agent.logger.Error("Agent command handler crashed: ", err)
		}
		agent.status.Update("agent-cmd-handler", "Stopped")
		agent.cmdHandlerSync.Done()
	}()

	for {
		agent.status.Update("agent-cmd-handler", "Idle")

		select {
		case cmd := <-agent.cmdChan:
			agent.status.UpdateRe("agent-cmd-handler", "Handling", cmd)
			agent.logger.Info("Cmd begin:", cmd)

			// Handle the cmd in a separate goroutine so if it gets stuck it won't affect us.
			go func() {
				var reply *proto.Reply
				defer func() {
					if err := recover(); err != nil {
						agent.logger.Error("Cmd crash:", cmd, err)
						reply = cmd.Reply(nil, fmt.Errorf("%s", err)) // err is type interface{}, not error
					}
					cmdReply <- reply
				}()
				if cmd.Service == "agent" {
					reply = agent.Handle(cmd)
				} else {
					if manager, ok := agent.services[cmd.Service]; ok {
						reply = manager.Handle(cmd)
					} else {
						reply = cmd.Reply(nil, pct.UnknownServiceError{Service: cmd.Service})
					}
				}
			}()

			// Wait for the cmd to complete.
			var timeout <-chan time.Time
			if cmd.Cmd == "Update" {
				timeout = time.After(5 * time.Minute)
			} else {
				timeout = time.After(20 * time.Second)
			}
			var reply *proto.Reply
			select {
			case reply = <-cmdReply:
				// todo: instrument cmd exec time
			case <-timeout:
				reply = cmd.Reply(nil, pct.CmdTimeoutError{Cmd: cmd.Cmd})
			}

			if reply.Error == "" {
				agent.logger.Info("Cmd ok:", reply)
			} else {
				agent.logger.Warn("Cmd fail:", reply)
			}

			// Reply to cmd.
			if reply != nil {
				agent.reply(reply)
			} else {
				agent.logger.Info(cmd, "executed, no reply")
			}
		case <-agent.cmdHandlerSync.StopChan: // from stop()
			agent.cmdHandlerSync.Graceful()
			return
		}
	}
}

func (agent *Agent) reply(reply *proto.Reply) {
	select {
	case agent.client.SendChan() <- reply:
		// SendChan is buffered so this should be very quick.
		// On error, client closes connection and sends false
		// to ConnectChan which is polled in main Run() loop.
	case <-time.After(20 * time.Second):
		agent.logger.Warn("Failed to send reply:", reply)
	}
}

func (agent *Agent) Handle(cmd *proto.Cmd) *proto.Reply {
	agent.status.UpdateRe("agent-cmd-handler", "Handling", cmd)

	var data interface{}
	var err error
	var errs []error
	switch cmd.Cmd {
	case "StartService":
		data, err = agent.handleStartService(cmd)
	case "StopService":
		data, err = agent.handleStopService(cmd)
	case "GetConfig":
		data, errs = agent.handleGetConfig(cmd)
	case "GetAllConfigs":
		data, errs = agent.GetAllConfigs()
	case "SetConfig":
		data, errs = agent.handleSetConfig(cmd)
	case "GetDefaults":
		data, errs = agent.GetDefaults()
	case "Version":
		data, errs = agent.handleVersion(cmd)
	case "Reconnect":
		/*
			Reconnect is a special case: there's no reply because we can't
			recv cmd on connection 1 and send reply on connection 2.  The
			"reply" in a sense is making a successful connection again.
			If that doesn't happen, then user/API knows reconnect failed.

			Do NOT call connect() here because Disconnect() causes Run() to receive
			false on client.ConnectChan() which causes it to call connect().
		*/
		agent.client.Disconnect()
		return nil // no reply
	default:
		errs = append(errs, pct.UnknownCmdError{Cmd: cmd.Cmd})
	}

	if err != nil {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		for _, err := range errs {
			if err != nil {
				agent.logger.Error(err)
			}
		}
	}

	return cmd.Reply(data, errs...)
}

func (agent *Agent) handleStartService(cmd *proto.Cmd) (interface{}, error) {
	agent.status.UpdateRe("agent-cmd-handler", "StartService", cmd)
	agent.logger.Info(cmd)

	// Unmarshal the data to get the service name and config.
	s := &proto.ServiceData{}
	if err := json.Unmarshal(cmd.Data, s); err != nil {
		return nil, err
	}

	// Check if we have a manager for the service.
	m, ok := agent.services[s.Name]
	if !ok {
		return nil, pct.UnknownServiceError{Service: s.Name}
	}

	// Start the service.
	if err := m.Start(); err != nil {
		return nil, err
	}

	return nil, nil
}

func (agent *Agent) handleStopService(cmd *proto.Cmd) (interface{}, error) {
	agent.status.UpdateRe("agent-cmd-handler", "StopService", cmd)
	agent.logger.Info(cmd)

	// Unmarshal the data to get the service name.
	s := new(proto.ServiceData)
	if err := json.Unmarshal(cmd.Data, s); err != nil {
		return nil, err
	}

	// Check if we have a manager for the service.  If not, that's ok,
	// just return because the service can't be running if we don't have it.
	m, ok := agent.services[s.Name]
	if !ok {
		return nil, pct.UnknownServiceError{Service: s.Name}
	}

	// Stop the service.
	err := m.Stop()
	return nil, err
}

func (agent *Agent) handleGetConfig(cmd *proto.Cmd) (interface{}, []error) {
	agent.status.UpdateRe("agent-cmd-handler", "GetConfig", cmd)
	agent.logger.Info(cmd)
	return agent.GetConfig()
}

func (agent *Agent) GetAllConfigs() (interface{}, []error) {
	configs, errs := agent.GetConfig()
	for service, manager := range agent.services {
		if manager == nil { // should not happen
			agent.logger.Error("Nil manager:", service)
			continue
		}
		config, err := manager.GetConfig()
		if err != nil && len(err) > 0 {
			errs = append(errs, err...)
			continue
		}
		if config != nil {
			// Not all services have a config.
			configs = append(configs, config...)
		}
	}
	return configs, errs
}

func (agent *Agent) GetDefaults() (interface{}, []error) {
	defaults := map[string]map[string]interface{}{
		"bin": map[string]interface{}{
			"pid-file": DEFAULT_PIDFILE,
			"basedir":  pct.DEFAULT_BASEDIR,
		},
		"agent": map[string]interface{}{
			"KeepAlive": DEFAULT_KEEPALIVE,
			"Listen":    DEFAULT_LISTEN,
		},
	}
	for service, manager := range agent.services {
		if manager == nil { // should not happen
			agent.logger.Error("Nil manager:", service)
			continue
		}
		def := manager.GetDefaults()
		if def == nil {
			continue
		}
		defaults[service] = make(map[string]interface{})
		for k, v := range def {
			defaults[service][k] = v
		}
	}
	return defaults, nil
}

func (agent *Agent) handleSetConfig(cmd *proto.Cmd) (interface{}, []error) {
	agent.status.UpdateRe("agent-cmd-handler", "SetConfig", cmd)
	agent.logger.Info(cmd)

	newConfig := &pc.Agent{}
	if err := json.Unmarshal(cmd.Data, newConfig); err != nil {
		return nil, []error{err}
	}

	agent.configMux.RLock()
	finalConfig := *agent.config // copy current config
	agent.configMux.RUnlock()

	errs := []error{}

	// Change keepalive if valid. It is not dynamic.
	if newConfig.Keepalive > 0 {
		agent.logger.Warn("Changing keepalive from", finalConfig.Keepalive, "to", newConfig.Keepalive,
			"; restart agent to take effect")
		finalConfig.Keepalive = newConfig.Keepalive
	}

	// Write the new, updated config.  If this fails, agent will use old config if restarted.
	if err := pct.Basedir.WriteConfig("agent", finalConfig); err != nil {
		errs = append(errs, errors.New("agent.WriteConfig:"+err.Error()))
	}

	// Lock agent config and re-point the pointer.
	agent.configMux.Lock()
	defer agent.configMux.Unlock()
	agent.config = &finalConfig

	return &finalConfig, errs
}

func (agent *Agent) handleVersion(cmd *proto.Cmd) (interface{}, []error) {
	v := &proto.Version{
		Running: release.VERSION,
	}
	bin, err := filepath.Abs(os.Args[0])
	if err != nil {
		return v, []error{err}
	}
	out, err := exec.Command(bin, "-version").Output()
	if err != nil {
		return v, []error{err}
	}
	v.Installed = strings.TrimSpace(string(out))
	return v, nil
}

//---------------------------------------------------------------------------
// Status handler
// --------------------------------------------------------------------------

func (agent *Agent) statusHandler() {
	replyChan := agent.client.SendChan()
	defer func() {
		if err := recover(); err != nil {
			agent.logger.Error("Agent status handler crashed: ", err)
		}
		agent.statusHandlerSync.Done()
	}()

	// Status handler doesn't have its own status because that's circular,
	// e.g. "How am I? I'm good!".

	for {
		select {
		case cmd := <-agent.statusChan:
			switch cmd.Service {
			case "":
				replyChan <- cmd.Reply(agent.AllStatus())
			case "agent":
				replyChan <- cmd.Reply(agent.Status())
			default:
				if manager, ok := agent.services[cmd.Service]; ok {
					replyChan <- cmd.Reply(manager.Status())
				} else {
					replyChan <- cmd.Reply(nil, pct.UnknownServiceError{Service: cmd.Service})
				}
			}
		case <-agent.statusHandlerSync.StopChan:
			agent.statusHandlerSync.Graceful()
			return
		}
	}
}

func (agent *Agent) Status() map[string]string {
	return agent.status.Merge(agent.client.Status())
}

func (agent *Agent) AllStatus() map[string]string {
	status := agent.Status()
	for service, manager := range agent.services {
		if manager == nil { // should not happen
			status[service] = fmt.Sprintf("ERROR: %s service manager is nil", service)
			continue
		}
		for k, v := range manager.Status() {
			status[k] = v
		}
	}
	return status
}
