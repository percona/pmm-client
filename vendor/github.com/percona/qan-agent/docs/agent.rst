=============================
Percona Query Analytics Agent
=============================

Percona Query Analytics Agent is the client-side tool for collecting and sending MySQL query metrics to a Percona Datastore. It uses either the MySQL slow log or Performance Schema. It is a static binary with no external dependencies.

Install
=======

Requirements
------------

* Linux OS (Debian, Ubuntu, CentOS, Red Hat, etc.)
* Root/sudo access
* Outbound HTTP and websocket connections to a Percona Datastore (port 9001 by default)
* MySQL 5.1 or later for slow log
* MySQL 5.6.9 or later for Performance Schema

Note: the agent must run as root because the MySQL slow log file has restricted permissions.

Quick
-----

As root on a server running MySQL locally:

.. code-block:: bash

    ./install <datastore hostname>

The only required argument is the Percona Datastore hostname (with optional ``:PORT`` suffix) to use.

For the quick install to work, the installer must able to auto-detect and connect to the local MySQL instance with grant privileges to create a MySQL user for the agent. This usually requires that your MySQL credentials are set in ``~/.my.cnf`` and MySQL is running on its standard port (3306) or has a detectable socket file. Often times, the following command is needed:

.. code-block:: bash

    ./install -user=root -pass=root <datastore hostname>

Where ``root`` is an actual user and password with grant privileges. (Note: those are single '-' not double.)

Run ``./install help`` to see all command line options.

Logging
=======

The agent has two logging systems: online and file. Online logging sends all log entries (except debug) to the API for viewing in the QAN app. File refers to STDOUT and STDERR. The default log level is "warning", so errors and warnings are written to STDERR. If the log level is increased to "info", info-level log entries are writen to STDOUT. The log level (set in ``config/log.conf``) affects only file logging.

Below are two examples that show which log levels are written and where (indicated by *) based on the log config:

**Default**

No ``config/log.conf`` file or it contains ``{"Level":"warning","Offline":"false"}``:

==========  === ======  ======
Level       API STDOUT  STDERR
==========  === ======  ======
Debug
Info        *
Warning     *           *
Error       *           *
Fatal       *           *
==========  === ======  ======

**Traditional**

If ``config/log.conf`` contains ``{"Level":"info","Offline":"true"}``:

==========  === ======  ======
Level       API STDOUT  STDERR
==========  === ======  ======
Debug
Info            *
Warning                 *
Error                   *
Fatal                   *
==========  === ======  ======

Configure
=========

Percona Query Analytics Agent is designed to be configured through the Percona Query Analytics Web App, but when necessary its config files can be changed manually. When editing config files, note that

- The ``config/`` dir is chmod 700 owner=root
- All config files are strict JSON (i.e. no trailing commas)

  - Use ``python -mjson.tool <file>`` to validate and pretty-print a JSON file

- Required variables are **bold**
- Variables with a default value are optional
- Default "(none)" means disabled, not used unless a value is set
- Agent must be restarted after changing a config file manually

  - Like editing ``/etc/my.cnf`` and restarting MySQL

- Agent dynamically reconfigures itself (i.e. no restart) when a config is changed via API

  - Like ``SET GLOBAL <dynamic var>`` but agent writes changes to local config file so change remains in effect if agent is restarted

- Boolean values are strings (fuzzy bools): "true", "yes", and "on" are true; any other value is false

  - Go is strongly typed and its JSON package only allows empty values to be omitted. There is no empty value for bool, but there is for string: "". For numeric values, 0 is considered "not set" and the default values is used

agent.conf
----------

This is the only required config file.

=============== ==========  =========================================
Variable        Default     Purpose
=============== ==========  =========================================
**UUID**                    ID of the agent instance

**ApiHostname**             ``host:port`` of datastore (no ``http(s)/ws(s)://`` prefix)

Keepalive       76          How often to ping API on cmd websocket

PidFile         agent.pid   PID file (relative to basedir)

Links                       API links sent by API; do not edit, but safe to remove (agent gets/sets them when it connects to API)
=============== ==========  =========================================

data.conf
---------

This config file is optional.

==============  =========== =========================================
Variable        Default     Purpose
==============  =========== =========================================
SendInterval    63          How often to send data to the datastore
Encoding        gzip        "gzip" or "none"
Blackhole       false       Send data to ``/dev/null``, not the datastore
Limits          (see below) Limits size of data spool
==============  =========== =========================================

`Limits` is a subdocument with these fields:

==============  ==========          =========================================
Variable        Default             Purpose
==============  ==========          =========================================
MaxAge          86400 (1 day)       Data files older than this are purged
MaxSize         104857600 (100 MiB) When the spool is larger than this, the oldest files are purged
MaxFiles        1000                When the spool has more files than this, the oldest files are purged
==============  ==========          =========================================

log.conf
--------

This config file is optional.

==============  ==========  =========================================
Variable        Default     Purpose
==============  ==========  =========================================
Level           warning     Minimum log level for STDOUT/STDERR
Offline         false       Do not log to API
==============  ==========  =========================================

qan-UUID.conf
-------------

``UUID`` is the UUID of a MySQL instance, like ``qan-04af149283e449885922a3e60e298310.conf``. If no such config files exist, then the agent is not configured for any MySQL instances.

=================   ==========  =========================================
Variable            Default     Purpose
=================   ==========  =========================================
**UUID**                        MySQL instance UUID to which this QAN config applies; should match the file suffix

CollectFrom         slowlog     "slowlog" or "perfschema"

Start               (varies)    List of MySQL queries to execute to configure the server

Stop                (varies)    List of MySQL queries to execute to un-configure the server

Interval            60          How often to collect and aggregate data

WorkerRunTime       55          Max runtime for each worker per interval

MaxSlowLogSize      1073741824  Rotate slow log when it becomes this large (bytes)

RemoveOldSlowLogs   true        Remove slow log after rotating if true

ExampleQueries      true        Send an example for each query

ReportLimit         200         Send only top N queries sorted by total query time, per interval
=================   ==========  =========================================
