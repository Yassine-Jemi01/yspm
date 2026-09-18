package repo

import (
	"fmt"
	"strings"

	"github.com/Yassine-Jemi01/yspm/internal/model"
)

func ValidateStableIndex(idx model.Index) error {
	if strings.TrimSpace(idx.Release) == "" {
		return fmt.Errorf("repository index is missing a release identifier")
	}
	if idx.Channel != "stable" {
		return fmt.Errorf("unsupported repository channel %q; yspm only tracks stable releases", idx.Channel)
	}
	return nil
}

func ValidateInstallPackage(p model.Package) error {
	if p.Kind == "meta" {
		return nil
	}
	if len(p.SHA256) != 64 || !isHex(p.SHA256) {
		return fmt.Errorf("package %q %s has no valid SHA-256 checksum", p.Name, p.Version)
	}
	return nil
}

func isHex(s string) bool {
	for _, r := range s {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return true
}
