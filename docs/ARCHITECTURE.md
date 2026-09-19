# yspm architecture

## Core layers

```text
cmd/yspm
   │
   ▼
internal/manager
   ├── repository access
   ├── dependency resolver
   ├── transaction planner
   ├── download/staging pipeline
   ├── install/remove operations
   └── transaction history/background worker
        │
        ├── internal/repo
        ├── internal/store
        ├── internal/config
        └── internal/model
```

## Dependency resolution

The resolver builds the union of all requested package requirements before downloading anything. Every requirement is represented as a package name plus an optional version constraint.

Examples:

```text
curl
openssl>=3.0
editor|editor-bin
```

The resolver chooses the highest repository version satisfying every constraint. Dependency cycles and unresolved requirements stop the transaction before files are changed.

## Transaction safety

Install and upgrade operations use:

```text
plan → download → verify → stage → validate → commit → database
```

The package database is written atomically. Files are installed from a staging directory and previous versions are retained until the database write succeeds. If a commit or database write fails, filesystem changes are restored.

## State database

`database.json` stores:

- active stable release
- installed package versions
- package dependencies
- explicit/automatic installation reason
- executable and desktop integration paths
- installed file lists
- checksums
- installation timestamps
- transaction history

The file is intentionally JSON for now so the implementation remains inspectable while the project is small.

## Background mode

A background transaction receives an ID and a log file. The worker process uses the same transaction engine as foreground operations, so there is only one installation implementation.

Only one transaction can hold the package-state lock at a time.

## Native distro packages

The current repository mostly contains upstream archive/AppImage packages. Native distro packages are a later layer where an archive contains a filesystem payload plus a signed manifest.

That package format should eventually make shared libraries first-class packages rather than copying private libraries into each application bundle. The existing dependency resolver and transaction engine are intended to become the foundation for that model.
