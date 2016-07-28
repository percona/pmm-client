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
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Usage()
			os.Exit(1)
		},
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			// NOTE: this function pre-runs with every command or sub-command except "config".
			// This flag will not run anywhere else than on rootCmd as this flag is not persistent one
			// and we want it only here without any config checks.
			if flagVersion {
				fmt.Println(pmm.VERSION)
				os.Exit(0)
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
			admin.SetAPI()

			// Check if server is alive.
			// Does not apply to "pmm-admin" and "pmm-admin info" commands.
			if !admin.ServerAlive() && cmd.Name() != "pmm-admin" && cmd.Name() != "info" {
				fmt.Printf("Unable to connect to PMM server by address: %s\n\n", admin.Config.ServerAddress)
				fmt.Println(`Check if the configured address is correct.
If server container is running on non-default port, ensure it was specified along with the address.
You may also check the firewall settings.`)
				os.Exit(1)
			}
		},
	}

	cmdAdd = &cobra.Command{
		Use:   "add",
		Short: "Add instance to monitoring.",
		Long:  "This command is used to add an instance to the monitoring.",
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			cmd.Root().PersistentPreRun(cmd.Root(), args)
			admin.ServiceName = admin.Config.ClientName
			admin.ServicePort = flagServicePort
			if len(args) > 0 {
				admin.ServiceName = args[0]
			}
		},
	}
	cmdAddOS = &cobra.Command{
		Use:   "os [name]",
		Short: "Add this OS to metrics monitoring.",
		Long: `This command adds this system to metrics monitoring.

There could be only one instance of OS being monitored for this system.

[name] is an optional argument, by default it is set to the client name of this PMM client.
		`,
		Run: func(cmd *cobra.Command, args []string) {
			if err := admin.AddOS(); err != nil {
				fmt.Println("Error adding OS:", err)
				os.Exit(1)
			}
			fmt.Println("OK, now monitoring this OS.")
		},
	}
	cmdAddMySQL = &cobra.Command{
		Use:   "mysql [name]",
		Short: "Add MySQL instance to metrics monitoring.",
		Long: `This command adds the given MySQL instance to metrics monitoring.

When adding a MySQL instance, you may specify additional options, the rest will be auto-detected.
If you want to create a new user to be used for metrics collecting, provide --create-user option. pmm-admin will create
a new user 'pmm-mysql@' automatically using the given (auto-detected) MySQL credentials for granting purpose.

[name] is an optional argument, by default it is set to the client name of this PMM client.
		`,
		Example: `  pmm-admin add mysql --password abc123
  pmm-admin add mysql --password abc123 --host 192.168.1.2 --create-user
  pmm-admin add mysql --user rdsuser --password abc123 --host my-rds.1234567890.us-east-1.rds.amazonaws.com my-rds`,
		Run: func(cmd *cobra.Command, args []string) {
			info, err := pmm.DetectMySQL("mysql", flagM)
			if err != nil {
				fmt.Println(err)
				os.Exit(1)
			}
			if err := admin.AddMySQL(info, flagM); err != nil {
				fmt.Println("Error adding MySQL:", err)
				os.Exit(1)
			}
			fmt.Println("OK, now monitoring MySQL using DSN", info["safe_dsn"])
		},
	}
	cmdAddQueries = &cobra.Command{
		Use:   "queries [name]",
		Short: "Add MySQL instance to Query Analytics.",
		Long: `This command adds the given MySQL instance to Query Analytics.

When adding a MySQL instance, you may specify additional options, the rest will be auto-detected.
If you want to create a new user to be used for query collecting, provide --create-user option. pmm-admin will create
a new user 'pmm-queries@' automatically using the given (auto-detected) MySQL credentials for granting purpose.

[name] is an optional argument, by default it is set to the client name of this PMM client.
		`,
		Example: `  pmm-admin add queries --password abc123
  pmm-admin add queries --password abc123 --host 192.168.1.2 --create-user
  pmm-admin add queries --user rdsuser --password abc123 --host my-rds.1234567890.us-east-1.rds.amazonaws.com my-rds`,
		Run: func(cmd *cobra.Command, args []string) {
			// Check --query-source flag.
			if flagM.QuerySource != "auto" && flagM.QuerySource != "slowlog" && flagM.QuerySource != "perfschema" {
				fmt.Println("Flag --query-source can take the following values: auto, slowlog, perfschema.")
				os.Exit(1)
			}
			info, err := pmm.DetectMySQL("queries", flagM)
			if err != nil {
				fmt.Println(err)
				os.Exit(1)
			}
			if err := admin.AddQueries(info); err != nil {
				fmt.Println("Error adding queries:", err)
				os.Exit(1)
			}
			fmt.Println("OK, now monitoring MySQL queries from", info["query_source"], "using DSN",
				info["safe_dsn"])
		},
	}
	cmdAddMongoDB = &cobra.Command{
		Use:   "mongodb [name]",
		Short: "Add MongoDB instance to metrics monitoring.",
		Long: `This command adds the given MongoDB instance to metrics monitoring.

When adding a MongoDB instance, you may provide --uri if the default one does not work for you.
Use additional options to specify MongoDB node type, cluster, replSet etc.

IMPORTANT: adding MongoDB instance to the monitoring with the existing OS one will rename the latter to match
the name of MongoDB instance and also sets the same options such as node type, cluster, replSet if provided.

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
			if err := admin.AddMongoDB(flagMongoURI, flagMongoNodeType, flagMongoReplSet, flagMongoCluster); err != nil {
				fmt.Println("Error adding MongoDB:", err)
				os.Exit(1)
			}
			fmt.Println("OK, now monitoring MongoDB using URI", pmm.SanitizeDSN(flagMongoURI))
		},
	}

	cmdRemove = &cobra.Command{
		Use:     "remove",
		Aliases: []string{"rm"},
		Short:   "Remove instance from monitoring.",
		Long:    "This command is used to remove an instance from the monitoring.",
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			cmd.Root().PersistentPreRun(cmd.Root(), args)
			if len(args) == 0 {
				fmt.Println("No name specified.\n")
				cmd.Usage()
				os.Exit(1)
			}
		},
	}
	cmdRemoveOS = &cobra.Command{
		Use:   "os NAME",
		Short: "Remove this OS from metrics monitoring.",
		Long:  "This command removes OS instance specified by NAME from metrics monitoring.",
		Run: func(cmd *cobra.Command, args []string) {
			name := args[0]
			if err := admin.RemoveOS(name); err != nil {
				fmt.Printf("Error removing OS %s: %s\n", name, err)
				os.Exit(1)
			}
			fmt.Printf("OK, removed OS %s from monitoring.\n", name)
		},
	}
	cmdRemoveMySQL = &cobra.Command{
		Use:   "mysql NAME",
		Short: "Remove MySQL instance from metrics monitoring.",
		Long:  "This command removes MySQL instance specified by NAME from metrics monitoring.",
		Run: func(cmd *cobra.Command, args []string) {
			name := args[0]
			if err := admin.RemoveMySQL(name); err != nil {
				fmt.Printf("Error removing MySQL %s: %s\n", name, err)
				os.Exit(1)
			}
			fmt.Printf("OK, removed MySQL %s from monitoring.\n", name)
		},
	}
	cmdRemoveQueries = &cobra.Command{
		Use:   "queries NAME",
		Short: "Remove MySQL instance from Query Analytics.",
		Long:  "This command removes MySQL instance specified by NAME from Query Analytics.",
		Run: func(cmd *cobra.Command, args []string) {
			name := args[0]
			if err := admin.RemoveQueries(name); err != nil {
				fmt.Printf("Error removing queries %s: %s\n", name, err)
				os.Exit(1)
			}
			fmt.Printf("OK, removed MySQL queries %s from monitoring.\n", name)
		},
	}
	cmdRemoveMongoDB = &cobra.Command{
		Use:   "mongodb NAME",
		Short: "Remove MongoDB instance from metrics monitoring.",
		Long:  "This command removes MongoDB instance specified by NAME from metrics monitoring.",
		Run: func(cmd *cobra.Command, args []string) {
			name := args[0]
			if err := admin.RemoveMongoDB(name); err != nil {
				fmt.Printf("Error removing MongoDB %s: %s\n", name, err)
				os.Exit(1)
			}
			fmt.Printf("OK, removed MongoDB %s from monitoring.\n", name)
		},
	}

	cmdList = &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List monitored instances for this system.",
		Long:    "This command displays the list monitored instances and their details.",
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
			cmd.Root().PersistentPreRun(cmd.Root(), args)
			admin.PrintInfo()
		},
	}

	cmdConfig = &cobra.Command{
		Use:   "config",
		Short: "Configure PMM Client.",
		Long:  "This command configures pmm-admin to communicate with PMM server.",
		Run: func(cmd *cobra.Command, args []string) {
			if err := admin.SetConfig(flagServerAddr, flagClientAddr, flagClientName); err != nil {
				fmt.Printf("Error configuring PMM client: %s\n", err)
				os.Exit(1)
			}
			fmt.Println("OK, PMM server is", admin.Config.ServerAddress)
		},
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			// Cancel root's PersistentPreRun as we do not require config file to exist here.
			// If the config does not exist, we will init an empty and write on Run.
			if err := admin.LoadConfig(flagConfigFile); err != nil {
				fmt.Printf("Error reading %s: %s\n", flagConfigFile, err)
				os.Exit(1)
			}
		},
	}

	cmdCheckNet = &cobra.Command{
		Use:   "check-network",
		Short: "Check network connectivity between client and server.",
		Long: `This command runs the tests against PMM server to verify a bi-directional network connectivity.

* Client > Server
Under this section you will find whether Consul, Query Analytics and Prometheus API are alive.
Also there is a connection performance test results with PMM server displayed.

* Server > Client
Here you will see the status of individual Prometheus endpoints and whether it can scrape metrics from this system.
Note, even this client can reach the server successfully it does not mean Prometheus is able to scrape from exporters.

In case, some of the endpoints are in problem state, please check if the corresponding service is running ("pmm-admin list").
If all endpoints are down here and "pmm-admin list" shows all services are up,
please check the firewall settings whether this system allows incoming connections by address:port in question.`,
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Root().PersistentPreRun(cmd.Root(), args)
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
			cmd.Root().PersistentPreRun(cmd.Root(), args)
			// It's all good if PersistentPreRun didn't fail.
			fmt.Println("OK")
		},
	}

	cmdStart = &cobra.Command{
		Use:   "start METRIC NAME",
		Short: "Start metric service by type and name.",
		Long:  "This command starts the corresponding system service or all.",
		Example: `  pmm-admin start os db01.vm
  pmm-admin start mysql db01.vm
  pmm-admin start queries db01.vm
  pmm-admin start mongodb db01.vm
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
			metric := args[0]
			name := args[1]
			if metric != "os" && metric != "mysql" && metric != "queries" && metric != "mongodb" {
				fmt.Println("METRIC argument can take the following values: os, mysql, queries, mongodb.\n")
				cmd.Usage()
				os.Exit(1)
			}

			if err := admin.StartStopMonitoring("start", metric, name); err != nil {
				fmt.Printf("Error starting %s metric service for %s: %s\n", metric, name, err)
				os.Exit(1)
			}
			fmt.Printf("OK, started %s metric service for %s.\n", metric, name)
		},
	}

	cmdStop = &cobra.Command{
		Use:   "stop METRIC NAME",
		Short: "Stop metric service by type and name.",
		Long:  "This command stops the corresponding system service or all.",
		Example: `  pmm-admin stop os db01.vm
  pmm-admin stop mysql db01.vm
  pmm-admin stop queries db01.vm
  pmm-admin stop mongodb db01.vm
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
			metric := args[0]
			name := args[1]
			if metric != "os" && metric != "mysql" && metric != "queries" && metric != "mongodb" {
				fmt.Println("METRIC argument can take the following values: os, mysql, queries, mongodb.\n")
				cmd.Usage()
				os.Exit(1)
			}

			if err := admin.StartStopMonitoring("stop", metric, name); err != nil {
				fmt.Printf("Error stopping %s metric service for %s: %s\n", metric, name, err)
				os.Exit(1)
			}
			fmt.Printf("OK, stopped %s metric service for %s.\n", metric, name)
		},
	}

	flagConfigFile, flagServerAddr, flagClientAddr, flagClientName      string
	flagVersion, flagNoEmoji, flagAll                                   bool
	flagServicePort                                                     uint
	flagMongoURI, flagMongoNodeType, flagMongoReplSet, flagMongoCluster string

	flagM pmm.MySQLFlags
)

func main() {
	// Setup commands and flags.
	cobra.EnableCommandSorting = false
	rootCmd.AddCommand(cmdAdd, cmdRemove, cmdList, cmdConfig, cmdInfo, cmdCheckNet, cmdPing, cmdStart, cmdStop)
	cmdAdd.AddCommand(cmdAddOS, cmdAddMySQL, cmdAddQueries, cmdAddMongoDB)
	cmdRemove.AddCommand(cmdRemoveOS, cmdRemoveMySQL, cmdRemoveQueries, cmdRemoveMongoDB)

	rootCmd.PersistentFlags().StringVarP(&flagConfigFile, "config-file", "c", pmm.ConfigFile, "PMM config file")
	rootCmd.Flags().BoolVarP(&flagVersion, "version", "v", false, "show version")

	cmdConfig.Flags().StringVar(&flagServerAddr, "server-addr", "", "PMM server address")
	cmdConfig.Flags().StringVar(&flagClientAddr, "client-addr", "", "Client address")
	cmdConfig.Flags().StringVar(&flagClientName, "client-name", "", "Client name (node identifier on Consul)")

	cmdAdd.PersistentFlags().UintVar(&flagServicePort, "service-port", 0, "service port")

	cmdAddMySQL.Flags().StringVar(&flagM.DefaultsFile, "defaults-file", "", "path to my.cnf")
	cmdAddMySQL.Flags().StringVar(&flagM.Host, "host", "", "MySQL host")
	cmdAddMySQL.Flags().StringVar(&flagM.Port, "port", "", "MySQL port")
	cmdAddMySQL.Flags().StringVar(&flagM.User, "user", "", "MySQL username")
	cmdAddMySQL.Flags().StringVar(&flagM.Password, "password", "", "MySQL password")
	cmdAddMySQL.Flags().StringVar(&flagM.Socket, "socket", "", "MySQL socket")
	cmdAddMySQL.Flags().BoolVar(&flagM.CreateUser, "create-user", false, "create a new MySQL user")
	cmdAddMySQL.Flags().BoolVar(&flagM.OldPasswords, "old-passwords", false, "use old passwords for a new user")
	cmdAddMySQL.Flags().UintVar(&flagM.MaxUserConn, "user-connections", 5, "max user connections for a new user")
	cmdAddMySQL.Flags().BoolVar(&flagM.DisableInfoSchema, "disable-infoschema", false, "disable all metrics from information_schema tables")
	cmdAddMySQL.Flags().BoolVar(&flagM.DisablePerTableStats, "disable-per-table-stats", false, "disable per table metrics (for MySQL with a huge number of tables)")

	cmdAddQueries.Flags().StringVar(&flagM.DefaultsFile, "defaults-file", "", "path to my.cnf")
	cmdAddQueries.Flags().StringVar(&flagM.Host, "host", "", "MySQL host")
	cmdAddQueries.Flags().StringVar(&flagM.Port, "port", "", "MySQL port")
	cmdAddQueries.Flags().StringVar(&flagM.User, "user", "", "MySQL username")
	cmdAddQueries.Flags().StringVar(&flagM.Password, "password", "", "MySQL password")
	cmdAddQueries.Flags().StringVar(&flagM.Socket, "socket", "", "MySQL socket")
	cmdAddQueries.Flags().BoolVar(&flagM.CreateUser, "create-user", false, "create a new MySQL user")
	cmdAddQueries.Flags().BoolVar(&flagM.OldPasswords, "old-passwords", false, "use old passwords for a new user")
	cmdAddQueries.Flags().UintVar(&flagM.MaxUserConn, "max-user-connections", 5, "max user connections for a new user")
	cmdAddQueries.Flags().StringVar(&flagM.QuerySource, "query-source", "auto", "source of SQL queries: auto, slowlog, perfschema")

	cmdAddMongoDB.Flags().StringVar(&flagMongoURI, "uri", "mongodb://localhost:27017", "MongoDB URI")
	cmdAddMongoDB.Flags().StringVar(&flagMongoNodeType, "nodetype", "", "node type: mongod, mongos, config, arbiter")
	cmdAddMongoDB.Flags().StringVar(&flagMongoCluster, "cluster", "", "cluster name")
	cmdAddMongoDB.Flags().StringVar(&flagMongoReplSet, "replset", "", "replSet name")

	cmdCheckNet.Flags().BoolVar(&flagNoEmoji, "no-emoji", false, "avoid emoji in the output")

	cmdStart.Flags().BoolVar(&flagAll, "all", false, "all metric services")
	cmdStop.Flags().BoolVar(&flagAll, "all", false, "all metric services")

	if os.Getuid() != 0 {
		fmt.Println("pmm-admin requires superuser privileges to manage system services.")
		os.Exit(1)
	}

	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
