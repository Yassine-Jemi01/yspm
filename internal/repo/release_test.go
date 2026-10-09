package repo

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Yassine-Jemi01/yspm/internal/model"
)

func validInstallPackage() model.Package {
	return model.Package{
		Name:         "demo",
		Version:      "1.0.0",
		Kind:         "binary",
		ABI:          "test-abi-1",
		URL:          "https://packages.example.test/demo.tar.gz",
		SHA256:       strings.Repeat("a", 64),
		Architecture: "x86_64",
	}
}

func validStableIndex() model.Index {
	return model.Index{
		Release:  "1",
		Channel:  "stable",
		ABI:      "test-abi-1",
		Packages: []model.Package{validInstallPackage()},
	}
}

func TestValidateStableIndexRequiresABI(t *testing.T) {
	idx := validStableIndex()
	idx.ABI = ""
	if err := ValidateStableIndex(idx); err == nil {
		t.Fatal("expected index without ABI to be rejected")
	}
}

func TestValidateStableIndexRequiresInstallChecksum(t *testing.T) {
	idx := validStableIndex()
	idx.Packages[0].SHA256 = ""
	if err := ValidateStableIndex(idx); err == nil {
		t.Fatal("expected installable package without SHA-256 to be rejected")
	}
}

func TestValidateStableIndexRequiresPackageABI(t *testing.T) {
	idx := validStableIndex()
	idx.Packages[0].ABI = ""
	if err := ValidateStableIndex(idx); err == nil {
		t.Fatal("expected installable package without ABI to be rejected")
	}
}

func TestValidateStableIndexRejectsMismatchedPackageABI(t *testing.T) {
	idx := validStableIndex()
	idx.Packages[0].ABI = "different-abi"
	if err := ValidateStableIndex(idx); err == nil {
		t.Fatal("expected package ABI that differs from index ABI to be rejected")
	}
}

func TestValidateStableIndexRejectsInvalidChecksumAndURL(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*model.Package)
	}{
		{
			name: "checksum length",
			mutate: func(p *model.Package) {
				p.SHA256 = "abcd"
			},
		},
		{
			name: "non-hex checksum",
			mutate: func(p *model.Package) {
				p.SHA256 = strings.Repeat("z", 64)
			},
		},
		{
			name: "missing URL",
			mutate: func(p *model.Package) {
				p.URL = ""
			},
		},
		{
			name: "unsupported URL scheme",
			mutate: func(p *model.Package) {
				p.URL = "javascript:alert(1)"
			},
		},
		{
			name: "HTTP URL missing host",
			mutate: func(p *model.Package) {
				p.URL = "https:///demo.tar.gz"
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			idx := validStableIndex()
			tt.mutate(&idx.Packages[0])
			if err := ValidateStableIndex(idx); err == nil {
				t.Fatal("expected invalid package metadata to be rejected")
			}
		})
	}
}

func TestValidateStableIndexAllowsMetaPackageWithoutArchive(t *testing.T) {
	idx := validStableIndex()
	idx.Packages = []model.Package{{
		Name:    "tools",
		Version: "1.0.0",
		Kind:    "meta",
	}}
	if err := ValidateStableIndex(idx); err != nil {
		t.Fatalf("meta-package should not require an archive checksum or URL: %v", err)
	}
}

func TestShippedRepositoryIndexPassesValidation(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "repo", "releases", "1", "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	var idx model.Index
	if err := json.Unmarshal(data, &idx); err != nil {
		t.Fatalf("decode shipped repository index: %v", err)
	}
	if err := ValidateStableIndex(idx); err != nil {
		t.Fatalf("shipped repository index should satisfy validation: %v", err)
	}
}

func TestHardcoreIndexDeclaresExplicitLegacyABI(t *testing.T) {
	root := t.TempDir()
	list := strings.Repeat("a", 64) + " demo.tar demo\n"
	if err := os.WriteFile(filepath.Join(root, "list.sha256"), []byte(list), 0o600); err != nil {
		t.Fatal(err)
	}
	idx, err := FetchHardcoreIndex(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateStableIndex(idx); err != nil {
		t.Fatalf("HardcoreLinux index should satisfy explicit legacy ABI validation: %v", err)
	}
	if idx.ABI != "hardcore-legacy" || idx.Packages[0].ABI != idx.ABI {
		t.Fatalf("Hardcore ABI metadata is inconsistent: index=%q package=%q", idx.ABI, idx.Packages[0].ABI)
	}
}
