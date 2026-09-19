package manager

import (
	"strconv"
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
		ai, aNum := numberToken(as[i])
		bi, bNum := numberToken(bs[i])
		if aNum && bNum {
			if ai < bi {
				return -1
			}
			if ai > bi {
				return 1
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

func numberToken(s string) (int64, bool) {
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return 0, false
		}
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, true
	}
	return n, true
}
