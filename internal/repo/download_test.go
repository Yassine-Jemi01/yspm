package repo

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func TestDownloadWithLimitRejectsOversizedSource(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.bin")
	destination := filepath.Join(dir, "cache", "download.bin")
	if err := os.WriteFile(source, []byte("0123456789"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := DownloadWithLimit(source, destination, 5); err == nil {
		t.Fatal("expected oversized download to be rejected")
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("oversized download left a destination behind: %v", err)
	}
}

func TestDownloadPackageChecksDeclaredSize(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.bin")
	destination := filepath.Join(dir, "cache", "download.bin")
	if err := os.WriteFile(source, []byte("short"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := DownloadPackage(source, destination, 8); err == nil {
		t.Fatal("expected archive size mismatch to be rejected")
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("size-mismatched download left a destination behind: %v", err)
	}
}

func TestExtractZipRejectsTraversalBeforeWriting(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "bad.zip")
	f, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	entry, err := w.Create("../outside.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte("outside")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(dir, "stage")
	if err := ExtractArchive(archive, "zip", destination); err == nil {
		t.Fatal("expected ZIP traversal path to be rejected")
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("destination should not be created before validation: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "outside.txt")); !os.IsNotExist(err) {
		t.Fatalf("ZIP extraction wrote outside destination: %v", err)
	}
}
