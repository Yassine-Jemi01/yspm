package manager

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestWithLockSerializesConcurrentTransactions(t *testing.T) {
	state := t.TempDir()
	var mu sync.Mutex
	var active, maximum int
	const workers = 8
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- withLock(state, func() error {
				mu.Lock()
				active++
				if active > maximum {
					maximum = active
				}
				mu.Unlock()
				time.Sleep(5 * time.Millisecond)
				mu.Lock()
				active--
				mu.Unlock()
				return nil
			})
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if maximum != 1 {
		t.Fatalf("maximum concurrent critical sections = %d, want 1", maximum)
	}
	if _, err := os.Stat(filepath.Join(state, "lock")); err != nil {
		t.Fatalf("persistent lock inode should remain after unlocking: %v", err)
	}
}
