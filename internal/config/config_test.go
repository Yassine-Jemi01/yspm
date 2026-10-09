package config

import (
	"path/filepath"
	"reflect"
	"testing"
)

func clearPathOverrides(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"YSPM_ROOT", "YSPM_DATA_DIR", "YSPM_CACHE_DIR", "YSPM_BIN_DIR",
		"YSPM_APPLICATIONS_DIR",
	} {
		t.Setenv(key, "")
	}
}

func TestNewPathsUsesSeparateUserAndSystemLayouts(t *testing.T) {
	home := t.TempDir()
	systemRoot := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("YSPM_ROOT", systemRoot)
	clearPathOverrides(t)

	userPaths, err := NewPaths(true)
	if err != nil {
		t.Fatal(err)
	}
	if userPaths.Root != home {
		t.Fatalf("user root = %q, want %q", userPaths.Root, home)
	}
	if userPaths.Database != filepath.Join(home, ".local", "share", "yspm", "database.json") {
		t.Fatalf("user database path = %q", userPaths.Database)
	}
	if userPaths.Cache != filepath.Join(home, ".cache", "yspm") {
		t.Fatalf("user cache path = %q", userPaths.Cache)
	}

	systemPaths, err := NewPaths(false)
	if err != nil {
		t.Fatal(err)
	}
	if systemPaths.Root != systemRoot {
		t.Fatalf("system root = %q, want %q", systemPaths.Root, systemRoot)
	}
	if systemPaths.Database != filepath.Join(systemRoot, "var", "lib", "yspm", "database.json") {
		t.Fatalf("system database path = %q", systemPaths.Database)
	}
	if systemPaths.Cache != filepath.Join(systemRoot, "var", "cache", "yspm") {
		t.Fatalf("system cache path = %q", systemPaths.Cache)
	}
}

func TestCacheDirForHonorsExplicitUserMode(t *testing.T) {
	home := t.TempDir()
	systemRoot := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("YSPM_ROOT", systemRoot)
	t.Setenv("YSPM_CACHE_DIR", "")

	userCache, err := CacheDirFor(true)
	if err != nil {
		t.Fatal(err)
	}
	systemCache, err := CacheDirFor(false)
	if err != nil {
		t.Fatal(err)
	}
	if userCache != filepath.Join(home, ".cache", "yspm") {
		t.Fatalf("user cache = %q", userCache)
	}
	if systemCache != filepath.Join(systemRoot, "var", "cache", "yspm") {
		t.Fatalf("system cache = %q", systemCache)
	}
	if userCache == systemCache {
		t.Fatal("user and system cache paths should differ")
	}
}

func TestNormalizeArchAliases(t *testing.T) {
	tests := map[string]string{
		"amd64":  "x86_64",
		"X86-64": "x86_64",
		"arm64":  "aarch64",
		"armhf":  "armv7",
		"386":    "i386",
		" RISCV64 ": "riscv64",
	}
	for input, want := range tests {
		if got := NormalizeArch(input); got != want {
			t.Errorf("NormalizeArch(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestForeignArchitecturesNormalizesAliases(t *testing.T) {
	t.Setenv("YSPM_FOREIGN_ARCHS", "arm64, amd64, ,RISCV64")
	want := map[string]bool{
		"aarch64": true,
		"x86_64":  true,
		"riscv64": true,
	}
	if got := ForeignArchitectures(); !reflect.DeepEqual(got, want) {
		t.Fatalf("ForeignArchitectures() = %#v, want %#v", got, want)
	}
}

func TestReleaseRepositoryURLUsesTemplate(t *testing.T) {
	t.Setenv("YSPM_RELEASE_REPOSITORY_TEMPLATE", "https://repo.example/releases/{release}/index.json")
	if got, want := ReleaseRepositoryURL("2"), "https://repo.example/releases/2/index.json"; got != want {
		t.Fatalf("ReleaseRepositoryURL() = %q, want %q", got, want)
	}
}
