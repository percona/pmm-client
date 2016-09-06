Percona Monitoring and Management (PMM) Client

v1.0.4 unreleased 2016-09-02

* Renamed services:
  * os > linux:metrics
  * mysql > mysql:metrics
  * queries > mysql:queries
  * mongodb > mongodb:metrics
* Added group commands:
  * "add/remove mysql" - adds linux:metrics, mysql:metrics, mysql:queries services in 1 go
  * "add/remove mongodb" - adds linux:metrics, mongodb:metrics
* Added SSL and HTTP password support with PMM Server. Currently, only pmm-admin communication with server is protected on client-side.
* Added check whether the required binaries of exporters are installed.
* Changed behaviour of --create-user MySQL flag:
  * now pmm-admin employs a single `pmm` MySQL user, verifies if already exists and stores the generated password in the config
  * added checks whether MySQL is read-only or a replication slave
  * stored credentials are automatically picked up by pmm-admin when are valid
* mysqld_exporter is replaced with custom 3-in-1 mysqld_exporter.
* pmm-admin creates 1 mysql metrics system service instead of 3 per MySQL instance.
* Do not require a name on service remove, using the client name by default.
* Added check for MongoDB connectivity when adding mongodb:metrics instance.
* Do not modify linux:metrics instance when adding mongodb:metrics one.
* Allowed to add more than one linux:metrics instance for testing purpose.
* Added consistency checks to avoid duplicate services across clients.
* Detect client address automatically.
* Disable table stats automatically with 10000+ tables.
* Improved installation process: install script from the tarball just copies binaries.
* Added MySQL version check to mysqld_exporter to eliminate errors on 5.5.
* Added --all flag to "remove" command.

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
