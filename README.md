# yspm

A small Linux package manager written in Go.

**yspm v0.1 — First Release**

`yspm` follows a **stable-release model**, not a rolling-release model. A repository is pinned to a numbered release. `update` refreshes metadata for that release, while `upgrade` only moves packages forward within the same release.

```text
yspm update
yspm search firefox
yspm info firefox
yspm install firefox
yspm list
yspm upgrade
yspm remove firefox
```

## Release model

`yspm v0.1` uses a numbered stable repository:

```text
repo/
└── releases/
    └── 1/
        └── index.json
```

Future stable releases can live beside it:

```text
repo/
└── releases/
    ├── 1/
    │   └── index.json
    └── 2/
        └── index.json
```

An installed system never silently jumps from release 1 to release 2. A future explicit release-upgrade command can be added later.

Within a stable release, package updates and security fixes are expected. The package maintainer controls which package versions belong to the release snapshot.

## Integrity

Every installable non-meta package is expected to provide a 64-character **SHA-256** checksum in the repository index.

`yspm` refuses to install a package without a valid SHA-256. Cached downloads are rechecked before reuse.

This keeps `update` usable even while a newly added upstream package is waiting for checksum verification.

## Package types

* `binary` — CLI tools packed as tar archives.
* `app` — GUI applications distributed as upstream tar/zip archives.
* `appimage` — Single-file AppImage applications.
* `meta` — Dependency-only packages.

## Paths

```text
~/.cache/yspm/          downloaded packages + cached index
~/.local/share/yspm/    installation data + package database
~/.local/bin/            command symlinks
```

Add `~/.local/bin` to your `PATH` if it is not already present.

## Build

```bash
go build -o yspm ./cmd/yspm
```

## Local repository testing

```bash
YSPM_REPOSITORY="$PWD/repo/releases/1/index.json" ./yspm update
YSPM_REPOSITORY="$PWD/repo/releases/1/index.json" ./yspm search firefox
```

## References

`yspm` is an independent implementation. Its package-management model and architecture were informed by studying the documentation and source code of existing Linux package managers and the Go standard library.

References:

* [Go Documentation](https://go.dev/doc/)
* [Debian APT Documentation](https://www.debian.org/doc/manuals/apt-guide/)
* [Debian APT Source](https://github.com/Debian/apt)
* [Fedora Packaging Guidelines](https://docs.fedoraproject.org/en-US/packaging-guidelines/)
* [DNF Source](https://github.com/rpm-software-management/dnf)
* [Arch Linux Pacman Documentation](https://man.archlinux.org/man/pacman.8)

These references are used for studying package-management concepts such as repositories, package metadata, dependencies, upgrades, versioning, and integrity verification. `yspm` does not copy their implementations.

## Notes

`yspm v0.1` is currently focused on **x86_64 Linux**.

The metadata model already carries `os` and `architecture`, so additional targets can be added later without changing the CLI.

This is the **first release** of yspm and is intended as a small, understandable foundation for future package-management features.
