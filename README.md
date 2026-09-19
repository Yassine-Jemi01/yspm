<div align="center">

<img src="https://upload.wikimedia.org/wikipedia/commons/thumb/0/05/Go_Logo_Blue.svg/1280px-Go_Logo_Blue.svg.png" alt="Go" width="220">

# yspm

**A Linux system package manager written in Go.**

[![Release](https://img.shields.io/github/v/release/Yassine-Jemi01/yspm?display_name=release&sort=semver)](https://github.com/Yassine-Jemi01/yspm/releases)
[![License](https://img.shields.io/github/license/Yassine-Jemi01/yspm)](https://github.com/Yassine-Jemi01/yspm/blob/main/LICENSE)

<img src="https://upload.wikimedia.org/wikipedia/commons/3/35/Tux.svg" alt="Tux" width="120">

</div>

---

yspm is designed for stable-release Linux distributions. The development branch adds native system packages, ABI-aware dependency resolution, shared-library metadata, lifecycle hooks, configuration handling, service integration, repository generation and signing, release upgrades, vulnerability metadata, multi-architecture selection, and optional Btrfs snapshots.

## Status

The main branch contains the latest stable release.

The system-package-manager branch contains the next development milestone and is not presented as a stable distro replacement yet.

## Requirements

- Go 1.23 or newer
- Git

Install Go using the [official installation instructions](https://go.dev/doc/install).

On Debian/Ubuntu:

~~~bash
sudo apt update
sudo apt install golang-go
~~~

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
yspm install <package>...           Install packages and dependencies
yspm remove <package>...            Remove packages safely
yspm autoremove                     Remove unneeded automatic dependencies
yspm search <query>                 Search the repository
yspm info <package>                 Show package metadata
yspm list --upgradable              Show available upgrades
yspm depends <package>              Show dependency tree
yspm why <package>                 Explain reverse dependencies
yspm explain <package>             Explain ABI and dependency metadata
yspm update                         Refresh repository metadata
yspm upgrade                        Upgrade within the pinned release
yspm release                       Show installed release and ABI
yspm release upgrade <release>     Migrate to another stable release
yspm audit                           Check vulnerability metadata
yspm check                           Verify installed files
yspm clean                           Remove package cache
yspm history                         Show transaction history
yspm transaction <id>               Show a transaction
yspm snapshot create               Create a Btrfs snapshot
yspm snapshot list                 List snapshots
yspm snapshot restore <id>         Restore a prepared snapshot target
yspm build                          Build a native .yspkg package
yspm repo index                    Generate a repository index
yspm repo sign                     Sign repository metadata
yspm keygen                        Generate repository signing keys
~~~

## Native package format

Native packages use .yspkg.

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

The metadata can contain:

- stable release ABI
- package and shared-library dependencies
- file manifests and checksums
- configuration files
- services
- triggers
- replacements and conflicts
- vulnerability records
- target OS and architecture

## Dependency and ABI model

yspm resolves normal package dependencies and shared-library requirements before changing installed state.

Native packages can declare shared-library SONAMEs:

~~~text
shared_requires:
  - libssl.so.3

shared_provides:
  - libssl.so.3
~~~

The build command can discover ELF NEEDED and SONAME entries with readelf.

A repository can pin one ABI identifier. Packages with a mismatched ABI are rejected.

This is intended to reuse shared libraries instead of privately bundling a copy inside every application.

## Transactions

Install and upgrade work as a transaction:

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

The database tracks ownership, manifests, configuration hashes, ABI, services, hooks, and transaction history.

A failed file commit restores the filesystem changes made by the transaction.

## Hooks, services, and triggers

Native packages can contain preinstall, postinstall, preremove, and postremove scripts.

Hooks receive YSPM_ROOT, YSPM_PACKAGE, and YSPM_VERSION.

Service metadata supports:

- systemd
- OpenRC
- runit
- HardcoreLinux initctl

Package triggers can refresh shared-library caches, the desktop database, fonts, and icon data.

Hooks are trusted package code and should only come from trusted repositories.

## Configuration files

Packages can declare files under config_files.

When a user-modified configuration would be replaced during an upgrade, yspm keeps the current file and writes the distribution copy as:

~~~text
file.yspm-dist
~~~

A modified configuration removed with a package can be preserved as:

~~~text
file.yspm-save
~~~

## Repositories

Generate an index from native packages:

~~~bash
yspm repo index \
  --dir ./packages \
  --output ./releases/1/index.json \
  --base-url https://repo.example/yspm/releases/1 \
  --release 1 \
  --abi yspm-abi-1
~~~

Sign the index:

~~~bash
yspm keygen repo.pub repo.key
yspm repo sign index.json repo.key index.json.sig
~~~

Clients can require detached Ed25519 signatures with:

~~~bash
YSPM_REQUIRE_SIGNATURES=1
YSPM_REPOSITORY_SIGNATURE=https://repo.example/index.json.sig
YSPM_REPOSITORY_PUBLIC_KEY=<public-key>
~~~

## Stable releases

The yspm application version is separate from the repository release.

~~~text
application: v0.2.x
repository: 1
~~~

Normal upgrade stays inside the current release.

An explicit migration is:

~~~bash
sudo yspm release upgrade 2
~~~

The target repository must provide stable metadata and a compatible ABI.

## Multi-architecture

Packages use normalized architecture names such as x86_64, aarch64, i386, and armv7.

Select a target architecture with:

~~~bash
sudo yspm install package --arch aarch64
~~~

Foreign architectures can be enabled with YSPM_FOREIGN_ARCHS.

## Security audit

Package metadata can contain vulnerability records:

~~~json
{
  "id": "CVE-YYYY-NNNN",
  "severity": "high",
  "fixed_version": "2.0.0"
}
~~~

Run:

~~~bash
yspm audit
~~~

The audit is metadata-driven and does not claim that the repository contains every upstream vulnerability.

## System snapshots

On Btrfs:

~~~bash
sudo yspm snapshot create
sudo yspm snapshot list
~~~

Automatic pre-transaction snapshots can be requested:

~~~bash
sudo yspm upgrade --snapshot
~~~

Live root restoration is intentionally refused. Root restoration must be performed from a prepared rescue or unmounted target.

## Compatibility with existing upstream packages

The current stable repository still contains upstream archives such as tarballs, ZIP files, and AppImages.

The development manager keeps a legacy integration path so the existing repository remains usable while native .yspkg packages are introduced.

## Development

~~~bash
go test ./...
go build ./cmd/yspm
~~~

See [Native System Packages](./docs/SYSTEM_PACKAGES.md) for the package and integration contract.

## License

GNU General Public License v3.0.
