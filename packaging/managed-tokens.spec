Name:           managed-tokens
Version:        0.1.0
Release:        1
Summary:        Utility to obtain Hashicorp vault (service) tokens from service kerberos principals and distribute them to experiment nodes

Group:          Applications/System
License:        Fermitools Software Legal Information (Modified BSD License)
URL:            TODO
Source0:        %{name}-%{version}.tar.gz

BuildRoot:      %(mktemp -ud %{_tmppath}/%{name}-%{version}-XXXXXX)
BuildArch:      x86_64

Requires:       krb5-workstation
Requires:       condor
Requires:       iputils
Requires:       rsync
Requires:       sqlite

%description
Utility to obtain Hashicorp vault (service) tokens from service kerberos principals and distribute them to experiment nodes

%prep
test ! -d %{buildroot} || {
rm -rf %{buildroot}
}

%setup -q

%build

%install

# Config file to /etc/managed-tokens
mkdir -p %{buildroot}/%{_sysconfdir}/%{name}
install -m 0774 managedTokens.yml %{buildroot}/%{_sysconfdir}/%{name}/managedTokens.yml

# Executables to /usr/bin
mkdir -p %{buildroot}/%{_bindir}
install -m 0755 refresh-uids-from-ferry %{buildroot}/%{_bindir}/refresh-uids-from-ferry
install -m 0755 run-onboarding-managed-tokens %{buildroot}/%{_bindir}/run-onboarding-managed-tokens
install -m 0755 token-push %{buildroot}/%{_bindir}/token-push

# Cron and logrotate
mkdir -p %{buildroot}/%{_sysconfdir}/cron.d
install -m 0644 %{name}.cron %{buildroot}/%{_sysconfdir}/cron.d/%{name}
mkdir -p %{buildroot}/%{_sysconfdir}/logrotate.d
install -m 0644 %{name}.logrotate %{buildroot}/%{_sysconfdir}/logrotate.d/%{name}

# Templates
mkdir -p %{buildroot}/%{_datadir}/%{name}/templates
install -m 0644 templates/*.txt  %{buildroot}/%{_datadir}/%{name}/templates


%clean
rm -rf %{buildroot}

%files
%defattr(0755, rexbatch, fife, 0774)
%config(noreplace) %{_sysconfdir}/%{name}/managedTokens.yml
%config(noreplace) %attr(0644, root, root) %{_sysconfdir}/cron.d/%{name}
%config(noreplace) %attr(0644, root, root) %{_sysconfdir}/logrotate.d/%{name}
%{_datadir}/%{name}/templates
%{_bindir}/refresh-uids-from-ferry
%{_bindir}/run-onboarding-managed-tokens
%{_bindir}/token-push

%post
# Logfiles at /var/log/managed-tokens
test -d /var/log/%{name} || {
install -d /var/log/%{name} -m 0774 -o rexbatch -g fife
}

# SQLite Database folder at /var/lib/managed-tokens
test -d %{_sharedstatedir}/%{name} || {
install -d %{_sharedstatedir}/%{name} -m 0774 -o rexbatch -g fife
}

%changelog
* Mon Aug 29 2022 Shreyas Bhat <sbhat@fnal.gov> - 0.1.0
First version of the managed tokens RPM