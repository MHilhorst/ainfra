package channels

import (
	"errors"
	iofs "io/fs"
	"path/filepath"
	"strings"

	"github.com/MHilhorst/ainfra/internal/provider"
	"github.com/MHilhorst/ainfra/internal/provider/fsmerge"
)

// Rules reconciles rule fragment files under <root>/.claude/ainfra/.
// Each fragment is fully owned by ainfra; the filename without .md extension
// is the resource ID. The rule's target file (e.g. CLAUDE.md) receives a
// single @-import line pointing at the fragment.
type Rules struct{}

// Channel returns the channel name this provider manages.
func (Rules) Channel() string { return "rules" }

func rulesDir(env provider.Env) string {
	return filepath.Join(env.Root, ".claude", "ainfra")
}

func fragmentPath(env provider.Env, id string) string {
	return filepath.Join(rulesDir(env), id+".md")
}

// Observe lists *.md files in <root>/.claude/ainfra/ and returns a Resource
// per file. A missing directory is treated as no resources. ContentHash is
// left empty; the orchestrator backfills it from the ledger.
func (Rules) Observe(env provider.Env) ([]provider.Resource, error) {
	entries, err := env.FS.ReadDir(rulesDir(env))
	if errors.Is(err, iofs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var resources []provider.Resource
	for _, name := range entries {
		if !strings.HasSuffix(name, ".md") {
			continue
		}
		id := strings.TrimSuffix(name, ".md")
		resources = append(resources, provider.Resource{
			ID:      id,
			Channel: "rules",
		})
	}
	return resources, nil
}

// Apply executes the channel plan, writing fragment files and ensuring import
// lines in target files, or removing fragments on delete.
// When env.DryRun is true, the result is computed but no files are modified.
//
// Documented limitation: Delete removes the fragment file but does not strip
// the @import line from the target file; a follow-up can add fsmerge.RemoveImportLine.
func (Rules) Apply(env provider.Env, plan provider.ChannelPlan) (provider.ApplyResult, error) {
	var applied []provider.Change

	for _, c := range plan.Changes {
		if c.Kind == provider.ChangeNoop {
			continue
		}

		if !env.DryRun {
			var err error
			switch c.Kind {
			case provider.ChangeCreate, provider.ChangeUpdate:
				target, _ := c.Resource.Payload["target"].(string)
				if target == "" {
					// target is optional; when omitted the renderer defaults to CLAUDE.md.
					target = "CLAUDE.md"
				}
				content, _ := c.Resource.Payload["content"].(string)
				err = fsmerge.WriteOwnedFile(env.FS, fragmentPath(env, c.ID), []byte(content))
				if err != nil {
					return provider.ApplyResult{}, err
				}
				importPath := ".claude/ainfra/" + c.ID + ".md"
				err = fsmerge.EnsureImportLine(env.FS, filepath.Join(env.Root, target), importPath)
			case provider.ChangeDelete:
				err = env.FS.Remove(fragmentPath(env, c.ID))
			}
			if err != nil {
				return provider.ApplyResult{}, err
			}
		}

		applied = append(applied, c)
	}

	return provider.ApplyResult{
		Channel: "rules",
		Applied: applied,
	}, nil
}
