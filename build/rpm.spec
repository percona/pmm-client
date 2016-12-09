%define debug_package %{nil}
Name:           pmm-client
Summary:        Percona Monitoring and Management Client
Version:        %{version}
Release:        %{release}
Group:          Applications/Databases
License:        AGPLv3
Vendor:         Percona LLC
URL:            https://percona.com
Source:         pmm-client-%{version}.tar.gz
BuildRoot:      %{_tmppath}/%{name}-%{version}-%{release}-root
BuildArch:      x86_64
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
install -m 0755 bin/node_exporter $RPM_BUILD_ROOT/usr/local/percona/pmm-client/
install -m 0755 bin/mysqld_exporter $RPM_BUILD_ROOT/usr/local/percona/pmm-client/
install -m 0755 bin/mongodb_exporter $RPM_BUILD_ROOT/usr/local/percona/pmm-client/
install -m 0755 bin/proxysql_exporter $RPM_BUILD_ROOT/usr/local/percona/pmm-client/
install -m 0755 bin/percona-qan-agent $RPM_BUILD_ROOT/usr/local/percona/qan-agent/bin/
install -m 0755 bin/percona-qan-agent-installer $RPM_BUILD_ROOT/usr/local/percona/qan-agent/bin/
if [ "$1" -ge 2 ]; then
    echo "NOTE:  This upgrade requires that the PMM monitoring services be re-added to provide credentials."
fi

%clean
rm -rf $RPM_BUILD_ROOT

%post
if [ "$1" -ge 2 ]; then
    echo "NOTE:  This upgrade requires that the PMM monitoring services be re-added to provide credentials."
fi
# upgrade
pmm-admin ping > /dev/null
if [ $? = 0 ] && [ "$1" = "2" ]; then
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
%dir /usr/local/percona/qan-agent/bin
/usr/local/percona/pmm-client/*
/usr/local/percona/qan-agent/bin/*
%if 0%{?rhel} == 5
    /usr/bin/pmm-admin
%else
    /usr/sbin/pmm-admin
%endif
