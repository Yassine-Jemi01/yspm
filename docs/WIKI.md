# yspm Wiki

![Go](https://upload.wikimedia.org/wikipedia/commons/thumb/0/05/Go_Logo_Blue.svg/1280px-Go_Logo_Blue.svg.png)

![Tux](https://upload.wikimedia.org/wikipedia/commons/thumb/3/35/Tux.svg/800px-Tux.svg.png)

## Home

yspm is a small Linux package manager written in Go. The project focuses on dependency-aware installation, safe transactions, package state, and stable repository releases.

### Current release

`v0.2.0`

### Commands

```bash
yspm update
yspm search firefox
yspm info firefox
yspm install firefox
yspm list
yspm upgrade
yspm remove firefox
```

Install several packages as one planned transaction:

```bash
yspm install firefox brave git vscode
```

Background operations:

```bash
yspm install firefox vscode --background
yspm upgrade --background
yspm transaction <id>
```

## Dependency resolution

Dependencies are resolved before installed state is changed. The resolver supports version constraints, alternatives, conflicts, provides/replaces, recommends, and suggests.

```text
openssl>=3.0
foo=1.2
lib<2.0
editor|editor-bin
```

## Transactions

```text
resolve
  ↓
download in parallel
  ↓
verify SHA-256
  ↓
stage
  ↓
validate
  ↓
commit
  ↓
database
```

The package-state lock prevents concurrent database changes. Staging and rollback protect against partial transactions.

## Package database

yspm tracks installed versions, dependencies, install reason, file ownership, checksums, timestamps, application integration, and transaction history.

## Stable releases

```text
repo/
└── releases/
    ├── 1/
    │   └── index.json
    └── 2/
        └── index.json
```

`upgrade` stays within the active stable repository release. Moving to another release is intended to be explicit.

## Package types

```text
binary     CLI archive
app        GUI application archive
appimage   AppImage
meta       dependency-only package
system     reserved for future native distro packages
```

## Security

Non-meta packages require SHA-256 checksums. Optional detached Ed25519 signatures can authenticate repository metadata.

## Architecture

See [`docs/ARCHITECTURE.md`](../docs/ARCHITECTURE.md) for the component layout, resolver, transaction engine, database, background worker, security model, and native package direction.

## Native package direction

The current repository is mainly based on upstream archives and AppImages. A future distro layer is intended to use native packages with manifests and shared system-library dependencies.

## Image sources

- Go logo: [Wikimedia Commons](https://commons.wikimedia.org/wiki/File:Go_Logo_Blue.svg)
- Tux: [Wikimedia Commons](https://commons.wikimedia.org/wiki/File:Tux.svg)
- Fedora DNF example: [Fedora Brasil](https://fedorabr.org/discussion/496/tutorial-atualizando-o-fedora-38-e-39-para-o-fedora-40)