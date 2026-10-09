package repo

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

type testTarMember struct {
	name     string
	typeflag byte
	linkname string
	data     string
	mode     int64
}

func writeTestTar(t *testing.T, destination string, members []testTarMember) {
	t.Helper()
	file, err := os.Create(destination)
	if err != nil {
		t.Fatal(err)
	}
	writer := tar.NewWriter(file)
	for _, member := range members {
		mode := member.mode
		if mode == 0 {
			mode = 0o644
		}
		header := &tar.Header{
			Name:     member.name,
			Typeflag: member.typeflag,
			Linkname: member.linkname,
			Mode:     mode,
		}
		if member.typeflag == tar.TypeReg {
			header.Size = int64(len(member.data))
		}
		if err := writer.WriteHeader(header); err != nil {
			_ = writer.Close()
			_ = file.Close()
			t.Fatalf("write tar header %q: %v", member.name, err)
		}
		if member.typeflag == tar.TypeReg {
			if _, err := writer.Write([]byte(member.data)); err != nil {
				_ = writer.Close()
				_ = file.Close()
				t.Fatalf("write tar file %q: %v", member.name, err)
			}
		}
	}
	if err := writer.Close(); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestExtractTarRejectsSymlinkEscape(t *testing.T) {
	base := t.TempDir()
	archivePath := filepath.Join(base, "malicious.tar")
	destination := filepath.Join(base, "stage")
	writeTestTar(t, archivePath, []testTarMember{
		{name: "pivot", typeflag: tar.TypeSymlink, linkname: "../../outside"},
		{name: "pivot/pwned", typeflag: tar.TypeReg, data: "not outside"},
	})

	if err := ExtractArchive(archivePath, "tar", destination); err == nil {
		t.Fatal("expected archive with escaping symlink to be rejected")
	}
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		t.Fatalf("destination should not be modified before validation; Lstat error = %v", err)
	}
	if _, err := os.Lstat(filepath.Join(base, "outside")); !os.IsNotExist(err) {
		t.Fatalf("outside path unexpectedly exists; Lstat error = %v", err)
	}
}

func TestExtractTarRejectsFilesNestedUnderSymlink(t *testing.T) {
	base := t.TempDir()
	archivePath := filepath.Join(base, "malicious.tar")
	destination := filepath.Join(base, "stage")
	writeTestTar(t, archivePath, []testTarMember{
		{name: "pivot", typeflag: tar.TypeSymlink, linkname: "inside"},
		{name: "pivot/pwned", typeflag: tar.TypeReg, data: "must not be written"},
	})

	if err := ExtractArchive(archivePath, "tar", destination); err == nil {
		t.Fatal("expected member nested beneath a symlink to be rejected")
	}
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		t.Fatalf("destination should not be created before validation; Lstat error = %v", err)
	}
}

func TestExtractTarRejectsEscapingHardlink(t *testing.T) {
	base := t.TempDir()
	archivePath := filepath.Join(base, "malicious.tar")
	destination := filepath.Join(base, "stage")
	writeTestTar(t, archivePath, []testTarMember{
		{name: "hard", typeflag: tar.TypeLink, linkname: "../../outside"},
	})

	if err := ExtractArchive(archivePath, "tar", destination); err == nil {
		t.Fatal("expected escaping hardlink to be rejected")
	}
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		t.Fatalf("destination should not be created before validation; Lstat error = %v", err)
	}
}

func TestExtractTarAllowsSafeSymlinksAndHardlinks(t *testing.T) {
	base := t.TempDir()
	archivePath := filepath.Join(base, "valid.tar")
	destination := filepath.Join(base, "stage")
	writeTestTar(t, archivePath, []testTarMember{
		{name: "usr", typeflag: tar.TypeDir, mode: 0o755},
		{name: "usr/bin", typeflag: tar.TypeDir, mode: 0o755},
		{name: "usr/bin/tool", typeflag: tar.TypeReg, data: "safe tool", mode: 0o755},
		{name: "usr/bin/tool-copy", typeflag: tar.TypeLink, linkname: "usr/bin/tool"},
		{name: "bin", typeflag: tar.TypeDir, mode: 0o755},
		{name: "bin/tool", typeflag: tar.TypeSymlink, linkname: "../usr/bin/tool"},
	})

	if err := ExtractArchive(archivePath, "tar", destination); err != nil {
		t.Fatalf("extract safe archive: %v", err)
	}
	for _, name := range []string{"usr/bin/tool", "usr/bin/tool-copy", "bin/tool"} {
		data, err := os.ReadFile(filepath.Join(destination, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if string(data) != "safe tool" {
			t.Fatalf("%s content = %q, want %q", name, data, "safe tool")
		}
	}
	linkTarget, err := os.Readlink(filepath.Join(destination, "bin/tool"))
	if err != nil {
		t.Fatalf("read safe symlink: %v", err)
	}
	if linkTarget != "../usr/bin/tool" {
		t.Fatalf("symlink target = %q, want %q", linkTarget, "../usr/bin/tool")
	}
}

func TestExtractTarAutoDetectsGzipForLegacyTarFormat(t *testing.T) {
	var raw bytes.Buffer
	writer := tar.NewWriter(&raw)
	data := []byte("legacy package")
	if err := writer.WriteHeader(&tar.Header{
		Name: "etc/installer/demo/pkinfo",
		Mode: 0o644,
		Size: int64(len(data)),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	base := t.TempDir()
	archivePath := filepath.Join(base, "legacy.tar")
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	gzipWriter := gzip.NewWriter(file)
	if _, err := gzipWriter.Write(raw.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	destination := filepath.Join(base, "stage")
	if err := ExtractArchive(archivePath, "tar", destination); err != nil {
		t.Fatalf("auto-detect gzip-compressed legacy tar: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(destination, "etc/installer/demo/pkinfo"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(data) {
		t.Fatalf("extracted data = %q, want %q", got, data)
	}
}
