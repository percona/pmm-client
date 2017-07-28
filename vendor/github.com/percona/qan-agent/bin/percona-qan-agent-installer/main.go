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
	"flag"
	"fmt"
	"log"
	"os"
	"regexp"

	"github.com/percona/pmm/proto"
	pc "github.com/percona/pmm/proto/config"
	"github.com/percona/qan-agent/bin/percona-qan-agent-installer/installer"
	"github.com/percona/qan-agent/bin/percona-qan-agent-installer/term"
	"github.com/percona/qan-agent/instance"
	"github.com/percona/qan-agent/pct"
)

const DEFAULT_DATASTORE_PORT = "9001"

var (
	flagBasedir                 string
	flagVerbose                 bool
	flagOldPasswords            bool
	flagInteractive             bool
	flagDebug                   bool
	flagMySQL                   bool
	flagMySQLDefaultsFile       string
	flagMySQLUser               string
	flagMySQLPass               string
	flagMySQLHost               string
	flagMySQLPort               string
	flagMySQLSocket             string
	flagMySQLMaxUserConnections int64
	flagQuerySource             string
	flagAgentMySQLUser          string
	flagAgentMySQLPass          string
)

var fs *flag.FlagSet

func init() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds | log.Lshortfile)

	fs = flag.NewFlagSet(os.Args[0], flag.ExitOnError)

	fs.BoolVar(&flagMySQL, "mysql", true, "Create MySQL instance")
	fs.BoolVar(&flagDebug, "debug", false, "Debug")
	fs.StringVar(&flagBasedir, "basedir", pct.DEFAULT_BASEDIR, "Agent basedir")
	fs.StringVar(&flagQuerySource, "query-source", "auto", "Where to collect queries: slowlog, perfschema, auto")

	fs.StringVar(&flagMySQLUser, "user", "", "MySQL username")
	fs.StringVar(&flagMySQLPass, "password", "", "MySQL password")
	fs.StringVar(&flagMySQLHost, "host", "", "MySQL host")
	fs.StringVar(&flagMySQLPort, "port", "", "MySQL port")
	fs.StringVar(&flagMySQLSocket, "socket", "", "MySQL socket file")
	fs.StringVar(&flagMySQLDefaultsFile, "defaults-file", "", "Path to my.cnf")

	fs.StringVar(&flagAgentMySQLUser, "agent-user", "", "Existing MySQL username for agent")
	fs.StringVar(&flagAgentMySQLPass, "agent-password", "", "Existing MySQL password for agent")

	fs.Int64Var(&flagMySQLMaxUserConnections, "max-user-connections", 5, "Max number of MySQL connections")
	fs.BoolVar(&flagOldPasswords, "old-passwords", false, "Old passwords")

}

var portSuffix *regexp.Regexp = regexp.MustCompile(`:\d+$`)

func main() {
	// It flag is unknown it exist with os.Exit(10),
	// so exit code=10 is strictly reserved for flags
	// Don't use it anywhere else, as shell script install.sh depends on it
	// NOTE: standard flag.Parse() was using os.Exit(2)
	//       which was the same as returned with ctrl+c
	if err := fs.Parse(os.Args[1:]); err != nil {
		if err == flag.ErrHelp {
			return
		} else {
			log.Fatal(err)
		}
	}

	args := fs.Args()
	if len(args) != 1 {
		fs.PrintDefaults()
		fmt.Printf("Got %d args, expected 1: API_HOST[:PORT]\n", len(args))
		fmt.Fprintf(os.Stderr, "Usage: %s [options] API_HOST[:PORT]\n", os.Args[0])
		os.Exit(1)
	}

	if !portSuffix.Match([]byte(args[0])) {
		args[0] += ":" + DEFAULT_DATASTORE_PORT
	}

	agentConfig := &pc.Agent{
		ApiHostname: args[0],
	}

	if flagMySQLSocket != "" && flagMySQLHost != "" {
		log.Print("Options -socket and -host are exclusive\n\n")
		os.Exit(1)
	}

	if flagMySQLSocket != "" && flagMySQLPort != "" {
		log.Print("Options -socket and -port are exclusive\n\n")
		os.Exit(1)
	}

	if flagQuerySource != "auto" && flagQuerySource != "slowlog" && flagQuerySource != "perfschema" {
		log.Printf("Invalid value for -query-source: '%s'\n\n", flagQuerySource)
		os.Exit(1)
	}

	flags := installer.Flags{
		Bool: map[string]bool{
			"mysql":         flagMySQL,
			"debug":         flagDebug,
			"old-passwords": flagOldPasswords,
		},
		String: map[string]string{
			"mysql-defaults-file": flagMySQLDefaultsFile,
			"mysql-user":          flagMySQLUser,
			"mysql-pass":          flagMySQLPass,
			"mysql-host":          flagMySQLHost,
			"mysql-port":          flagMySQLPort,
			"mysql-socket":        flagMySQLSocket,
			"query-source":        flagQuerySource,
			"agent-mysql-user":    flagAgentMySQLUser,
			"agent-mysql-pass":    flagAgentMySQLPass,
		},
		Int64: map[string]int64{
			"mysql-max-user-connections": flagMySQLMaxUserConnections,
		},
	}

	fmt.Println("CTRL-C at any time to quit")

	api := pct.NewAPI()
	if _, err := api.Init(agentConfig.ApiHostname, nil); err != nil {
		fmt.Printf("Cannot connect to API %s: %s\n", agentConfig.ApiHostname, err)
		os.Exit(1)
	}
	fmt.Printf("API host: %s\n", pct.URL(agentConfig.ApiHostname))

	// Agent stores all its files in the basedir.  This must be called first
	// because installer uses pct.Basedir and assumes it's already initialized.
	if err := pct.Basedir.Init(flagBasedir); err != nil {
		log.Printf("Error initializing basedir %s: %s\n", flagBasedir, err)
		os.Exit(1)
	}

	logChan := make(chan proto.LogEntry, 100)
	logger := pct.NewLogger(logChan, "instance-repo")
	instanceRepo := instance.NewRepo(logger, pct.Basedir.Dir("config"), api)
	terminal := term.NewTerminal(os.Stdin, flagInteractive, flagDebug)
	agentInstaller, err := installer.NewInstaller(terminal, flagBasedir, api, instanceRepo, agentConfig, flags)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	// todo: catch SIGINT and clean up
	if err := agentInstaller.Run(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	os.Exit(0)
}
