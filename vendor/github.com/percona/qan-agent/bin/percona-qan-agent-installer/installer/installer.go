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

package installer

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"

	_ "github.com/go-sql-driver/mysql"
	"github.com/nu7hatch/gouuid"
	"github.com/percona/go-mysql/dsn"
	"github.com/percona/pmm/proto"
	pc "github.com/percona/pmm/proto/config"
	"github.com/percona/qan-agent/agent/release"
	"github.com/percona/qan-agent/bin/percona-qan-agent-installer/term"
	"github.com/percona/qan-agent/instance"
	"github.com/percona/qan-agent/pct"
)

var portNumberRe = regexp.MustCompile(`\.\d+$`)

type Flags struct {
	Bool   map[string]bool
	String map[string]string
	Int64  map[string]int64
}

type Installer struct {
	term         *term.Terminal
	basedir      string
	api          pct.APIConnector
	instanceRepo *instance.Repo
	agentConfig  *pc.Agent
	flags        Flags
	// --
	os            *proto.Instance
	agent         *proto.Instance
	mysql         *proto.Instance
	dsnUser       dsn.DSN
	dsnAgent      dsn.DSN
	debug         bool
	mysqlDistro   string
	mysqlVersion  string
	mysqlHostname string
}

func NewUUID() string {
	u4, err := uuid.NewV4()
	if err != nil {
		fmt.Println("Could not create UUID4: %v", err)
		return ""
	}
	return strings.Replace(u4.String(), "-", "", -1)
}

func NewInstaller(terminal *term.Terminal, basedir string, api pct.APIConnector, instanceRepo *instance.Repo, agentConfig *pc.Agent, flags Flags) (*Installer, error) {
	dsnUser := dsn.DSN{
		DefaultsFile: flags.String["mysql-defaults-file"],
		Username:     flags.String["mysql-user"],
		Password:     flags.String["mysql-pass"],
		Hostname:     flags.String["mysql-host"],
		Port:         flags.String["mysql-port"],
		Socket:       flags.String["mysql-socket"],
	}

	installer := &Installer{
		term:         terminal,
		basedir:      basedir,
		api:          api,
		instanceRepo: instanceRepo,
		agentConfig:  agentConfig,
		flags:        flags,
		// --
		dsnUser: dsnUser,
		debug:   flags.Bool["debug"],
	}
	return installer, nil
}

func (i *Installer) Run() (err error) {
	if i.debug {
		fmt.Printf("basedir: %s\n", i.basedir)
		fmt.Printf("Agent.Config: %+v\n", i.agentConfig)
	}

	// Must create OS instance first because agent instance references it.
	if err = i.CreateOSInstance(); err != nil {
		return fmt.Errorf("Failed to create OS instance: %s", err)
	}

	if err := i.CreateAgent(); err != nil {
		return fmt.Errorf("Failed to create agent instance: %s", err)
	}

	// Auto-detect and create MySQL instance if -mysql=true (default).
	if i.flags.Bool["mysql"] {
		if err = i.CreateMySQLInstance(); err != nil {
			return fmt.Errorf("Failed to create MySQL instance: %s", err)
		}
	}

	configs, err := i.GetDefaultConfigs(i.os, i.mysql)
	if err != nil {
		return err
	}
	if err := i.writeConfigs(configs); err != nil {
		return fmt.Errorf("Failed to write configs: %s", err)
	}

	return nil
}

func (i *Installer) CreateOSInstance() error {
	hostname, _ := os.Hostname()
	i.os = &proto.Instance{
		Subsystem: "os",
		UUID:      NewUUID(),
		Name:      hostname,
	}
	created, err := i.api.CreateInstance("/instances", i.os)
	if err != nil {
		return err
	}

	// todo: distro, version

	if err := i.instanceRepo.Add(*i.os, true); err != nil {
		return err
	}

	if created {
		fmt.Printf("Created OS: name=%s uuid=%s\n", i.os.Name, i.os.UUID)
	} else {
		fmt.Printf("Using existing OS instance: name=%s uuid=%s\n", i.os.Name, i.os.UUID)
	}
	return nil
}

func (i *Installer) CreateAgent() error {
	i.agent = &proto.Instance{
		Subsystem:  "agent",
		UUID:       NewUUID(),
		ParentUUID: i.os.UUID,
		Name:       i.os.Name,
		Version:    release.VERSION,
	}
	created, err := i.api.CreateInstance("/instances", i.agent)
	if err != nil {
		return err
	}

	// To save data we need agent config with uuid and links
	i.agentConfig.UUID = i.agent.UUID
	i.agentConfig.Links = i.agent.Links

	if created {
		fmt.Printf("Created agent instance: name=%s uuid=%s\n", i.agent.Name, i.agent.UUID)
	} else {
		fmt.Printf("Using existing agent instance: name=%s uuid=%s\n", i.agent.Name, i.agent.UUID)
	}
	return nil
}

func (i *Installer) CreateMySQLInstance() error {
	// Get MySQL DSN for agent to use. It is new MySQL user created just for
	// agent, or user is asked for existing one. DSN is verified prior returning
	// by connecting to MySQL.
	dsn, err := i.getAgentDSN()
	if err != nil {
		return err
	}

	i.mysql = &proto.Instance{
		Subsystem:  "mysql",
		ParentUUID: i.os.UUID,
		UUID:       NewUUID(),
		Name:       i.mysqlHostname,
		DSN:        dsn.String(),
	}

	created, err := i.api.CreateInstance("/instances", i.mysql)
	if err != nil {
		return err
	}

	if err := i.instanceRepo.Add(*i.mysql, true); err != nil {
		return err
	}

	if created {
		fmt.Printf("Created MySQL instance: name=%s uuid=%s\n", i.mysql.Name, i.mysql.UUID)
	} else {
		fmt.Printf("Using existing MySQL instance: name=%s uuid=%s\n", i.mysql.Name, i.mysql.UUID)
	}
	return nil
}

func (i *Installer) GetDefaultConfigs(os, mysql *proto.Instance) (configs []proto.AgentConfig, err error) {
	agentConfig, err := i.getAgentConfig()
	if err != nil {
		return nil, err
	}
	configs = append(configs, *agentConfig)

	// We don't need log and data configs. They use all built-in defaults.

	// QAN config with defaults
	if mysql != nil {
		if i.flags.String["query-source"] == "auto" {
			// MySQL is local if the server hostname == MySQL hostname without port number.
			mysqlHostname := portNumberRe.ReplaceAllLiteralString(mysql.Name, "")
			if i.flags.Bool["debug"] {
				log.Printf("Hostnames: os='%s', MySQL='%s'\n", os.Name, mysqlHostname)
			}
			if os.Name == mysqlHostname {
				i.flags.String["query-source"] = "slowlog"
			} else {
				i.flags.String["query-source"] = "perfschema"
			}
		}
		fmt.Printf("Query source: %s\n", i.flags.String["query-source"])
		config, err := i.getQANConfig(i.flags.String["query-source"])
		if err != nil {
			fmt.Printf("WARNING: cannot start Query Analytics: %s\n", err)
		} else {
			configs = append(configs, *config)
		}
	}

	return configs, nil
}

func (i *Installer) writeConfigs(configs []proto.AgentConfig) error {
	for _, config := range configs {
		name := config.Service
		switch name {
		case "qan":
			name += "-" + config.UUID
		}
		if err := pct.Basedir.WriteConfigString(name, config.Set); err != nil {
			return err
		}
	}

	return nil
}

func (i *Installer) getAgentConfig() (*proto.AgentConfig, error) {
	configJson, err := json.Marshal(i.agentConfig)
	if err != nil {
		return nil, err
	}
	agentConfig := &proto.AgentConfig{
		Service: "agent",
		Set:     string(configJson),
	}

	return agentConfig, nil
}

func (i *Installer) getQANConfig(collectFrom string) (*proto.AgentConfig, error) {
	config := map[string]string{
		"UUID":        i.mysql.UUID,
		"CollectFrom": collectFrom,
		// All defaults, created at runtime by qan manager
	}
	bytes, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	agentConfig := &proto.AgentConfig{
		UUID:    i.mysql.UUID,
		Service: "qan",
		Set:     string(bytes),
	}
	return agentConfig, nil
}
