package manager

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWithLockSerializesCallers(t *testing.T) {
	state := t.TempDir()
	firstEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- withLock(state, func() error {
			close(firstEntered)
			<-releaseFirst
			return nil
		})
	}()
	<-firstEntered

	secondEntered := make(chan struct{})
	secondDone := make(chan error, 1)
	go func() {
		secondDone <- withLock(state, func() error {
			close(secondEntered)
			return nil
		})
	}()
	select {
	case <-secondEntered:
		t.Fatal("second transaction entered while the first held the lock")
	case <-time.After(75 * time.Millisecond):
	}

	close(releaseFirst)
	if err := <-firstDone; err != nil {
		t.Fatalf("first lock holder: %v", err)
	}
	select {
	case <-secondEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("second transaction did not acquire lock after release")
	}
	if err := <-secondDone; err != nil {
		t.Fatalf("second lock holder: %v", err)
	}
}

func TestWithLockCanRecoverFromStaleLockFile(t *testing.T) {
	state := t.TempDir()
	lockPath := filepath.Join(state, "lock")
	if err := os.WriteFile(lockPath, []byte("old lock from crashed process"), 0o600); err != nil {
		t.Fatal(err)
	}
	entered := false
	if err := withLock(state, func() error {
		entered = true
		return nil
	}); err != nil {
		t.Fatalf("flock should recover even if the lock file already exists: %v", err)
	}
	if !entered {
		t.Fatal("transaction did not execute")
	}
	// The lock inode remains in place to prevent waiters locking different inodes.
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("lock file should remain in place: %v", err)
	}
}
