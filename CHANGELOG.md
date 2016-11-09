Percona Monitoring and Management (PMM) Client

v1.0.7 unreleased 2016-11-09

* Added --bind-address flag to support running PMM server and client on the different networks.
  By default, this address is the same as client one. When running PMM on different networks, --client-address should be set to remote (public) address
  and --bind-address to local (private) address. This also assumes you configure NAT and port forwarding between those addresses.
* Amended output of systemv service status if run adhoc (requires re-adding services).

v1.0.6 unreleased 2016-11-04

* Fixes for "mysql:queries" service using perfschema query source:
  * do not crash when DIGEST_TEXT is NULL
  * do not iterate over all the query digests on the start
  * send query examples to Query Analytics if available (depends from workload).
* Show the actual query source for mysql:queries services by `pmm-admin list`, in case it was changed on UI.
* Added "purge" command to purge metrics data on the server.
* Updated mongodb_exporter with RocksDB support and various fixes.
* Removed --nodetype and --replset flags for mongodb:metrics, they are not needed now, --cluster flag is made optional.
  It is recommended to re-add mongodb:metrics service and then purge the existing mongodb metrics using purge command.
* Enabled monitoring of file descriptors (requires re-adding linux:metrics service).
* Improved full uninstallation when PMM server is unreachable.
* Added time drift check between server and client to `pmm-admin check-network` output.

v1.0.5 released 2016-10-14

* Added check for orphaned local and remote services.
* Added "repair" command to remove orphaned services.
* Added "proxysql:metrics" service and proxysql_exporter.
* Amended "check-network" output.
* Disallow inital client configuration with the name that is currently in use.
* Disable table stats automatically with 1000+ tables when adding "mysql:metrics" service.
* Fixes for "mysql:queries" service:
  * improved registration and detection of orphaned setup
  * pid file "" is not longer created on Amazon Linux (requires to re-add mysql:queries service)
  * correctly detect when the slow log is rotated and also perform its own rotation
  * handling MySQL using a timezone different than UTC
  * RELOAD privilige is required to flush slow log if used as a query source.

v1.0.4 released 2016-09-13

* Renamed services:
  * os > linux:metrics
  * mysql > mysql:metrics
  * queries > mysql:queries
  * mongodb > mongodb:metrics
* Added group commands:
  * "add/remove mysql" - adds linux:metrics, mysql:metrics, mysql:queries services in 1 go
  * "add/remove mongodb" - adds linux:metrics, mongodb:metrics
* Added options to support SSL and HTTP password protection on PMM server.
* Added check whether all the required binaries are installed.
* Changed behaviour of --create-user MySQL flag:
  * now pmm-admin employs a single "pmm" MySQL user, verifies if already exists and stores the generated password in the config
  * added checks whether MySQL is read-only or a replication slave
  * stored credentials are automatically picked up by pmm-admin when valid
* mysqld_exporter is replaced with custom one https://github.com/percona/mysqld_exporter
* Now pmm-admin creates 1 mysql metrics system service instead of 3 per MySQL instance.
* Do not require a name on service remove, using the client name by default.
* Added check for MongoDB connectivity when adding mongodb:metrics instance.
* Do not modify linux:metrics instance when adding mongodb:metrics one.
* Allowed to add more than one linux:metrics instance for testing purpose.
* Added consistency checks to avoid duplicate services across clients.
* Detect client address automatically.
* Disable table stats automatically with 10000+ tables.
* Improved installation process and created packages.
* Added MySQL version check to mysqld_exporter to eliminate errors on 5.5.
* Added --all flag to "remove" command.
* Added "restart" command.

v1.0.3 released 2016-08-05

* "queries" service (percona-qan-agent) did not start when using unix-systemv.
* Fixed error where removing stopped services "os", "mysql" using linux-upstart.
* Fixed password auto-detection for MySQL 5.7.
* Added --disable-userstats, --disable-binlogstats, --disable-processlist MySQL flags.
* Renamed --disable-per-table-stats to --disable-tablestats.
* Removed unnecessary flag --disable-infoschema.

v1.0.2 released 2016-07-28

* Full rewrite of pmm-admin utility:
  * more options, flexible usage and user-friendly CLI
  * eliminated intermediate percona-prom-pm process
  * now monitoring services are created dynamically via platform service manager (systemd, upstart or systemv)
  * ability to choose a custom name for instances
  * ability to choose a port, by default it is auto-selected starting 42000
  * ability to monitor multiple database instances on the same node
  * ability to check bidirectional network connectivity
  * ability to stop/start individual metric services or all at once
* Installation improvements.

v1.0.1 released 2016-06-09

* Improvements to pmm-admin and ability to set server address with the port.
* The latest versions of metrics exporters, qan-agent.
* Added mongodb_exporter.
* Added uninstall script.

v1.0.0 released 2016-04-17

* First release.
