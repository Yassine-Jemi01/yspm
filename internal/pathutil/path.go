package pathutil

import "path/filepath"

// Within reports whether target resolves lexically to root or a path below it.
func Within(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !filepath.IsAbs(rel) && len(rel) > 2 && rel[:3] != ".."+string(filepath.Separator)) || rel == ".."
}
