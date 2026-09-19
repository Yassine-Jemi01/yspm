package manager

import "testing"

func TestParseDependency(t *testing.T) {
	tests := []struct {
		in, name, op, version, arch string
	}{
		{"openssl>=3.0", "openssl", ">=", "3.0", ""},
		{"foo=1.2:amd64", "foo", "=", "1.2", "x86_64"},
		{"libssl.so.3", "libssl.so.3", "", "", ""},
		{"foo|bar", "foo|bar", "", "", ""},
	}
	for _, tt := range tests {
		got := parseDependency(tt.in)
		if got.Name != tt.name || got.Op != tt.op || got.Version != tt.version || got.Arch != tt.arch {
			t.Fatalf("%q -> %+v, want %s %s %s %s", tt.in, got, tt.name, tt.op, tt.version, tt.arch)
		}
	}
}
