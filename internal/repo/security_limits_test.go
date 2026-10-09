package repo

import (
	"archive/zip"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Yassine-Jemi01/yspm/internal/model"
)

func TestSignedCachedIndexIsVerifiedEveryTime(t *testing.T) {
	t.Setenv("YSPM_REQUIRE_SIGNATURES", "1")
	t.Setenv("YSPM_CACHE_DIR", filepath.Join(t.TempDir(), "cache"))
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("YSPM_REPOSITORY_PUBLIC_KEY", hex.EncodeToString(publicKey))

	idx := validStableIndex()
	raw, err := json.Marshal(idx)
	if err != nil {
		t.Fatal(err)
	}
	signature := ed25519.Sign(privateKey, raw)
	if err := CacheFetchedIndexFor(raw, signature, true); err != nil {
		t.Fatalf("cache signed index: %v", err)
	}
	if _, err := LoadCachedIndexFor(true); err != nil {
		t.Fatalf("load authentic cached index: %v", err)
	}

	cachePath, err := indexCachePathFor(true)
	if err != nil {
		t.Fatal(err)
	}
	cacheBytes, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	var envelope indexCacheEnvelope
	if err := json.Unmarshal(cacheBytes, &envelope); err != nil {
		t.Fatal(err)
	}
	var tampered model.Index
	if err := json.Unmarshal(envelope.RawIndex, &tampered); err != nil {
		t.Fatal(err)
	}
	tampered.Packages[0].Version = "999.0"
	envelope.RawIndex, err = json.Marshal(tampered)
	if err != nil {
		t.Fatal(err)
	}
	cacheBytes, err = json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, cacheBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCachedIndexFor(true); err == nil {
		t.Fatal("expected tampered signed cache to be rejected")
	}
}

func TestRequiredSignatureRejectsUnsignedCachedIndex(t *testing.T) {
	t.Setenv("YSPM_REQUIRE_SIGNATURES", "1")
	t.Setenv("YSPM_CACHE_DIR", filepath.Join(t.TempDir(), "cache"))
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("YSPM_REPOSITORY_PUBLIC_KEY", hex.EncodeToString(publicKey))
	raw, err := json.Marshal(validStableIndex())
	if err != nil {
		t.Fatal(err)
	}
	if err := CacheFetchedIndexFor(raw, nil, true); err == nil {
		t.Fatal("expected unsigned cached index to be rejected when signatures are required")
	}
}

func TestIndexCacheUsesExplicitUserMode(t *testing.T) {
	home := t.TempDir()
	root := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("YSPM_ROOT", root)
	t.Setenv("YSPM_CACHE_DIR", "")
	t.Setenv("YSPM_REQUIRE_SIGNATURES", "")
	if err := CacheIndexFor(validStableIndex(), true); err != nil {
		t.Fatal(err)
	}
	userCache := filepath.Join(home, ".cache", "yspm", "index.json")
	systemCache := filepath.Join(root, "var", "cache", "yspm", "index.json")
	if _, err := os.Stat(userCache); err != nil {
		t.Fatalf("user cache not used: %v", err)
	}
	if _, err := os.Stat(systemCache); !os.IsNotExist(err) {
		t.Fatalf("system cache unexpectedly used for --user mode: %v", err)
	}
}

func TestReadSourceLimitRejectsOversizedLocalMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "large.json")
	if err := os.WriteFile(path, []byte("123456789"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readSourceLimit(path, 8); err == nil {
		t.Fatal("expected oversized metadata to be rejected")
	}
}

func TestWriteAtomicLimitDoesNotLeavePartialDestination(t *testing.T) {
	path := filepath.Join(t.TempDir(), "download.bin")
	if err := writeAtomicLimit(strings.NewReader("abcdef"), path, 5); err == nil {
		t.Fatal("expected oversized stream to be rejected")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("partial destination was left behind: %v", err)
	}
}
func TestExtractZipRejectsDuplicateNamesBeforeWriting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "duplicate.zip")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for i := 0; i < 2; i++ {
		entry, err := writer.Create("bin/tool")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte("tool")); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "stage")
	if err := ExtractArchive(path, "zip", destination); err == nil {
		t.Fatal("expected duplicate ZIP names to be rejected")
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("destination should not be created for invalid ZIP: %v", err)
	}
}
