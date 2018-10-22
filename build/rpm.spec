%define debug_package %{nil}
Name:           pmm-client
Summary:        Percona Monitoring and Management Client
Version:        %{version}
Release:        %{release}%{?dist}
Group:          Applications/Databases
License:        AGPLv3
Vendor:         Percona LLC
URL:            https://percona.com
Source:         pmm-client-%{version}.tar.gz
BuildRoot:      %{_tmppath}/%{name}-%{version}-%{release}-root
AutoReq:        no

%description
Percona Monitoring and Management (PMM) is an open-source platform for managing and monitoring MySQL and MongoDB
performance. It is developed by Percona in collaboration with experts in the field of managed database services,
support and consulting.
PMM is a free and open-source solution that you can run in your own environment for maximum security and reliability.
It provides thorough time-based analysis for MySQL and MongoDB servers to ensure that your data works as efficiently
as possible.

%prep
%setup -q

%build

%install
%if 0%{?rhel} == 5
    install -m 0755 -d $RPM_BUILD_ROOT/usr/bin
    install -m 0755 bin/pmm-admin $RPM_BUILD_ROOT/usr/bin/
%else
    install -m 0755 -d $RPM_BUILD_ROOT/usr/sbin
    install -m 0755 bin/pmm-admin $RPM_BUILD_ROOT/usr/sbin/
%endif
install -m 0755 -d $RPM_BUILD_ROOT/usr/local/percona/pmm-client
install -m 0755 -d $RPM_BUILD_ROOT/usr/local/percona/qan-agent/bin
install -m 0755 -d $RPM_BUILD_ROOT/usr/local/percona/pmm-client/textfile-collector
install -m 0755 bin/node_exporter $RPM_BUILD_ROOT/usr/local/percona/pmm-client/
install -m 0755 bin/mysqld_exporter $RPM_BUILD_ROOT/usr/local/percona/pmm-client/
install -m 0755 bin/postgres_exporter $RPM_BUILD_ROOT/usr/local/percona/pmm-client/
install -m 0755 bin/mongodb_exporter $RPM_BUILD_ROOT/usr/local/percona/pmm-client/
install -m 0755 bin/proxysql_exporter $RPM_BUILD_ROOT/usr/local/percona/pmm-client/
install -m 0755 bin/pt-summary $RPM_BUILD_ROOT/usr/local/percona/qan-agent/bin/
install -m 0755 bin/pt-mysql-summary $RPM_BUILD_ROOT/usr/local/percona/qan-agent/bin/
install -m 0755 bin/pt-mongodb-summary $RPM_BUILD_ROOT/usr/local/percona/qan-agent/bin/
install -m 0755 bin/percona-qan-agent $RPM_BUILD_ROOT/usr/local/percona/qan-agent/bin/
install -m 0755 bin/percona-qan-agent-installer $RPM_BUILD_ROOT/usr/local/percona/qan-agent/bin/
install -m 0644 queries-mysqld.yml $RPM_BUILD_ROOT/usr/local/percona/pmm-client
install -m 0755 example.prom $RPM_BUILD_ROOT/usr/local/percona/pmm-client/textfile-collector/

%clean
rm -rf $RPM_BUILD_ROOT

%post
# upgrade
pmm-admin ping > /dev/null
if [ $? = 0 ] && [ "$1" = "2" ]; then
%if 0%{?rhel} == 6
    for file in $(find -L /etc/init.d -maxdepth 1 -name "pmm-*")
    do
        sed -i 's|^name=$(basename $0)|name=$(basename $(readlink -f $0))|' "$file"
    done
    for file in $(find -L /etc/init.d -maxdepth 1 -name "pmm-linux-metrics*")
    do
        sed -i  "s/,meminfo_numa/,meminfo_numa,textfile/" "$file"
    done
    for file in $(find -L /etc/init -maxdepth 1 -name "pmm-linux-metrics*")
    do
        sed -i  "s/,meminfo_numa/,meminfo_numa,textfile/" "$file"
    done
%else
    for file in $(find -L /etc/systemd/system -maxdepth 1 -name "pmm-*")
    do
        network_exists=$(grep -c "network.target" "$file")
        if [ $network_exists = 0 ]; then
            sed -i 's/Unit]/Unit]\nAfter=network.target\nAfter=syslog.target/' "$file"
        fi
    done
    for file in $(find -L /etc/systemd/system -maxdepth 1 -name "pmm-linux-metrics*")
    do
        sed -i  "s/,meminfo_numa/,meminfo_numa,textfile/" "$file"
    done
%endif
    pmm-admin restart --all
fi

%preun
# uninstall
if [ "$1" = "0" ]; then
    pmm-admin uninstall
fi

%postun
# uninstall
if [ "$1" = "0" ]; then
    rm -rf /usr/local/percona/pmm-client
    rm -rf /usr/local/percona/qan-agent
    echo "Uninstall complete."
fi

%files
%dir /usr/local/percona/pmm-client
%dir /usr/local/percona/pmm-client/textfile-collector
%dir /usr/local/percona/qan-agent/bin
/usr/local/percona/pmm-client/textfile-collector/*
/usr/local/percona/pmm-client/*
/usr/local/percona/qan-agent/bin/*
%if 0%{?rhel} == 5
    /usr/bin/pmm-admin
%else
    /usr/sbin/pmm-admin
%endif
