package repo

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSignedCachedIndexIsVerifiedOnEveryLoad(t *testing.T) {
	cacheDir := t.TempDir()
	t.Setenv("YSPM_CACHE_DIR", cacheDir)
	t.Setenv("YSPM_REQUIRE_SIGNATURES", "1")
	t.Setenv("YSPM_REPOSITORY_SIGNATURE", "")
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("YSPM_REPOSITORY_PUBLIC_KEY", hex.EncodeToString(publicKey))

	source := "https://repo.example.test/releases/1/index.json"
	indexData := []byte(`{"release":"1","channel":"stable","abi":"test-abi","packages":[]}`)
	signature := ed25519.Sign(privateKey, indexData)
	if err := CacheIndexProofFor(true, source, indexData, signature); err != nil {
		t.Fatalf("cache signed index: %v", err)
	}
	if _, err := LoadCachedIndexFor(true, source); err != nil {
		t.Fatalf("load valid signed cache: %v", err)
	}

	cachePath := filepath.Join(cacheDir, "index.json")
	data, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	var envelope indexCacheEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatal(err)
	}
	envelope.IndexData = []byte(`{"release":"1","channel":"stable","abi":"test-abi","packages":[{"name":"tampered","kind":"meta"}]}`)
	data, err = json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCachedIndexFor(true, source); err == nil {
		t.Fatal("tampered cached index was trusted despite required signatures")
	}
}

func TestSignedModeRejectsLegacyUnsignedCache(t *testing.T) {
	cacheDir := t.TempDir()
	t.Setenv("YSPM_CACHE_DIR", cacheDir)
	t.Setenv("YSPM_REQUIRE_SIGNATURES", "1")
	t.Setenv("YSPM_REPOSITORY_PUBLIC_KEY", "")
	source := "https://repo.example.test/index.json"
	data := []byte(`{"release":"1","channel":"stable","abi":"test-abi","packages":[]}`)
	if err := os.WriteFile(filepath.Join(cacheDir, "index.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCachedIndexFor(true, source); err == nil {
		t.Fatal("legacy unsigned cache was trusted while signatures are required")
	}
}
