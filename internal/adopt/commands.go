package adopt

import (
	"errors"
	"fmt"
	iofs "io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/MHilhorst/ainfra/internal/manifest"
)

// readCommands enumerates <dir>/.claude/commands/*.md, emitting one command
// entry per file with the filename (minus .md) as its id.
func readCommands(dir string) (map[string]manifest.Command, error) {
	root := filepath.Join(dir, ".claude", "commands")
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
			Source: "./.claude/commands/" + name,
		}
	}
	return out, nil
}
