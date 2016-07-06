Percona Monitoring and Management (PMM) Client

v1.0.2 unreleased 2016-07-13

* Full rewrite of pmm-admin utility:
  * better options, more flexible usage and user friendly interface
  * eliminated intermediate percona-prom-pm process
  * now monitoring services are created dynamically via platform service manager (systemd, upstart, systemv, whatever is available first)
  * ability to name instances
  * ability to choose the port, by default it is auto-selected starting 42000
  * ability to monitor multiple instances of MySQL and MongoDB on the same node
  * added checks for connectivity verification

v1.0.1 released 2016-06-09

* Improvements to pmm-admin and ability to set server address with the port
* The latest versions of metrics exporters, qan-agent
* Added mongodb_exporter
* Added uninstall script

v1.0.0 released 2016-04-17

* First release
