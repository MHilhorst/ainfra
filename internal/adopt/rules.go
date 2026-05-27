package adopt

import (
	"os"
	"path/filepath"

	"github.com/MHilhorst/ainfra/internal/manifest"
)

// readRules emits one manifest rule per top-level context file present in the
// repo. CLAUDE.md and AGENTS.md are the two recognised filenames.
func readRules(dir string) map[string]manifest.Rule {
	out := map[string]manifest.Rule{}
	for id, file := range map[string]string{
		"claude-md": "CLAUDE.md",
		"agents-md": "AGENTS.md",
	} {
		path := filepath.Join(dir, file)
		if _, err := os.Stat(path); err == nil {
			out[id] = manifest.Rule{
				Source: "./" + file,
				Target: file,
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
