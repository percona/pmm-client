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
	"fmt"
	"os"

	"github.com/percona/pmm-client/pmm"
	"github.com/spf13/cobra"
)

var (
	admin pmm.Admin

	rootCmd = &cobra.Command{
		Use: "pmm-admin",
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			// NOTE: this function pre-runs with every command or sub-command with
			// the only exception "pmm-admin config" which bypasses it.

			// This flag will not run anywhere else than on rootCmd as this flag is not persistent one
			// and we want it only here without any config checks.
			if flagVersion {
				fmt.Println(pmm.VERSION)
				os.Exit(0)
			}

			// No checks when running w/o commands.
			if cmd.Name() == "pmm-admin" {
				return
			}

			if path := pmm.CheckBinaries(); path != "" {
				fmt.Println("Installation problem, one of the binaries does not exist:", path)
				os.Exit(1)
			}

			// Read config file.
			if !pmm.FileExists(flagConfigFile) {
				fmt.Println(flagConfigFile, "does not exist. Please make sure you have run ./install script.")
				os.Exit(1)
			}

			if err := admin.LoadConfig(flagConfigFile); err != nil {
				fmt.Printf("Error reading %s: %s\n", flagConfigFile, err)
				os.Exit(1)
			}

			if admin.Config.ServerAddress == "" || admin.Config.ClientAddress == "" || admin.Config.ClientName == "" {
				fmt.Println(flagConfigFile, "exists but some options are missed. Run 'pmm-admin config --help'.")
				os.Exit(1)
			}

			// "pmm-admin info" should display info w/o connectivity.
			if cmd.Name() == "info" {
				return
			}

			admin.SetAPI()
			// Check if server is alive.
			if !admin.ServerAlive() {
				fmt.Printf("Unable to connect to PMM server by address: %s\n\n", admin.Config.ServerAddress)
				fmt.Println(`* Check if the configured address is correct.
* If server container is running on non-default port, ensure it was specified along with the address.
* If server is enabled for SSL or self-signed SSL or password protected, ensure the corresponding flags were set.
* You may also check the firewall settings.`)
				os.Exit(1)
			}
		},
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Usage()
			os.Exit(1)
		},
	}

	cmdAdd = &cobra.Command{
		Use:   "add",
		Short: "Add service to monitoring.",
		Long:  "This command is used to add a monitoring service.",
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			cmd.Root().PersistentPreRun(cmd.Root(), args)
			admin.ServiceName = admin.Config.ClientName
			admin.ServicePort = flagServicePort
			if len(args) > 0 {
				admin.ServiceName = args[0]
			}
		},
	}
	cmdAddMySQL = &cobra.Command{
		Use:   "mysql [name]",
		Short: "Add complete monitoring for MySQL instance.",
		Long: `This command adds the given MySQL instance to system, metrics and queries monitoring.

When adding a MySQL instance, this tool tries to auto-detect the DSN and credentials.
If you want to create a new user to be used for metrics collecting, provide --create-user option. pmm-admin will create
a new user 'pmm@' automatically using the given (auto-detected) MySQL credentials for granting purpose.

[name] is an optional argument, by default it is set to the client name of this PMM client.
		`,
		Example: `  pmm-admin add mysql --password abc123
  pmm-admin add mysql --password abc123 --create-user`,
		Run: func(cmd *cobra.Command, args []string) {
			// Check --query-source flag.
			if flagM.QuerySource != "auto" && flagM.QuerySource != "slowlog" && flagM.QuerySource != "perfschema" {
				fmt.Println("Flag --query-source can take the following values: auto, slowlog, perfschema.")
				os.Exit(1)
			}

			if err := admin.AddLinuxMetrics(); err != nil {
				fmt.Println("Error adding linux metrics:", err)
				os.Exit(1)
			}
			fmt.Println("OK, now monitoring this system.")

			info, err := pmm.DetectMySQL(flagM)
			if err != nil {
				fmt.Println(err)
				os.Exit(1)
			}
			if err := admin.AddMySQLMetrics(info, flagM); err != nil {
				fmt.Println("Error adding MySQL metrics:", err)
				os.Exit(1)
			}
			fmt.Println("OK, now monitoring MySQL metrics using DSN", info["safe_dsn"])

			if err := admin.AddMySQLQueries(info); err != nil {
				fmt.Println("Error adding MySQL queries:", err)
				os.Exit(1)
			}
			fmt.Println("OK, now monitoring MySQL queries from", info["query_source"], "using DSN",
				info["safe_dsn"])
		},
	}
	cmdAddLinuxMetrics = &cobra.Command{
		Use:   "linux:metrics [name]",
		Short: "Add this system to metrics monitoring.",
		Long: `This command adds this system to linux metrics monitoring.

There could be only one instance of linux metrics being monitored for this system.

[name] is an optional argument, by default it is set to the client name of this PMM client.
		`,
		Run: func(cmd *cobra.Command, args []string) {
			if err := admin.AddLinuxMetrics(); err != nil {
				fmt.Println("Error adding linux metrics:", err)
				os.Exit(1)
			}
			fmt.Println("OK, now monitoring this system.")
		},
	}
	cmdAddMySQLMetrics = &cobra.Command{
		Use:   "mysql:metrics [name]",
		Short: "Add MySQL instance to metrics monitoring.",
		Long: `This command adds the given MySQL instance to metrics monitoring.

When adding a MySQL instance, this tool tries to auto-detect the DSN and credentials.
If you want to create a new user to be used for metrics collecting, provide --create-user option. pmm-admin will create
a new user 'pmm@' automatically using the given (auto-detected) MySQL credentials for granting purpose.

[name] is an optional argument, by default it is set to the client name of this PMM client.
		`,
		Example: `  pmm-admin add mysql:metrics --password abc123
  pmm-admin add mysql:metrics --password abc123 --host 192.168.1.2 --create-user
  pmm-admin add mysql:metrics --user rdsuser --password abc123 --host my-rds.1234567890.us-east-1.rds.amazonaws.com my-rds`,
		Run: func(cmd *cobra.Command, args []string) {
			info, err := pmm.DetectMySQL(flagM)
			if err != nil {
				fmt.Println(err)
				os.Exit(1)
			}
			if err := admin.AddMySQLMetrics(info, flagM); err != nil {
				fmt.Println("Error adding MySQL metrics:", err)
				os.Exit(1)
			}
			fmt.Println("OK, now monitoring MySQL metrics using DSN", info["safe_dsn"])
		},
	}
	cmdAddMySQLQueries = &cobra.Command{
		Use:   "mysql:queries [name]",
		Short: "Add MySQL instance to Query Analytics.",
		Long: `This command adds the given MySQL instance to Query Analytics.

When adding a MySQL instance, this tool tries to auto-detect the DSN and credentials.
If you want to create a new user to be used for query collecting, provide --create-user option. pmm-admin will create
a new user 'pmm@' automatically using the given (auto-detected) MySQL credentials for granting purpose.

[name] is an optional argument, by default it is set to the client name of this PMM client.
		`,
		Example: `  pmm-admin add mysql:queries --password abc123
  pmm-admin add mysql:queries --password abc123 --host 192.168.1.2 --create-user
  pmm-admin add mysql:queries --user rdsuser --password abc123 --host my-rds.1234567890.us-east-1.rds.amazonaws.com my-rds`,
		Run: func(cmd *cobra.Command, args []string) {
			// Check --query-source flag.
			if flagM.QuerySource != "auto" && flagM.QuerySource != "slowlog" && flagM.QuerySource != "perfschema" {
				fmt.Println("Flag --query-source can take the following values: auto, slowlog, perfschema.")
				os.Exit(1)
			}
			info, err := pmm.DetectMySQL(flagM)
			if err != nil {
				fmt.Println(err)
				os.Exit(1)
			}
			if err := admin.AddMySQLQueries(info); err != nil {
				fmt.Println("Error adding MySQL queries:", err)
				os.Exit(1)
			}
			fmt.Println("OK, now monitoring MySQL queries from", info["query_source"], "using DSN",
				info["safe_dsn"])
		},
	}
	cmdAddMongoDB = &cobra.Command{
		Use:   "mongodb [name]",
		Short: "Add complete monitoring for MongoDB instance.",
		Long: `This command adds the given MongoDB instance to system and metrics monitoring.

When adding a MongoDB instance, you may provide --uri if the default one does not work for you.
Use additional options to specify MongoDB node type, cluster, replSet etc.

[name] is an optional argument, by default it is set to the client name of this PMM client.
		`,
		Example: `  pmm-admin add mongodb
  pmm-admin add mongodb --nodetype mongod --cluster cluster-1.2 --replset rs1`,
		Run: func(cmd *cobra.Command, args []string) {
			// Check --nodetype flag.
			if flagMongoNodeType != "" && flagMongoNodeType != "mongod" && flagMongoNodeType != "mongos" &&
				flagMongoNodeType != "config" && flagMongoNodeType != "arbiter" {
				fmt.Println("Flag --nodetype can take the following values: mongod, mongos, config, arbiter.")
				os.Exit(1)
			}

			if err := admin.AddLinuxMetrics(); err != nil {
				fmt.Println("Error adding linux metrics:", err)
				os.Exit(1)
			}
			fmt.Println("OK, now monitoring this system.")

			if err := admin.AddMongoDBMetrics(flagMongoURI, flagMongoNodeType, flagMongoReplSet, flagMongoCluster); err != nil {
				fmt.Println("Error adding MongoDB metrics:", err)
				os.Exit(1)
			}
			fmt.Println("OK, now monitoring MongoDB metrics using URI", pmm.SanitizeDSN(flagMongoURI))
		},
	}
	cmdAddMongoDBMetrics = &cobra.Command{
		Use:   "mongodb:metrics [name]",
		Short: "Add MongoDB instance to metrics monitoring.",
		Long: `This command adds the given MongoDB instance to metrics monitoring.

When adding a MongoDB instance, you may provide --uri if the default one does not work for you.
Use additional options to specify MongoDB node type, cluster, replSet etc.

IMPORTANT: adding MongoDB instance to the monitoring with the existing linux:metrics one will rename the latter to match
the name of MongoDB instance and also sets the same options such as node type, cluster, replSet if provided.

[name] is an optional argument, by default it is set to the client name of this PMM client.
		`,
		Example: `  pmm-admin add mongodb:metrics
  pmm-admin add mongodb:metrics --nodetype mongod --cluster cluster-1.2 --replset rs1`,
		Run: func(cmd *cobra.Command, args []string) {
			// Check --nodetype flag.
			if flagMongoNodeType != "" && flagMongoNodeType != "mongod" && flagMongoNodeType != "mongos" &&
				flagMongoNodeType != "config" && flagMongoNodeType != "arbiter" {
				fmt.Println("Flag --nodetype can take the following values: mongod, mongos, config, arbiter.")
				os.Exit(1)
			}
			if err := admin.AddMongoDBMetrics(flagMongoURI, flagMongoNodeType, flagMongoReplSet, flagMongoCluster); err != nil {
				fmt.Println("Error adding MongoDB metrics:", err)
				os.Exit(1)
			}
			fmt.Println("OK, now monitoring MongoDB metrics using URI", pmm.SanitizeDSN(flagMongoURI))
		},
	}

	cmdRemove = &cobra.Command{
		Use:     "remove",
		Aliases: []string{"rm"},
		Short:   "Remove service from monitoring.",
		Long:    "This command is used to remove a monitoring service.",
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			cmd.Root().PersistentPreRun(cmd.Root(), args)
			if len(args) == 0 {
				fmt.Println("No name specified.\n")
				cmd.Usage()
				os.Exit(1)
			}
		},
	}
	cmdRemoveMySQL = &cobra.Command{
		Use:   "mysql NAME",
		Short: "Remove all monitoring for MySQL instance.",
		Long: `This command removes all monitoring for MySQL instance specified by NAME.

The command did not stop when one of the services is missed or failed to remove.
		`,
		Run: func(cmd *cobra.Command, args []string) {
			name := args[0]

			if err := admin.RemoveLinuxMetrics(name); err != nil {
				fmt.Printf("Error removing linux metrics %s: %s\n", name, err)
			} else {
				fmt.Printf("OK, removed system %s from monitoring.\n", name)
			}

			if err := admin.RemoveMySQLMetrics(name); err != nil {
				fmt.Printf("Error removing MySQL metrics %s: %s\n", name, err)
			} else {
				fmt.Printf("OK, removed MySQL metrics %s from monitoring.\n", name)
			}

			if err := admin.RemoveMySQLQueries(name); err != nil {
				fmt.Printf("Error removing MySQL queries %s: %s\n", name, err)
			} else {
				fmt.Printf("OK, removed MySQL queries %s from monitoring.\n", name)
			}
		},
	}
	cmdRemoveLinuxMetrics = &cobra.Command{
		Use:   "linux:metrics NAME",
		Short: "Remove this system from metrics monitoring.",
		Long:  "This command removes system specified by NAME from linux metrics monitoring.",
		Run: func(cmd *cobra.Command, args []string) {
			name := args[0]
			if err := admin.RemoveLinuxMetrics(name); err != nil {
				fmt.Printf("Error removing linux metrics %s: %s\n", name, err)
				os.Exit(1)
			}
			fmt.Printf("OK, removed system %s from monitoring.\n", name)
		},
	}
	cmdRemoveMySQLMetrics = &cobra.Command{
		Use:   "mysql:metrics NAME",
		Short: "Remove MySQL instance from metrics monitoring.",
		Long:  "This command removes MySQL instance specified by NAME from metrics monitoring.",
		Run: func(cmd *cobra.Command, args []string) {
			name := args[0]
			if err := admin.RemoveMySQLMetrics(name); err != nil {
				fmt.Printf("Error removing MySQL metrics %s: %s\n", name, err)
				os.Exit(1)
			}
			fmt.Printf("OK, removed MySQL metrics %s from monitoring.\n", name)
		},
	}
	cmdRemoveMySQLQueries = &cobra.Command{
		Use:   "mysql:queries NAME",
		Short: "Remove MySQL instance from Query Analytics.",
		Long:  "This command removes MySQL instance specified by NAME from Query Analytics.",
		Run: func(cmd *cobra.Command, args []string) {
			name := args[0]
			if err := admin.RemoveMySQLQueries(name); err != nil {
				fmt.Printf("Error removing MySQL queries %s: %s\n", name, err)
				os.Exit(1)
			}
			fmt.Printf("OK, removed MySQL queries %s from monitoring.\n", name)
		},
	}
	cmdRemoveMongoDB = &cobra.Command{
		Use:   "mongodb NAME",
		Short: "Remove all monitoring for MongoDB instance.",
		Long: `This command removes all monitoring for MongoDB instance specified by NAME.

The command did not stop when one of the services is missed or failed to remove.
		`,
		Run: func(cmd *cobra.Command, args []string) {
			name := args[0]

			if err := admin.RemoveLinuxMetrics(name); err != nil {
				fmt.Printf("Error removing linux metrics %s: %s\n", name, err)
			} else {
				fmt.Printf("OK, removed system %s from monitoring.\n", name)
			}

			if err := admin.RemoveMongoDBMetrics(name); err != nil {
				fmt.Printf("Error removing MongoDB metrics %s: %s\n", name, err)
			} else {
				fmt.Printf("OK, removed MongoDB metrics %s from monitoring.\n", name)
			}
		},
	}
	cmdRemoveMongoDBMetrics = &cobra.Command{
		Use:   "mongodb:metrics NAME",
		Short: "Remove MongoDB instance from metrics monitoring.",
		Long:  "This command removes MongoDB instance specified by NAME from metrics monitoring.",
		Run: func(cmd *cobra.Command, args []string) {
			name := args[0]
			if err := admin.RemoveMongoDBMetrics(name); err != nil {
				fmt.Printf("Error removing MongoDB metrics %s: %s\n", name, err)
				os.Exit(1)
			}
			fmt.Printf("OK, removed MongoDB metrics %s from monitoring.\n", name)
		},
	}

	cmdList = &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List monitoring services for this system.",
		Long:    "This command displays the list of monitoring services and their details.",
		Run: func(cmd *cobra.Command, args []string) {
			admin.PrintInfo()
			err := admin.List()
			if err != nil {
				fmt.Println("Error listing instances:", err)
				os.Exit(1)
			}
		},
	}

	cmdInfo = &cobra.Command{
		Use:   "info",
		Short: "Display PMM Client information.",
		Long:  "This command displays PMM client configuration details.",
		Run: func(cmd *cobra.Command, args []string) {
			admin.PrintInfo()
		},
	}

	cmdConfig = &cobra.Command{
		Use:   "config",
		Short: "Configure PMM Client.",
		Long: `This command configures pmm-admin to communicate with PMM server.

You can enable SSL or setup HTTP basic authentication.
When HTTP password and no user is given, the default username will be "pmm".

IMPORTANT: resetting server address clears up SSL and HTTP authentication if no corresponding flags are provided.`,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			// Cancel root's PersistentPreRun as we do not require config file to exist here.
			// If the config does not exist, we will init an empty and write on Run.
			if err := admin.LoadConfig(flagConfigFile); err != nil {
				fmt.Printf("Error reading %s: %s\n", flagConfigFile, err)
				os.Exit(1)
			}
		},
		Run: func(cmd *cobra.Command, args []string) {
			if flagC.ServerSSL && flagC.ServerInsecureSSL {
				fmt.Println("Flags --enable-ssl and --enable-insecure-ssl are mutually exclusive.")
				os.Exit(1)
			}
			if err := admin.SetConfig(flagC); err != nil {
				fmt.Printf("Error configuring PMM client: %s\n", err)
				os.Exit(1)
			}
			fmt.Println("OK, PMM server is", admin.Config.ServerAddress)
		},
	}

	cmdCheckNet = &cobra.Command{
		Use:   "check-network",
		Short: "Check network connectivity between client and server.",
		Long: `This command runs the tests against PMM server to verify a bi-directional network connectivity.

* Client --> Server
Under this section you will find whether Consul, Query Analytics and Prometheus APIs are alive.
Also there is a connection performance test results with PMM server displayed.

* Client <-- Server
Here you will see the status of individual Prometheus endpoints and whether it can scrape metrics from this system.
Note, even this client can reach the server successfully it does not mean Prometheus is able to scrape from exporters.

In case, some of the endpoints are in problem state, please check if the corresponding service is running ("pmm-admin list").
If all endpoints are down here and "pmm-admin list" shows all services are up,
please check the firewall settings whether this system allows incoming connections by address:port in question.`,
		Run: func(cmd *cobra.Command, args []string) {
			if err := admin.CheckNetwork(flagNoEmoji); err != nil {
				fmt.Println("Error checking network status:", err)
				os.Exit(1)
			}
		},
	}

	cmdPing = &cobra.Command{
		Use:   "ping",
		Short: "Check if PMM server is alive.",
		Long:  "This command verifies the connectivity with PMM server.",
		Run: func(cmd *cobra.Command, args []string) {
			// It's all good if PersistentPreRun didn't fail.
			fmt.Println("OK")
		},
	}

	cmdStart = &cobra.Command{
		Use:   "start TYPE NAME",
		Short: "Start service by type and name.",
		Long:  "This command starts the corresponding system service or all.",
		Example: `  pmm-admin start linux:metrics db01.vm
  pmm-admin start mysql:metrics db01.vm
  pmm-admin start mysql:queries db01.vm
  pmm-admin start mongodb:metrics db01.vm
  pmm-admin start --all`,
		Run: func(cmd *cobra.Command, args []string) {
			if flagAll {
				err, noServices := admin.StartStopAllMonitoring("start")
				if err != nil {
					fmt.Printf("Error starting one of the services: %s\n", err)
					os.Exit(1)
				}
				if noServices {
					fmt.Println("OK, no services found.")
				} else {
					fmt.Println("OK, all services are started.")
				}
				os.Exit(0)
			}

			// Check args.
			if len(args) < 2 {
				fmt.Println("No metric type or name specified.\n")
				cmd.Usage()
				os.Exit(1)
			}
			svcType := args[0]
			name := args[1]
			if svcType != "linux:metrics" && svcType != "mysql:metrics" && svcType != "mysql:queries" && svcType != "mongodb:metrics" {
				fmt.Println("METRIC argument can take the following values: linux:metrics, mysql:metrics, mysql:queries, mongodb:metrics.\n")
				cmd.Usage()
				os.Exit(1)
			}

			if err := admin.StartStopMonitoring("start", svcType, name); err != nil {
				fmt.Printf("Error starting %s service for %s: %s\n", svcType, name, err)
				os.Exit(1)
			}
			fmt.Printf("OK, started %s service for %s.\n", svcType, name)
		},
	}

	cmdStop = &cobra.Command{
		Use:   "stop TYPE NAME",
		Short: "Stop service by type and name.",
		Long:  "This command stops the corresponding system service or all.",
		Example: `  pmm-admin stop linux:metrics db01.vm
  pmm-admin stop mysql:metrics db01.vm
  pmm-admin stop mysql:queries db01.vm
  pmm-admin stop mongodb:metrics db01.vm
  pmm-admin stop --all`,
		Run: func(cmd *cobra.Command, args []string) {
			if flagAll {
				err, noServices := admin.StartStopAllMonitoring("stop")
				if err != nil {
					fmt.Printf("Error stopping one of the services: %s\n", err)
					os.Exit(1)
				}
				if noServices {
					fmt.Println("OK, no services found.")
				} else {
					fmt.Println("OK, all services are stopped.")
				}
				os.Exit(0)
			}

			// Check args.
			if len(args) < 2 {
				fmt.Println("No metric type or name specified.\n")
				cmd.Usage()
				os.Exit(1)
			}
			svcType := args[0]
			name := args[1]
			if svcType != "linux:metrics" && svcType != "mysql:metrics" && svcType != "mysql:queries" && svcType != "mongodb:metrics" {
				fmt.Println("METRIC argument can take the following values: linux:metrics, mysql:metrics, mysql:queries, mongodb:metrics.\n")
				cmd.Usage()
				os.Exit(1)
			}

			if err := admin.StartStopMonitoring("stop", svcType, name); err != nil {
				fmt.Printf("Error stopping %s service for %s: %s\n", svcType, name, err)
				os.Exit(1)
			}
			fmt.Printf("OK, stopped %s service for %s.\n", svcType, name)
		},
	}

	flagConfigFile, flagMongoURI, flagMongoNodeType, flagMongoReplSet, flagMongoCluster string

	flagVersion, flagNoEmoji, flagAll bool
	flagServicePort                   uint
	flagM                             pmm.MySQLFlags
	flagC                             pmm.Config
)

func main() {
	// Setup commands and flags.
	cobra.EnableCommandSorting = false
	rootCmd.AddCommand(cmdAdd, cmdRemove, cmdList, cmdConfig, cmdInfo, cmdCheckNet, cmdPing, cmdStart, cmdStop)
	cmdAdd.AddCommand(cmdAddMySQL, cmdAddLinuxMetrics, cmdAddMySQLMetrics, cmdAddMySQLQueries,
		cmdAddMongoDB, cmdAddMongoDBMetrics)
	cmdRemove.AddCommand(cmdRemoveMySQL, cmdRemoveLinuxMetrics, cmdRemoveMySQLMetrics, cmdRemoveMySQLQueries,
		cmdRemoveMongoDB, cmdRemoveMongoDBMetrics)

	rootCmd.PersistentFlags().StringVarP(&flagConfigFile, "config-file", "c", pmm.ConfigFile, "PMM config file")
	rootCmd.Flags().BoolVarP(&flagVersion, "version", "v", false, "show version")

	cmdConfig.Flags().StringVar(&flagC.ServerAddress, "server-address", "", "PMM server address, optionally with port number")
	cmdConfig.Flags().StringVar(&flagC.ClientAddress, "client-address", "", "Client address")
	cmdConfig.Flags().StringVar(&flagC.ClientName, "client-name", "", "Client name (node identifier on Consul)")
	cmdConfig.Flags().StringVar(&flagC.HttpUser, "http-user", "pmm", "HTTP user for PMM Server")
	cmdConfig.Flags().StringVar(&flagC.HttpPassword, "http-password", "", "HTTP password for PMM Server")
	cmdConfig.Flags().BoolVar(&flagC.ServerSSL, "enable-ssl", false, "Enable SSL to communicate with PMM Server")
	cmdConfig.Flags().BoolVar(&flagC.ServerInsecureSSL, "enable-insecure-ssl", false, "Enable insecure SSL (self-signed certificate) to communicate with PMM Server")

	cmdAdd.PersistentFlags().UintVar(&flagServicePort, "service-port", 0, "service port")

	cmdAddMySQL.Flags().StringVar(&flagM.DefaultsFile, "defaults-file", "", "path to my.cnf")
	cmdAddMySQL.Flags().StringVar(&flagM.Host, "host", "", "MySQL host")
	cmdAddMySQL.Flags().StringVar(&flagM.Port, "port", "", "MySQL port")
	cmdAddMySQL.Flags().StringVar(&flagM.User, "user", "", "MySQL username")
	cmdAddMySQL.Flags().StringVar(&flagM.Password, "password", "", "MySQL password")
	cmdAddMySQL.Flags().StringVar(&flagM.Socket, "socket", "", "MySQL socket")
	cmdAddMySQL.Flags().BoolVar(&flagM.CreateUser, "create-user", false, "create a new MySQL user")
	cmdAddMySQL.Flags().StringVar(&flagM.CreateUserPassword, "create-user-password", "", "optional password for a new MySQL user")
	cmdAddMySQL.Flags().UintVar(&flagM.MaxUserConn, "create-user-maxconn", 5, "max user connections for a new user")
	cmdAddMySQL.Flags().BoolVar(&flagM.Force, "force", false, "force to create or update MySQL user")
	cmdAddMySQL.Flags().BoolVar(&flagM.DisableTableStats, "disable-tablestats", false, "disable table statistics (for MySQL with a huge number of tables)")
	cmdAddMySQL.Flags().BoolVar(&flagM.DisableUserStats, "disable-userstats", false, "disable user statistics")
	cmdAddMySQL.Flags().BoolVar(&flagM.DisableBinlogStats, "disable-binlogstats", false, "disable binlog statistics")
	cmdAddMySQL.Flags().BoolVar(&flagM.DisableProcesslist, "disable-processlist", false, "disable process state metrics")
	cmdAddMySQL.Flags().StringVar(&flagM.QuerySource, "query-source", "auto", "source of SQL queries: auto, slowlog, perfschema")

	cmdAddMySQLMetrics.Flags().StringVar(&flagM.DefaultsFile, "defaults-file", "", "path to my.cnf")
	cmdAddMySQLMetrics.Flags().StringVar(&flagM.Host, "host", "", "MySQL host")
	cmdAddMySQLMetrics.Flags().StringVar(&flagM.Port, "port", "", "MySQL port")
	cmdAddMySQLMetrics.Flags().StringVar(&flagM.User, "user", "", "MySQL username")
	cmdAddMySQLMetrics.Flags().StringVar(&flagM.Password, "password", "", "MySQL password")
	cmdAddMySQLMetrics.Flags().StringVar(&flagM.Socket, "socket", "", "MySQL socket")
	cmdAddMySQLMetrics.Flags().BoolVar(&flagM.CreateUser, "create-user", false, "create a new MySQL user")
	cmdAddMySQLMetrics.Flags().StringVar(&flagM.CreateUserPassword, "create-user-password", "", "optional password for a new MySQL user")
	cmdAddMySQLMetrics.Flags().UintVar(&flagM.MaxUserConn, "create-user-maxconn", 5, "max user connections for a new user")
	cmdAddMySQLMetrics.Flags().BoolVar(&flagM.Force, "force", false, "force to create or update MySQL user")
	cmdAddMySQLMetrics.Flags().BoolVar(&flagM.DisableTableStats, "disable-tablestats", false, "disable table statistics (for MySQL with a huge number of tables)")
	cmdAddMySQLMetrics.Flags().BoolVar(&flagM.DisableUserStats, "disable-userstats", false, "disable user statistics")
	cmdAddMySQLMetrics.Flags().BoolVar(&flagM.DisableBinlogStats, "disable-binlogstats", false, "disable binlog statistics")
	cmdAddMySQLMetrics.Flags().BoolVar(&flagM.DisableProcesslist, "disable-processlist", false, "disable process state metrics")

	cmdAddMySQLQueries.Flags().StringVar(&flagM.DefaultsFile, "defaults-file", "", "path to my.cnf")
	cmdAddMySQLQueries.Flags().StringVar(&flagM.Host, "host", "", "MySQL host")
	cmdAddMySQLQueries.Flags().StringVar(&flagM.Port, "port", "", "MySQL port")
	cmdAddMySQLQueries.Flags().StringVar(&flagM.User, "user", "", "MySQL username")
	cmdAddMySQLQueries.Flags().StringVar(&flagM.Password, "password", "", "MySQL password")
	cmdAddMySQLQueries.Flags().StringVar(&flagM.Socket, "socket", "", "MySQL socket")
	cmdAddMySQLQueries.Flags().BoolVar(&flagM.CreateUser, "create-user", false, "create a new MySQL user")
	cmdAddMySQLQueries.Flags().StringVar(&flagM.CreateUserPassword, "create-user-password", "", "optional password for a new MySQL user")
	cmdAddMySQLQueries.Flags().UintVar(&flagM.MaxUserConn, "create-user-maxconn", 5, "max user connections for a new user")
	cmdAddMySQLQueries.Flags().BoolVar(&flagM.Force, "force", false, "force to create or update MySQL user")
	cmdAddMySQLQueries.Flags().StringVar(&flagM.QuerySource, "query-source", "auto", "source of SQL queries: auto, slowlog, perfschema")

	cmdAddMongoDB.Flags().StringVar(&flagMongoURI, "uri", "mongodb://localhost:27017", "MongoDB URI")
	cmdAddMongoDB.Flags().StringVar(&flagMongoNodeType, "nodetype", "", "node type: mongod, mongos, config, arbiter")
	cmdAddMongoDB.Flags().StringVar(&flagMongoCluster, "cluster", "", "cluster name")
	cmdAddMongoDB.Flags().StringVar(&flagMongoReplSet, "replset", "", "replSet name")

	cmdAddMongoDBMetrics.Flags().StringVar(&flagMongoURI, "uri", "mongodb://localhost:27017", "MongoDB URI")
	cmdAddMongoDBMetrics.Flags().StringVar(&flagMongoNodeType, "nodetype", "", "node type: mongod, mongos, config, arbiter")
	cmdAddMongoDBMetrics.Flags().StringVar(&flagMongoCluster, "cluster", "", "cluster name")
	cmdAddMongoDBMetrics.Flags().StringVar(&flagMongoReplSet, "replset", "", "replSet name")

	cmdCheckNet.Flags().BoolVar(&flagNoEmoji, "no-emoji", false, "avoid emoji in the output")

	cmdStart.Flags().BoolVar(&flagAll, "all", false, "all monitoring services")
	cmdStop.Flags().BoolVar(&flagAll, "all", false, "all monitoring services")

	if os.Getuid() != 0 {
		fmt.Println("pmm-admin requires superuser privileges to manage system services.")
		os.Exit(1)
	}

	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
