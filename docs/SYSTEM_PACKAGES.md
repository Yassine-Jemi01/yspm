# Native System Packages

## Package format

The .yspkg format is a gzip-compressed tar archive containing:

```text
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
  ...
```

The root/ tree is installed relative to the selected target root.

## ABI

Every native package can declare an ABI identifier. A repository can also declare its ABI.

A package whose ABI differs from the repository ABI is rejected. This lets a stable release share one system ABI while newer distro releases move to a new ABI.

## Shared libraries

Native packages can declare:

- shared_provides for SONAMEs provided by a package
- shared_requires for SONAMEs required by binaries

yspm build can discover ELF NEEDED and SONAME entries using readelf when available.

The dependency resolver treats shared_requires as dependency expressions, so a package can depend on a shared-object provider instead of bundling a private copy.

## Configuration files

Packages can list config_files.

During upgrade:

- unchanged user configuration can be replaced
- modified configuration is preserved
- the new distribution version is written as *.yspm-dist

During removal, modified configuration is preserved as *.yspm-save.

## Lifecycle hooks

Native packages may contain:

```text
preinstall
postinstall
preremove
postremove
```

Hooks receive:

```text
YSPM_ROOT
YSPM_PACKAGE
YSPM_VERSION
```

Hooks are trusted package code and should only be accepted from trusted repositories.

## Services

Package metadata can declare services:

```json
{
  "services": [
    {
      "name": "example.service",
      "enable": true,
      "start": true
    }
  ]
}
```

yspm detects systemd, OpenRC, runit, and HardcoreLinux initctl integration.

## Triggers

Supported metadata refresh triggers include:

- ldconfig
- desktop-database
- font-cache
- icon-cache

Unknown triggers are rejected.

## Architecture

Packages declare their architecture using normalized names such as:

```text
x86_64
aarch64
i386
armv7
```

A user can select a target architecture with --arch. Foreign architectures can be enabled with YSPM_FOREIGN_ARCHS.

## Repository

Build an index:

```bash
yspm repo index \
  --dir ./packages \
  --output ./releases/1/index.json \
  --base-url https://repo.example/yspm/releases/1 \
  --release 1 \
  --abi yspm-abi-1
```

Sign it:

```bash
yspm keygen repo.pub repo.key
yspm repo sign index.json repo.key index.json.sig
```

Clients can require signatures through YSPM_REQUIRE_SIGNATURES=1.

## Release upgrades

A release remains pinned by default:

```bash
yspm upgrade
```

Only the current release is considered.

An explicit release migration is:

```bash
yspm release upgrade 2
```

The target repository must provide a stable index and compatible ABI metadata.

## Security audit

Packages can carry vulnerability metadata with an identifier, severity and fixed version.

```bash
yspm audit
```

The audit is metadata-driven and does not claim that an upstream CVE database is complete.

## System snapshots

On Btrfs:

```bash
sudo yspm snapshot create
sudo yspm snapshot list
sudo yspm snapshot restore <id>
```

Live / restoration is intentionally refused. Restoring a root snapshot must be performed from a prepared rescue or unmounted environment.

Automatic pre-transaction snapshots can be requested with:

```bash
sudo yspm install gcc --snapshot
sudo yspm upgrade --snapshot
```

## User mode

System mode is the default:

```bash
sudo yspm install hello
```

Per-user mode is available explicitly:

```bash
yspm install hello --user
```

System mode writes to FHS-style locations under the selected YSPM_ROOT and stores package state under /var/lib/yspm. User mode stores state under the user's home.
