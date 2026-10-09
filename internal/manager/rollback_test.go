package manager

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTransactionRollbackRestoresBackedUpFile(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "etc", "demo.conf")
	backup := filepath.Join(root, "staging", "backup", "demo.conf")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(backup), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backup, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}

	rb := &transactionRollback{entries: []rollbackEntry{{target: target, backup: backup}}}
	if err := rb.rollback(); err != nil {
		t.Fatalf("rollback transaction: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "original" {
		t.Fatalf("restored content = %q, want %q", got, "original")
	}
	if _, err := os.Lstat(backup); !os.IsNotExist(err) {
		t.Fatalf("backup should have been moved back to target, stat error = %v", err)
	}
}

func TestTransactionRollbackRemovesNewlyCreatedFile(t *testing.T) {
	target := filepath.Join(t.TempDir(), "usr", "bin", "new-command")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("new"), 0o755); err != nil {
		t.Fatal(err)
	}

	rb := &transactionRollback{entries: []rollbackEntry{{target: target}}}
	if err := rb.rollback(); err != nil {
		t.Fatalf("rollback transaction: %v", err)
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Fatalf("new file survived rollback: %v", err)
	}
}

func TestTransactionFinalizeRemovesBackups(t *testing.T) {
	backup := filepath.Join(t.TempDir(), "backup", "previous.conf")
	if err := os.MkdirAll(filepath.Dir(backup), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backup, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	rb := &transactionRollback{entries: []rollbackEntry{{target: "/unused-in-test", backup: backup}}}
	if err := rb.finalize(); err != nil {
		t.Fatalf("finalize transaction: %v", err)
	}
	if _, err := os.Lstat(backup); !os.IsNotExist(err) {
		t.Fatalf("rollback backup was not removed: %v", err)
	}
}

func TestCopyNodeRecursivelyCopiesFilesAndSymlinks(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	destination := filepath.Join(root, "destination")
	nested := filepath.Join(source, "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "data.txt"), []byte("recursive copy"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("data.txt", filepath.Join(nested, "data-link")); err != nil {
		t.Fatal(err)
	}

	if err := copyNode(source, destination); err != nil {
		t.Fatalf("copy directory tree: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(destination, "nested", "data.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "recursive copy" {
		t.Fatalf("copied content = %q, want %q", got, "recursive copy")
	}
	link := filepath.Join(destination, "nested", "data-link")
	target, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("copied symlink missing: %v", err)
	}
	if target != "data.txt" {
		t.Fatalf("symlink target = %q, want %q", target, "data.txt")
	}
}
