package adopt

import (
	"os"

	"github.com/MHilhorst/ainfra/internal/manifest"
)

// readRules emits one manifest rule per RuleSource whose file is present.
func readRules(sources []RuleSource) map[string]manifest.Rule {
	out := map[string]manifest.Rule{}
	for _, rs := range sources {
		if _, err := os.Stat(rs.Path); err != nil {
			continue
		}
		out[rs.ID] = manifest.Rule{
			Source: rs.Source,
			Target: rs.Target,
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
