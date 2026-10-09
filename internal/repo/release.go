package repo

import (
	"fmt"
	"strings"

	"github.com/Yassine-Jemi01/yspm/internal/model"
)

// ValidateStableIndex validates all release metadata before it can be cached,
// resolved, or used to download packages. Installable entries must provide
// complete download metadata and declare exactly the ABI advertised by index.
func ValidateStableIndex(idx model.Index) error {
	if strings.TrimSpace(idx.Release) == "" {
		return fmt.Errorf("repository index is missing a release identifier")
	}
	if idx.Channel != "stable" {
		return fmt.Errorf("unsupported repository channel %q; yspm only tracks stable releases", idx.Channel)
	}
	abi := strings.TrimSpace(idx.ABI)
	if abi == "" {
		return fmt.Errorf("repository index is missing an ABI identifier")
	}

	seen := make(map[string]bool, len(idx.Packages))
	for _, p := range idx.Packages {
		name := strings.TrimSpace(p.Name)
		if name == "" {
			return fmt.Errorf("repository index contains a package with no name")
		}
		identity := name + "@" + strings.TrimSpace(p.Architecture)
		if seen[identity] {
			return fmt.Errorf("repository index contains duplicate package entry %q for architecture %q", name, p.Architecture)
		}
		seen[identity] = true

		if p.Kind != "meta" {
			if err := ValidateInstallPackage(p); err != nil {
				return fmt.Errorf("invalid repository package %q: %w", name, err)
			}
			if strings.TrimSpace(p.ABI) != abi {
				return fmt.Errorf("package %q ABI %q does not match repository ABI %q", name, p.ABI, abi)
			}
			continue
		}
		if p.ABI != "" && strings.TrimSpace(p.ABI) != abi {
			return fmt.Errorf("meta-package %q ABI %q does not match repository ABI %q", name, p.ABI, abi)
		}
	}
	return nil
}
