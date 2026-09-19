# Applying the yspm upgrade

This archive contains the upgraded source tree for `cmd/yspm`, `internal`, and the new architecture documentation.

From the existing yspm repository:

```bash
cd ~/Documents/yspm
cp -a cmd cmd.backup
cp -a internal internal.backup
cp -a README.md README.md.v0.1.backup
```

Extract the archive over the repository, then test:

```bash
go test ./...
go vet ./...
go build -o yspm ./cmd/yspm
./yspm --help
```

The existing `repo/releases/1/index.json`, `LICENSE`, and `.gitignore` are intentionally left out of the replacement archive so your current repository metadata and licensing files are preserved.

For a safe local test, use a temporary HOME and override `YSPM_REPOSITORY` to a local index. Do not run the test with `sudo`.
