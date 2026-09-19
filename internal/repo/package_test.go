package repo

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Yassine-Jemi01/yspm/internal/model"
)

func TestYSPKGBuildReadAndExtract(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "usr", "bin"), 0o755); err != nil { t.Fatal(err) }
	src := filepath.Join(root, "usr", "bin", "hello")
	if err := os.WriteFile(src, []byte("#!/bin/sh\necho hello\n"), 0o755); err != nil { t.Fatal(err) }
	if err := os.WriteFile(filepath.Join(root, "etc.conf"), []byte("initial\n"), 0o644); err != nil { t.Fatal(err) }

	p := model.Package{
		Name:         "hello",
		Version:      "1.0.0",
		Description:  "test package",
		OS:           "linux",
		Architecture: "x86_64",
		License:      "GPL-3.0",
		ABI:          "yspm-abi-1",
		Format:       PackageFormat,
	}
	out := filepath.Join(t.TempDir(), "hello.yspkg")
	if err := BuildPackage(root, out, p, ""); err != nil { t.Fatal(err) }

	got, err := ReadPackageMetadata(out)
	if err != nil { t.Fatal(err) }
	if got.Name != "hello" || got.Version != "1.0.0" || got.Format != PackageFormat { t.Fatalf("unexpected metadata: %+v", got) }

	files, err := ListPackageFiles(out)
	if err != nil { t.Fatal(err) }
	found := false
	for _, f := range files {
		if f.Path == "usr/bin/hello" && f.Type == "file" { found = true }
	}
	if !found { t.Fatalf("hello file missing from manifest: %+v", files) }

	dest := filepath.Join(t.TempDir(), "extract")
	if err := ExtractPackage(out, dest); err != nil { t.Fatal(err) }
	b, err := os.ReadFile(filepath.Join(dest, "usr", "bin", "hello"))
	if err != nil { t.Fatal(err) }
	if string(b) != "#!/bin/sh\necho hello\n" { t.Fatalf("unexpected extracted content: %q", b) }
}
