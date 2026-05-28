package adopt

import (
	"errors"
	"fmt"
	iofs "io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/MHilhorst/ainfra/internal/manifest"
)

// readSkills enumerates subdirectories of root and emits one Skill entry per
// directory using the directory name as the skill id. The convention Claude
// Code follows is "a skill is a directory containing SKILL.md" — but we
// accept any subdirectory so the import path stays generous (a hand-built
// skill missing SKILL.md still surfaces and the user can fix it later).
//
// sourceBase is the prefix written into Skill.Source ("./.claude/skills" for
// repo scope, an absolute path for user scope), matching the pattern used by
// commands and rules.
func readSkills(root, sourceBase string) (map[string]manifest.Skill, error) {
	entries, err := os.ReadDir(root)
	if errors.Is(err, iofs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("adopt: read %s: %w", root, err)
	}
	out := map[string]manifest.Skill{}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	for _, name := range names {
		out[name] = manifest.Skill{
			Source: filepath.Join(sourceBase, name),
		}
	}
	return out, nil
}
