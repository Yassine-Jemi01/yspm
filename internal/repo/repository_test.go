package repo

import (
	"testing"

	"github.com/Yassine-Jemi01/yspm/internal/model"
)

func TestCompareVersion(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.2.0", "1.1.9", 1},
		{"1.9.0", "2.0.0", -1},
		{"2.0", "2.0.0", 0},
		{"155.0.1", "154.9.0", 1},
	}
	for _, tc := range cases {
		if got := CompareVersion(tc.a, tc.b); got != tc.want {
			t.Fatalf("CompareVersion(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestSafeArchivePath(t *testing.T) {
	bad := []string{"../evil", "/etc/passwd", `..\\evil`}
	for _, path := range bad {
		if _, err := safeArchivePath(path); err == nil {
			t.Fatalf("expected %q to be rejected", path)
		}
	}
}

func TestValidateStableIndex(t *testing.T) {
	idx := model.Index{Release: "1", Channel: "stable", Packages: []model.Package{{Name: "ok", Version: "1.0.0", Kind: "binary"}, {Name: "meta", Version: "1.0.0", Kind: "meta"}}}
	if err := ValidateStableIndex(idx); err != nil {
		t.Fatalf("valid stable index rejected: %v", err)
	}
}

func TestValidateInstallPackage(t *testing.T) {
	pkg := model.Package{Name: "ok", Version: "1.0.0", Kind: "binary", SHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}
	if err := ValidateInstallPackage(pkg); err != nil {
		t.Fatalf("valid package rejected: %v", err)
	}
	pkg.SHA256 = ""
	if err := ValidateInstallPackage(pkg); err == nil {
		t.Fatal("expected missing SHA-256 to be rejected")
	}
}
