package manager

import (
	"strings"
	"unicode"
)

func compareVersion(a, b string) int {
	if a == b {
		return 0
	}
	as, bs := tokenizeVersion(a), tokenizeVersion(b)
	for i := 0; i < len(as) || i < len(bs); i++ {
		if i >= len(as) {
			return -1
		}
		if i >= len(bs) {
			return 1
		}
		aNum, bNum := numericToken(as[i]), numericToken(bs[i])
		if aNum && bNum {
			if result := compareNumericTokens(as[i], bs[i]); result != 0 {
				return result
			}
			continue
		}
		if aNum != bNum {
			if aNum {
				return 1
			}
			return -1
		}
		ai, bi := strings.ToLower(as[i]), strings.ToLower(bs[i])
		if ai < bi {
			return -1
		}
		if ai > bi {
			return 1
		}
	}
	return 0
}

func tokenizeVersion(v string) []string {
	var out []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for _, r := range v {
		if unicode.IsLetter(r) || (r >= '0' && r <= '9') {
			cur.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return out
}

func numericToken(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// compareNumericTokens compares unbounded decimal version tokens without
// overflowing a machine integer. Leading zeroes are ignored for ordering.
func compareNumericTokens(a, b string) int {
	a = strings.TrimLeft(a, "0")
	b = strings.TrimLeft(b, "0")
	if a == "" {
		a = "0"
	}
	if b == "" {
		b = "0"
	}
	if len(a) < len(b) {
		return -1
	}
	if len(a) > len(b) {
		return 1
	}
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}
