Percona Monitoring and Management (PMM) Client

v1.0.2 unreleased 2016-07-21

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
