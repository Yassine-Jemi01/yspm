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
		aNum := numericToken(as[i])
		bNum := numericToken(bs[i])
		if aNum && bNum {
			if cmp := compareNumericToken(as[i], bs[i]); cmp != 0 {
				return cmp
			}
			continue
		}
		if aNum != bNum {
			if aNum {
				return 1
			}
			return -1
		}
		if as[i] < bs[i] {
			return -1
		}
		if as[i] > bs[i] {
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
			out = append(out, strings.ToLower(cur.String()))
			cur.Reset()
		}
	}
	for _, r := range v {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
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

// compareNumericToken compares decimal strings without converting them to a
// fixed-width integer, so arbitrarily large version components remain ordered.
func compareNumericToken(a, b string) int {
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
