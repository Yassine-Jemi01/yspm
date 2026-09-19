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
