package pathutil

import (
	"path/filepath"
	"testing"
)

func TestWithin(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "root")
	nested := filepath.Join(root, "usr", "bin", "tool")
	sibling := filepath.Join(parent, "root-escape", "tool")
	outside := filepath.Join(parent, "outside")

	tests := []struct {
		name, target string
		want         bool
	}{
		{name: "root itself", target: root, want: true},
		{name: "nested target", target: nested, want: true},
		{name: "sibling sharing prefix", target: sibling, want: false},
		{name: "outside target", target: outside, want: false},
		{name: "parent directory", target: parent, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Within(root, tt.target); got != tt.want {
				t.Fatalf("Within(%q, %q) = %t, want %t", root, tt.target, got, tt.want)
			}
		})
	}
}
