package manager

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

func withLock(state string, fn func() error) error {
	if err := os.MkdirAll(state, 0o755); err != nil {
		return fmt.Errorf("create transaction state directory: %w", err)
	}

	// Do not unlink this file after unlocking. Processes waiting in flock must
	// keep locking the same inode; deleting it introduces a two-lock-file race.
	path := filepath.Join(state, "lock")
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return fmt.Errorf("open transaction lock: %w", err)
	}
	defer f.Close()

	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
		if err == nil {
			break
		}
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		return fmt.Errorf("acquire transaction lock: %w", err)
	}
	defer func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	}()

	if err := f.Truncate(0); err != nil {
		return fmt.Errorf("truncate transaction lock metadata: %w", err)
	}
	if _, err := f.Seek(0, 0); err != nil {
		return fmt.Errorf("seek transaction lock metadata: %w", err)
	}
	if _, err := fmt.Fprintf(f, "pid=%d\n", os.Getpid()); err != nil {
		return fmt.Errorf("write transaction lock metadata: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("sync transaction lock metadata: %w", err)
	}
	return fn()
}
