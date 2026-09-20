<div align="center">

<img src="https://upload.wikimedia.org/wikipedia/commons/thumb/0/05/Go_Logo_Blue.svg/1280px-Go_Logo_Blue.svg.png" alt="Go" width="220">

# yspm

**A Linux system package manager written in Go.**

[![Release](https://img.shields.io/github/v/release/Yassine-Jemi01/yspm?display_name=release&sort=semver)](https://github.com/Yassine-Jemi01/yspm/releases)
[![License](https://img.shields.io/github/license/Yassine-Jemi01/yspm)](https://github.com/Yassine-Jemi01/yspm/blob/main/LICENSE)

<img src="https://upload.wikimedia.org/wikipedia/commons/3/35/Tux.svg" alt="Tux" width="120">

</div>

---

yspm is a small Linux system package manager written in Go.

It is designed for stable-release Linux distributions and focuses on native system packages, dependency resolution, shared libraries, safe transactions, package ownership, configuration handling, lifecycle hooks, service integration, repository metadata, release management, security auditing, and multi-architecture support.

## Status

The latest tagged release is **v0.2.0**.

The `main` branch now contains the native system-package milestone. This includes the `.yspkg` package format, ABI-aware dependency handling, shared-library metadata, configuration file management, lifecycle hooks, service integration, repository generation and signing, release upgrades, vulnerability metadata, multi-architecture selection, and optional Btrfs snapshots.

This milestone is still evolving and should not yet be treated as a complete production replacement for mature Linux package managers.

## Requirements

- Go 1.23 or newer
- Git

Install Go using the [official installation instructions](https://go.dev/doc/install).

Check the installed version:

~~~bash
go version
~~~

## Install and use

System mode is the default and state-changing operations require root:

~~~bash
sudo yspm update
sudo yspm install gcc firefox
sudo yspm upgrade
sudo yspm remove gcc
~~~

Per-user mode is explicit:

~~~bash
yspm install ripgrep --user
~~~

A native package can be installed directly:

~~~bash
sudo yspm install ./hello-1.0.0-x86_64.yspkg
~~~

## Commands

~~~text
yspm install <package>...          Install packages and dependencies
yspm remove <package>...           Remove packages safely
yspm autoremove                    Remove unneeded automatic dependencies
yspm search <query>                Search the repository
yspm info <package>                Show package metadata
yspm list                          List installed packages
yspm list --upgradable             Show available upgrades
yspm list --explicit               Show explicitly installed packages
yspm depends <package>             Show the dependency tree
yspm why <package>                Explain reverse dependencies
yspm owner <path>                 Show the package that owns a path
yspm sync                         Import HardcoreLinux legacy packages
yspm explain <package>            Explain ABI and dependency metadata
yspm update                        Refresh repository metadata
yspm upgrade                       Upgrade within the pinned release
yspm release                      Show the installed release and ABI
yspm release upgrade <release>    Migrate to another stable release
yspm audit                         Check vulnerability metadata
yspm check                         Verify installed files
yspm clean                         Remove package cache
yspm history                       Show transaction history
yspm transaction <id>              Show a transaction
yspm snapshot create               Create a Btrfs snapshot
yspm snapshot list                 List snapshots
yspm snapshot restore <id>         Restore a prepared snapshot target
yspm build                         Build a native .yspkg package
yspm repo index                    Generate a repository index
yspm repo sign                     Sign repository metadata
yspm keygen                        Generate repository signing keys
~~~

## Transactions

Package installation, removal, and upgrades are handled as transactions.

~~~text
resolve
  ↓
download
  ↓
verify SHA-256
  ↓
stage
  ↓
validate ownership/conflicts
  ↓
preinstall hook
  ↓
commit
  ↓
postinstall hook
  ↓
services and triggers
  ↓
save package database
~~~

The transaction system tracks package state, file ownership, manifests, configuration hashes, ABI information, services, hooks, and transaction history.

State-changing operations use a package lock to prevent concurrent database changes.

Failed filesystem commits can be rolled back by the transaction machinery.

Background transactions are available:

~~~bash
sudo yspm install firefox --background
sudo yspm upgrade --background
yspm transaction <id>
~~~

## Native package format

Native system packages use the `.yspkg` format.

~~~text
metadata.json
scripts/
  preinstall
  postinstall
  preremove
  postremove
root/
  usr/
  etc/
  var/
~~~

The `root/` tree is installed relative to the selected target root.

Package metadata can contain:

- package and release information
- ABI and architecture
- normal dependencies
- shared-library requirements and providers
- conflicts, provides, and replacements
- file manifests and checksums
- configuration files
- services
- triggers
- vulnerability records

Build a native package with:

~~~bash
yspm build \
  --root ./root \
  --output ./hello-1.0.0-x86_64.yspkg \
  --name hello \
  --version 1.0.0 \
  --abi yspm-abi-1 \
  --arch x86_64
~~~

## Dependencies and shared libraries

yspm supports version constraints, alternative dependencies, conflicts, provides, replaces, recommends, and suggests.

Native packages can also declare shared-library dependencies:

~~~text
shared_requires:
  - libssl.so.3

shared_provides:
  - libssl.so.3
~~~

When available, `yspm build` can inspect ELF binaries with `readelf` and discover NEEDED and SONAME entries.

This allows packages to depend on system shared libraries instead of carrying a private copy of every library.

## ABI

A repository can define an ABI identifier, for example:

~~~text
yspm-abi-1
~~~

Native packages can declare the ABI they were built for.

A package with an incompatible ABI is rejected. This provides a compatibility boundary for stable distribution releases.

## Configuration files

Packages can declare configuration files under `config_files`.

When a package upgrade encounters a modified configuration file, yspm preserves the current file and stores the new distribution version as:

~~~text
file.yspm-dist
~~~

Modified configuration files can also be preserved during removal as:

~~~text
file.yspm-save
~~~

## Hooks, services, and triggers

Native packages may contain:

~~~text
preinstall
postinstall
preremove
postremove
~~~

Hooks receive:

~~~text
YSPM_ROOT
YSPM_PACKAGE
YSPM_VERSION
~~~

Package metadata can declare services for supported init systems:

- systemd
- OpenRC
- runit
- HardcoreLinux initctl

Supported package triggers include:

- ldconfig
- desktop-database
- font-cache
- icon-cache

Hooks are trusted package code and should only be accepted from trusted repositories.

## Repository management

Generate a repository index from `.yspkg` packages:

~~~bash
yspm repo index \
  --dir ./packages \
  --output ./releases/1/index.json \
  --base-url https://repo.example/yspm/releases/1 \
  --release 1 \
  --abi yspm-abi-1
~~~

Generate signing keys:

~~~bash
yspm keygen repo.pub repo.key
~~~

Sign repository metadata:

~~~bash
yspm repo sign index.json repo.key index.json.sig
~~~

Clients can require detached Ed25519 repository signatures with:

~~~bash
YSPM_REQUIRE_SIGNATURES=1
~~~

## Stable releases

yspm separates the application version from the distribution repository release.

For example:

~~~text
application: v0.2.0
repository: 1
ABI: yspm-abi-1
~~~

Normal upgrades stay inside the current repository release:

~~~bash
sudo yspm upgrade
~~~

Switching to another stable release is explicit:

~~~bash
sudo yspm release upgrade 2
~~~

The target release must provide compatible repository metadata and ABI information.

## Multi-architecture

Supported normalized architecture names include:

~~~text
x86_64
aarch64
i386
armv7
~~~

Select a target architecture explicitly:

~~~bash
sudo yspm install package --arch aarch64
~~~

Foreign architectures can be configured with `YSPM_FOREIGN_ARCHS`.

## Security audit

Package metadata can contain vulnerability records:

~~~json
{
  "id": "CVE-YYYY-NNNN",
  "severity": "high",
  "fixed_version": "2.0.0",
  "url": "https://example.org/advisory"
}
~~~

Run:

~~~bash
yspm audit
~~~

The audit is metadata-driven and does not claim that the repository contains every upstream vulnerability.

## System snapshots

On Btrfs systems, yspm can create read-only snapshots:

~~~bash
sudo yspm snapshot create
sudo yspm snapshot list
~~~

Automatic pre-transaction snapshots can be requested with:

~~~bash
sudo yspm upgrade --snapshot
~~~

Live restoration of the running root filesystem is intentionally refused. Root restoration must be performed from a prepared rescue environment or another unmounted target.

## HardcoreLinux compatibility

yspm has a dedicated compatibility mode for the current HardcoreLinux package ecosystem.

~~~bash
export YSPM_REPOSITORY_FORMAT=hardcore
sudo yspm update
sudo yspm install sway
~~~

The mode reads HardcoreLinux `repo/list.sha256` metadata and gzip-compressed tar packages containing:

~~~text
/etc/installer/<package>/
  install
  pkinfo
  uninstall
~~~

Existing installations can be imported without reinstalling anything:

~~~bash
sudo yspm sync
sudo yspm list
sudo yspm owner /usr/bin/sway
~~~

Legacy `pkinfo` dependencies, SHA-256 archive verification, file ownership, configuration preservation, and the legacy `install`/`uninstall` lifecycle are integrated with yspm transactions. Native `.yspkg` packages remain supported separately.

## Compatibility

The repository still supports the existing package path for older archives such as:

~~~text
tar.gz
tar.xz
ZIP
AppImage
~~~

Native `.yspkg` packages provide the system-package path while the older package types remain available for compatibility.

## Development

Run tests:

~~~bash
go test ./...
~~~

Build yspm:

~~~bash
go build ./cmd/yspm
~~~

See the documentation for more details:

- [Wiki](./docs/WIKI.md)
- [Architecture](./docs/ARCHITECTURE.md)
- [Native System Packages](./docs/SYSTEM_PACKAGES.md)

## License

GNU General Public License v3.0.
