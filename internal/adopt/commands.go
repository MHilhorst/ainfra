package adopt

import (
	"errors"
	"fmt"
	iofs "io/fs"
	"os"
	"sort"
	"strings"

	"github.com/MHilhorst/ainfra/internal/manifest"
)

// readCommands enumerates the given directory for *.md files and emits one
// command entry per file with the filename (minus .md) as its id. sourceBase
// is the prefix written into Command.Source for each file ("./.claude/commands"
// for repo scope, an absolute path for user scope).
func readCommands(root, sourceBase string) (map[string]manifest.Command, error) {
	entries, err := os.ReadDir(root)
	if errors.Is(err, iofs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("adopt: read %s: %w", root, err)
	}
	out := map[string]manifest.Command{}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	for _, name := range names {
		id := strings.TrimSuffix(name, ".md")
		out[id] = manifest.Command{
			Source: sourceBase + "/" + name,
		}
	}
	return out, nil
}
