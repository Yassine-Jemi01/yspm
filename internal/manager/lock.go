package manager

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

const (
	lockRetryInterval = 50 * time.Millisecond
	lockWaitTimeout   = 30 * time.Second
)

// withLock uses an advisory kernel lock instead of an O_EXCL sentinel file.
// The lock file intentionally remains on disk: deleting a locked inode can let
// a second process lock a newly-created inode while the first process is active.
// flock is released by the kernel when the process exits or crashes.
func withLock(state string, fn func() error) error {
	if err := os.MkdirAll(state, 0o755); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}
	path := filepath.Join(state, "lock")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("open transaction lock: %w", err)
	}
	defer file.Close()

	deadline := time.Now().Add(lockWaitTimeout)
	for {
		err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			defer syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
			return fn()
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) && !errors.Is(err, syscall.EINTR) {
			return fmt.Errorf("acquire transaction lock: %w", err)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for another yspm transaction after %s", lockWaitTimeout)
		}
		time.Sleep(lockRetryInterval)
	}
}
