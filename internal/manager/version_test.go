package manager

import "testing"

func TestCompareVersion(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.2.0", "1.2.0", 0},
		{"1.10.0", "1.9.0", 1},
		{"1.2.0", "1.3.0", -1},
		{"2.0", "1.99", 1},
	}
	for _, tc := range cases {
		if got := compareVersion(tc.a, tc.b); got != tc.want {
			t.Fatalf("compareVersion(%q,%q)=%d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestSatisfies(t *testing.T) {
	cases := []struct {
		version, constraint string
		want                bool
	}{
		{"1.5.0", ">=1.2.0", true},
		{"1.0.0", ">1.0.0", false},
		{"1.0.0", "=1.0.0", true},
		{"1.0.0", "!=1.0.0", false},
		{"2.0.0", "<2.1", true},
	}
	for _, tc := range cases {
		if got := satisfies(tc.version, tc.constraint); got != tc.want {
			t.Fatalf("satisfies(%q,%q)=%v, want %v", tc.version, tc.constraint, got, tc.want)
		}
	}
}
