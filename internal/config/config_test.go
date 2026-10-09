package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCacheDirForUsesExplicitMode(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("YSPM_ROOT", t.TempDir())
	t.Setenv("YSPM_DATA_DIR", "")
	t.Setenv("YSPM_CACHE_DIR", "")

	userCache, err := CacheDirFor(true)
	if err != nil {
		t.Fatal(err)
	}
	wantUser := filepath.Join(home, ".cache", "yspm")
	if userCache != wantUser {
		t.Fatalf("user cache = %q, want %q", userCache, wantUser)
	}

	systemCache, err := CacheDirFor(false)
	if err != nil {
		t.Fatal(err)
	}
	if systemCache == userCache {
		t.Fatalf("system and user cache paths must differ, got %q", systemCache)
	}
	if _, err := os.Stat(systemCache); !os.IsNotExist(err) && err != nil {
		t.Fatalf("unexpected stat error for system cache path: %v", err)
	}
}
