%global debug_package %{nil}
%undefine _disable_source_fetch
%global upstream_version %{?version_override}%{!?version_override:0.3.0}
%global github_owner %{?github_owner_override}%{!?github_owner_override:caracal-os}
%global github_repo %{?github_repo_override}%{!?github_repo_override:caracal-setup}
%global source_tag %{?source_tag_override}%{!?source_tag_override:v%{upstream_version}}
%global source_dir_name %{github_repo}-%{upstream_version}

Name:           caracal-setup
Version:        %{upstream_version}
Release:        %{?release_override}%{!?release_override:1}%{?dist}
Summary:        First-launch setup wizard for Caracal OS
License:        MIT
URL:            https://github.com/%{github_owner}/%{github_repo}
Source0:        %{url}/archive/refs/tags/%{source_tag}.tar.gz#/%{name}-%{version}.tar.gz

BuildRequires:  gcc
BuildRequires:  golang >= 1.25
BuildRequires:  glib2-devel
BuildRequires:  gtk3-devel
BuildRequires:  pkgconf-pkg-config
BuildRequires:  webkit2gtk4.1-devel

%description
caracal-setup provides a Wails-based first-launch wizard for Caracal OS.
It can optionally update the current username and password, launch the
mandatory ujust first-run flow, and finish with a reboot action.

%prep
%autosetup -n %{source_dir_name}

%build
mkdir -p build
export GOFLAGS="-buildmode=pie -trimpath -mod=vendor"
go build -tags="desktop,production,webkit2_41" -ldflags="-s -w" -o build/caracal-setup .

%check
export GOFLAGS="-mod=vendor"
go test ./...

%install
install -d %{buildroot}%{_bindir}
install -d %{buildroot}%{_prefix}/lib/caracal-setup
install -d %{buildroot}%{_datadir}/caracal-setup
install -d %{buildroot}%{_datadir}/pixmaps

install -pm0755 build/caracal-setup %{buildroot}%{_bindir}/caracal-setup
cp -a scripts %{buildroot}%{_prefix}/lib/caracal-setup/
cp -a assets %{buildroot}%{_datadir}/caracal-setup/

install -pm0644 logo.txt %{buildroot}%{_datadir}/caracal-setup/logo.txt
install -pm0644 build/appicon.png %{buildroot}%{_datadir}/pixmaps/caracal-setup.png
install -Dpm0644 packaging/caracal-setup.desktop %{buildroot}%{_datadir}/applications/caracal-setup.desktop

%files
%license LICENSE
%doc README.md
%{_bindir}/caracal-setup
%{_prefix}/lib/caracal-setup/scripts/*
%{_datadir}/caracal-setup/logo.txt
%{_datadir}/caracal-setup/assets/images/*
%{_datadir}/pixmaps/caracal-setup.png
%{_datadir}/applications/caracal-setup.desktop

%changelog
* Wed May 06 2026 Atumia <atumia@users.noreply.github.com> - %{version}-%{release}
- Build caracal-setup as a packaged Wails RPM
