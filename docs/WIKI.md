# yspm Wiki

![Go](https://upload.wikimedia.org/wikipedia/commons/0/05/Go_Logo_Blue.svg)

![Tux](https://upload.wikimedia.org/wikipedia/commons/3/35/Tux.svg)

## Current release

`v0.2.0`

yspm is a small Linux package manager written in Go, focused on dependency-aware installation, safe transactions, package state, and stable repository releases.

## Commands

```bash
yspm update
yspm search firefox
yspm info firefox
yspm install firefox
yspm list
yspm upgrade
yspm remove firefox
```

Install multiple packages in one transaction:

```bash
yspm install firefox brave git vscode
```

Background transactions:

```bash
yspm install firefox vscode --background
yspm upgrade --background
yspm transaction <id>
```

## v0.2.0 features

- Dependency resolution with version constraints
- Alternative dependencies
- Conflicts, provides, and replaces
- Explicit and automatic package tracking
- `autoremove`
- Parallel downloads
- SHA-256 verification and cache re-validation
- Staging and transaction rollback
- File ownership and conflict detection
- Executable and desktop integration
- Background transactions with IDs and logs
- Upgrade detection within the current stable release
- Transaction history and local consistency checks
- Optional Ed25519 repository metadata verification

## Transaction flow

```text
resolve
  ↓
download in parallel
  ↓
verify
  ↓
stage
  ↓
validate
  ↓
commit
  ↓
database
```

The package-state lock prevents concurrent database changes.

## Stable releases

```text
repo/
└── releases/
    ├── 1/
    │   └── index.json
    └── 2/
        └── index.json
```

`upgrade` remains inside the active stable repository release. Switching to another release is intended to be explicit.

## Package types

```text
binary     CLI archive
app        GUI application archive
appimage   AppImage
meta       dependency-only package
system     reserved for future native distro packages
```

## Security

Installable non-meta packages require a valid SHA-256 checksum. Optional detached Ed25519 signatures can authenticate repository metadata.

## Architecture

See [docs/ARCHITECTURE.md](../docs/ARCHITECTURE.md) for the resolver, transaction engine, package database, background worker, security model, and native package direction.

## References

- [Go documentation](https://go.dev/doc/)
- [Debian Reference — Package Management](https://www.debian.org/doc/manuals/debian-reference/ch02)
- [Debian package basics](https://www.debian.org/doc/manuals/debian-faq/pkg-basics.en.html)
- [Debian Dependency Hell](https://wiki.debian.org/DependencyHell)
- [Fedora Packaging Guidelines](https://docs.fedoraproject.org/en-US/packaging-guidelines/)
- [DNF5](https://github.com/rpm-software-management/dnf5)

## Image sources

- Go logo: [Wikimedia Commons](https://commons.wikimedia.org/wiki/File:Go_Logo_Blue.svg)
- Tux: [Wikimedia Commons](https://commons.wikimedia.org/wiki/File:Tux.svg)
