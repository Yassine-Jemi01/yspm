package manager

import "testing"

func TestParseDependencyArchitectureAndEpoch(t *testing.T) {
	tests := []struct {
		input, name, op, version, arch string
	}{
		{input: "foo>=1:2.0", name: "foo", op: ">=", version: "1:2.0"},
		{input: "foo:aarch64>=2.0", name: "foo", op: ">=", version: "2.0", arch: "aarch64"},
		{input: "foo:amd64", name: "foo", arch: "x86_64"},
		{input: "foo>=1.2.0", name: "foo", op: ">=", version: "1.2.0"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseDependency(tt.input)
			if got.Name != tt.name || got.Op != tt.op || got.Version != tt.version || got.Arch != tt.arch {
				t.Fatalf("parseDependency(%q) = %#v, want name=%q op=%q version=%q arch=%q", tt.input, got, tt.name, tt.op, tt.version, tt.arch)
			}
		})
	}
}
