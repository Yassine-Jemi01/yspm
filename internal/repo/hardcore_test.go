package repo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHardcorePKInfoAndManifest(t *testing.T) {
	root := t.TempDir()
	installer := filepath.Join(root, "etc", "installer", "hello")
	if err := os.MkdirAll(installer, 0o755); err != nil { t.Fatal(err) }
	if err := os.WriteFile(filepath.Join(installer, "pkinfo"), []byte("hello 1.2.3 musl zlib\n"), 0o644); err != nil { t.Fatal(err) }
	if err := os.WriteFile(filepath.Join(installer, "install"), []byte("#!/bin/sh\n"), 0o755); err != nil { t.Fatal(err) }
	if err := os.WriteFile(filepath.Join(installer, "uninstall"), []byte("rm /usr/bin/hello\nrmdir /usr/bin\n"), 0o755); err != nil { t.Fatal(err) }
	if err := os.MkdirAll(filepath.Join(root, "usr", "bin"), 0o755); err != nil { t.Fatal(err) }
	if err := os.WriteFile(filepath.Join(root, "usr", "bin", "hello"), []byte("hello\n"), 0o755); err != nil { t.Fatal(err) }

	data, err := ReadHardcoreInstaller(installer)
	if err != nil { t.Fatal(err) }
	if data.Package.Name != "hello" || data.Package.Version != "1.2.3" { t.Fatalf("unexpected package: %+v", data.Package) }
	if len(data.Package.Dependencies) != 2 { t.Fatalf("dependencies: %+v", data.Package.Dependencies) }

	manifest, err := HardcoreInstalledManifest(root, "hello")
	if err != nil { t.Fatal(err) }
	found := false
	for _, e := range manifest {
		if e.Path == "usr/bin/hello" && e.Type == "file" { found = true }
	}
	if !found { t.Fatalf("manifest missing installed file: %+v", manifest) }
}

func TestFetchHardcoreIndexLocal(t *testing.T) {
	root := t.TempDir()
	sum := "0000000000000000000000000000000000000000000000000000000000000000"
	if err := os.WriteFile(filepath.Join(root, "list.sha256"), []byte(sum+"\thello.tar\thello\n"), 0o644); err != nil { t.Fatal(err) }
	idx, err := FetchHardcoreIndex(root)
	if err != nil { t.Fatal(err) }
	if idx.Channel != "stable" || len(idx.Packages) != 1 { t.Fatalf("unexpected index: %+v", idx) }
	if idx.Packages[0].Format != HardcorePackageFormat || idx.Packages[0].Name != "hello" { t.Fatalf("unexpected package: %+v", idx.Packages[0]) }
}
