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
	"flag"
	"fmt"
	golog "log"
	"os"
	"os/signal"
	"os/user"
	"runtime"
	"syscall"
	"time"

	"github.com/percona/pmm/proto"
	pc "github.com/percona/pmm/proto/config"
	"github.com/percona/qan-agent/agent"
	"github.com/percona/qan-agent/agent/release"
	"github.com/percona/qan-agent/client"
	"github.com/percona/qan-agent/data"
	"github.com/percona/qan-agent/instance"
	"github.com/percona/qan-agent/log"
	"github.com/percona/qan-agent/mrms"
	"github.com/percona/qan-agent/mysql"
	"github.com/percona/qan-agent/pct"
	pctCmd "github.com/percona/qan-agent/pct/cmd"
	"github.com/percona/qan-agent/qan"
	qanFactory "github.com/percona/qan-agent/qan/factory"
	"github.com/percona/qan-agent/qan/perfschema"
	"github.com/percona/qan-agent/qan/slowlog"
	"github.com/percona/qan-agent/query"
	"github.com/percona/qan-agent/ticker"
)

var (
	flagBasedir string
	flagPidFile string
	flagListen  string
	flagPing    bool
	flagVersion bool
)

func init() {
	if os.Getenv("GOMAXPROCS") == "" {
		runtime.GOMAXPROCS(1)
	}

	golog.SetFlags(golog.Ldate | golog.Ltime | golog.Lmicroseconds | golog.Lshortfile)
	golog.SetOutput(os.Stdout)

	flag.StringVar(&flagBasedir, "basedir", pct.DEFAULT_BASEDIR, "Agent basedir")
	flag.StringVar(&flagPidFile, "pid-file", agent.DEFAULT_PIDFILE, "PID file")
	flag.StringVar(&flagListen, "listen", agent.DEFAULT_LISTEN, "Agent interface address")
	flag.BoolVar(&flagPing, "ping", false, "Ping API")
	flag.BoolVar(&flagVersion, "version", false, "Print version")

	flag.Parse()

	// We don't accept any possitional arguments
	if len(flag.Args()) != 0 {
		flag.Usage()
		os.Exit(1)
	}
}

func main() {
	// //////////////////////////////////////////////////////////////////////
	// Handle options like -version which don't start the agent.
	// //////////////////////////////////////////////////////////////////////

	agentVersion := fmt.Sprintf("percona-qan-agent %s", release.VERSION)
	if flagVersion {
		fmt.Println(agentVersion)
		return
	}

	if err := pct.Basedir.Init(flagBasedir); err != nil {
		fmt.Printf("Error initializing basedir %s: %s", flagBasedir, err)
		os.Exit(1)
	}

	// Read agent.conf to get API hostname and agent UUID.
	agentConfigFile := pct.Basedir.ConfigFile("agent")
	if !pct.FileExists(agentConfigFile) {
		fmt.Printf("Agent config file %s does not exist", agentConfigFile)
		os.Exit(1)
	}

	bytes, err := agent.LoadConfig()
	if err != nil {
		fmt.Print("Error reading agent config file %s: %s", agentConfigFile, err)
		os.Exit(1)
	}
	agentConfig := &pc.Agent{}
	if err := json.Unmarshal(bytes, agentConfig); err != nil {
		fmt.Printf("Error decoding agent config file %s: %s", agentConfigFile, err)
		os.Exit(1)
	}

	fmt.Printf("# Version: %s\n", agentVersion)
	fmt.Printf("# Basedir: %s\n", pct.Basedir.Path())
	fmt.Printf("# Listen:  %s\n", flagListen)
	fmt.Printf("# PID:     %d\n", os.Getpid())
	fmt.Printf("# API:     %s\n", agentConfig.ApiHostname)
	fmt.Printf("# UUID:    %s\n", agentConfig.UUID)

	// -ping and exit.
	if flagPing {
		t0 := time.Now()
		code, err := pct.Ping(agentConfig.ApiHostname, nil)
		d := time.Now().Sub(t0)
		if err != nil || code != 200 {
			fmt.Printf("Ping FAIL (%d %d %s)", d, code, err)
			os.Exit(1)
		}
		fmt.Printf("Ping OK (%s)", d)
		return
	}

	// //////////////////////////////////////////////////////////////////////
	// Run the agent
	// //////////////////////////////////////////////////////////////////////

	// Create PID file or die trying.
	pidFile := pct.NewPidFile()
	if err := pidFile.Set(flagPidFile); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	err = run(agentConfig) // run the agent

	pidFile.Remove() // always remove PID file

	if err != nil {
		golog.Println(err)
		os.Exit(1)
	}
}

func run(agentConfig *pc.Agent) error {
	golog.Println("Starting agent...")
	var stopErr error
	defer func() {
		if stopErr == nil {
			golog.Println("Agent has stopped")
		}
	}()

	// //////////////////////////////////////////////////////////////////////
	// Internal services, factories, and other dependencies
	// //////////////////////////////////////////////////////////////////////
	pctCmd.Factory = &pctCmd.RealCmdFactory{}
	connFactory := &mysql.RealConnectionFactory{}
	nowFunc := func() int64 { return time.Now().UTC().UnixNano() }
	clock := ticker.NewClock(&ticker.RealTickerFactory{}, nowFunc)

	// //////////////////////////////////////////////////////////////////////
	// Start API interface
	// //////////////////////////////////////////////////////////////////////

	// The API interface provides low-level functionality to websocket clients.
	// To be useful, it must connect once to get resource links. Do this async
	// so in case we're offline the agent still starts and collects data. We
	// can spool and send data later when API is online.
	api := pct.NewAPI()
	go func() {
		haveWarned := false
		for {
			if err := api.Connect(agentConfig.ApiHostname, agentConfig.UUID); err != nil {
				if !haveWarned {
					golog.Printf("Cannot connect to API: %s. Verify that the"+
						" agent UUID and API hostname printed above are"+
						" correct and that no network issues prevent this"+
						" host from accessing the API. Connection"+
						" attempts to API will continue until successful, but"+
						" additional errors will not be logged, and agent"+
						" will not send data until connected to API.", err)
					haveWarned = true // don't flood log in case we're offline for a long time
				}
				time.Sleep(3 * time.Second)
				continue
			}
			golog.Println("API is ready")
			return
		}
	}()

	// //////////////////////////////////////////////////////////////////////
	// Agent services
	// //////////////////////////////////////////////////////////////////////

	// Log relay
	logChan := make(chan proto.LogEntry, log.BUFFER_SIZE*2)
	logClient, err := client.NewWebsocketClient(pct.NewLogger(logChan, "log-ws"), api, "log", nil)
	if err != nil {
		return err
	}
	logManager := log.NewManager(
		logClient,
		logChan,
	)
	if err := logManager.Start(); err != nil {
		return fmt.Errorf("Error starting log manager: %s", err)
	}

	// MRMS (MySQL Restart Monitoring Service)
	mrmsMonitor := mrms.NewRealMonitor(
		pct.NewLogger(logChan, "mrms-monitor"),
		&mysql.RealConnectionFactory{},
	)
	mrmsManager := mrms.NewManager(
		pct.NewLogger(logChan, "mrms-manager"),
		mrmsMonitor,
	)
	if err := mrmsManager.Start(); err != nil {
		return fmt.Errorf("Error starting mrms manager: %s", err)
	}

	// Instance manager
	itManager := instance.NewManager(
		pct.NewLogger(logChan, "instance-manager"),
		pct.Basedir.Dir("instance"),
		api,
		mrmsMonitor,
	)
	if err := itManager.Start(); err != nil {
		return fmt.Errorf("Error starting instance manager: %s", err)
	}

	// Data spooler and sender
	hostname, _ := os.Hostname()
	dataClient, err := client.NewWebsocketClient(pct.NewLogger(logChan, "data-ws"), api, "data", nil)
	if err != nil {
		return err
	}
	dataManager := data.NewManager(
		pct.NewLogger(logChan, "data"),
		pct.Basedir.Dir("data"),
		pct.Basedir.Dir("trash"),
		hostname,
		dataClient,
	)
	if err := dataManager.Start(); err != nil {
		return fmt.Errorf("Error starting data manager: %s", err)
	}

	// Query (real-time EXPLAIN, SHOW CREATE TABLE, etc.)
	queryManager := query.NewManager(
		pct.NewLogger(logChan, "query"),
		itManager.Repo(),
		&mysql.RealConnectionFactory{},
	)
	if err := queryManager.Start(); err != nil {
		return fmt.Errorf("Error starting query manager: %s", err)
	}

	// Query Analytics
	qanManager := qan.NewManager(
		pct.NewLogger(logChan, "qan"),
		clock,
		itManager.Repo(),
		mrmsMonitor,
		connFactory,
		qanFactory.NewRealAnalyzerFactory(
			logChan,
			qanFactory.NewRealIntervalIterFactory(logChan),
			slowlog.NewRealWorkerFactory(logChan),
			perfschema.NewRealWorkerFactory(logChan),
			dataManager.Spooler(),
			clock,
		),
	)
	if err := qanManager.Start(); err != nil {
		return fmt.Errorf("Error starting qan manager: %s", err)
	}

	// //////////////////////////////////////////////////////////////////////
	// Create and start the agent
	// //////////////////////////////////////////////////////////////////////

	cmdClient, err := client.NewWebsocketClient(pct.NewLogger(logChan, "agent-ws"), api, "cmd", nil)
	if err != nil {
		return err
	}
	agentLogger := pct.NewLogger(logChan, "agent")
	agentRouter := agent.NewAgent(
		agentConfig,
		agentLogger,
		cmdClient,
		flagListen,
		map[string]pct.ServiceManager{ // agent services
			"log":      logManager,
			"data":     dataManager,
			"qan":      qanManager,
			"instance": itManager,
			"mrms":     mrmsManager,
			"query":    queryManager,
		},
	)

	// Run the agent, wait for it to stop, signal, or crash.
	stopChan := make(chan error, 2)
	go func() {
		defer func() {
			if err := recover(); err != nil {
				errMsg := fmt.Sprintf("Agent crashed: %s", err)
				golog.Println(errMsg)
				agentLogger.Error(errMsg)
				stopChan <- fmt.Errorf("%s", errMsg)
			}
		}()
		stopChan <- agentRouter.Run() // ----- RUN THE AGENT -----
	}()

	// Run local agent API.
	lo := agent.NewLocalInterface(flagListen, agentRouter, itManager.Repo())
	go lo.Run()

	golog.Println("Agent is ready")

	// //////////////////////////////////////////////////////////////////////
	// Signal handlers
	// //////////////////////////////////////////////////////////////////////

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	reconnectSigChan := make(chan os.Signal, 1)
	signal.Notify(reconnectSigChan, syscall.SIGHUP) // kill -HUP PID

	intSigChan := make(chan os.Signal, 1)
	signal.Notify(intSigChan, syscall.SIGINT) // CTRL-C

	// //////////////////////////////////////////////////////////////////////
	// Wait for agent stop, signals, etc.
	// //////////////////////////////////////////////////////////////////////

SIGNAL_LOOP:
	for {
		select {
		case stopErr = <-stopChan:
			// stopErr is logged in defer
			break SIGNAL_LOOP // stop running
		case sig := <-sigChan:
			msg := fmt.Sprintf("Caught %s signal, shutting down", sig)
			agentLogger.Info(msg)
			golog.Println(msg)
			break SIGNAL_LOOP // stop running
		case <-intSigChan:
			msg := "Caught CTRL-C (SIGINT), shutting down"
			agentLogger.Warn(msg)
			golog.Println(msg)
			break SIGNAL_LOOP // stop running
		case <-reconnectSigChan:
			u, _ := user.Current()
			cmd := &proto.Cmd{
				Ts:        time.Now().UTC(),
				User:      u.Username + " (SIGHUP)",
				AgentUUID: agentConfig.UUID,
				Service:   "agent",
				Cmd:       "Reconnect",
			}
			agentRouter.Handle(cmd)
		}
	}

	// //////////////////////////////////////////////////////////////////////
	// Clean up, undo any changes we made to MySQL.
	/////////////////////////////////////////////////////////////////////////

	// Disable slow log or perf schema. It's not terrible if we don't, but
	// it's a lot better if we do.
	golog.Println("Stopping QAN...")
	if err := qanManager.Stop(); err != nil {
		msg := fmt.Sprintf("Cannot stop QAN: %s", err)
		agentLogger.Warn(msg)
		golog.Printf(msg)
	}

	golog.Println("Waiting 2 seconds to flush agent log to API...")
	time.Sleep(2 * time.Second) // wait for last replies and log entries

	return stopErr
}
