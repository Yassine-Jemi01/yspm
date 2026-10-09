package pathutil

import (
	"path/filepath"
	"strings"
)

// Within reports whether target resolves lexically to root or a path below it.
func Within(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel))
}
