# yspm

A small Linux package manager written in Go.

`yspm` is designed around a stable-release repository model rather than a rolling-release model. The package manager resolves dependencies before changing the installed state, downloads packages concurrently, verifies checksums, stages changes, and commits them as a transaction.

## What is implemented

- Multi-package transactions: `yspm install firefox brave git`
- Dependency resolution with version constraints: `foo>=1.2`, `foo=1.2`, `foo<2.0`
- Alternative dependencies: `foo|bar`
- Dependency inspection: `depends`, `why`, `explain`
- Package relationships: `conflicts`, `provides`, `replaces`, `recommends`, `suggests`
- Explicit vs automatically installed packages
- `autoremove` for orphaned automatic dependencies
- Parallel downloads with a bounded worker pool
- SHA-256 verification and cache re-validation
- Atomic staging and rollback when a transaction fails
- File ownership tracking and conflict detection
- Executable symlink creation in `~/.local/bin`
- Desktop entry generation for GUI/AppImage packages
- Transaction lock to prevent concurrent database changes
- Background transactions with IDs, logs, `history`, and `transaction <id>`
- Upgrade detection and stable-release pinning
- Automatic refresh of repository metadata before `upgrade`
- Local consistency checks with `check`
- Cache cleanup with `clean`
- Optional detached Ed25519 repository signature verification

## Commands

```text
yspm install <package>...          Install packages and dependencies
yspm remove <package>...          Remove packages safely
yspm autoremove                    Remove unneeded auto-installed dependencies
yspm search <query>                Search the repository
yspm info <package>               Show package information
yspm list                           List installed packages
yspm list --upgradable             Show packages with available upgrades
yspm list --explicit               Show explicitly installed packages
yspm depends <package>             Show the dependency tree
yspm why <package>                Show why an installed package is needed
yspm explain <package>             Explain dependency metadata and resolution
yspm update                        Refresh the pinned repository index
yspm upgrade                       Upgrade within the current stable release
yspm clean                         Remove cached package archives
yspm check                         Check local package database consistency
yspm history                       Show transaction history
yspm transaction <id>              Show transaction status
yspm release                       Show the pinned stable release
```

Long-running operations can run in the background:

```bash
yspm install firefox vscode --background
yspm upgrade --background
yspm transaction <id>
```

## Transaction model

`yspm` follows this order for install/upgrade operations:

```text
resolve dependencies
        ↓
download packages in parallel
        ↓
verify SHA-256 checksums
        ↓
extract into staging
        ↓
validate package/file conflicts
        ↓
commit the transaction
        ↓
write the package database
```

The transaction lock prevents concurrent writes to the package state. A failed commit restores files that were changed during the transaction.

## Stable releases

A repository is pinned to a numbered release:

```text
repo/
└── releases/
    └── 1/
        └── index.json
```

A future release can exist beside it:

```text
repo/
└── releases/
    ├── 1/
    │   └── index.json
    └── 2/
        └── index.json
```

An installed release never silently becomes another release. `upgrade` only considers newer package versions inside the current release.

## Package metadata

The repository index supports fields for:

- name, version, revision
- release, OS, architecture
- dependencies and version constraints
- recommends and suggests
- conflicts, provides, replaces
- checksum, size, URL
- package kind and archive format
- executable entry and command name
- GUI desktop metadata
- license, maintainer, homepage

For non-`meta` packages, a valid 64-character SHA-256 is required before installation.

## Package types

- `binary` — command-line software distributed as an archive
- `app` — GUI applications distributed as an archive
- `appimage` — single-file AppImage applications
- `meta` — dependency-only packages
- `system` — reserved for future native distro packages installed into the filesystem root

The current official repository is still focused on upstream application/archive packages. A real distro base will eventually need native system packages built against the distro's own stable ABI so shared libraries can be reused instead of bundled privately.

## Paths

Default user installation paths:

```text
~/.cache/yspm/
~/.local/share/yspm/
~/.local/bin/
~/.local/share/applications/
```

The following environment variables make isolated testing possible:

```text
YSPM_REPOSITORY
YSPM_DATA_DIR
YSPM_CACHE_DIR
YSPM_BIN_DIR
YSPM_APPLICATIONS_DIR
```

## Build and test

```bash
go test ./...
go build -o yspm ./cmd/yspm
./yspm --help
```

For isolated repository testing, point `YSPM_REPOSITORY` to a local `index.json` and use a temporary `HOME` or dedicated `YSPM_*_DIR` paths.

## Repository security

SHA-256 protects against corrupted or unexpectedly changed package files, but a checksum alone does not authenticate the repository owner. `yspm` therefore also supports optional detached Ed25519 signature verification for the repository index through:

```text
YSPM_REQUIRE_SIGNATURES=1
YSPM_REPOSITORY_SIGNATURE=<signature URL or path>
YSPM_REPOSITORY_PUBLIC_KEY=<hex or base64 Ed25519 public key>
```

A production distro repository should publish signed metadata and have a key rotation/revocation policy.

## References

`yspm` is an independent implementation. The project studies package-management concepts from established systems and Go's standard library rather than copying their implementations.

- Go documentation: https://go.dev/doc/
- Debian APT documentation: https://www.debian.org/doc/manuals/debian-reference/ch02
- Debian package dependency documentation: https://www.debian.org/doc/manuals/debian-faq/pkg-basics.en.html
- Debian dependency-hell notes: https://wiki.debian.org/DependencyHell
- Fedora Packaging Guidelines: https://docs.fedoraproject.org/en-US/packaging-guidelines/
- DNF source: https://github.com/rpm-software-management/dnf5
- Pacman manual: https://man.archlinux.org/man/pacman.8

## License

GNU General Public License v3.0.
