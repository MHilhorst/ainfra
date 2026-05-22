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

// resolveRuleTarget turns a rule's target path into an absolute path. A
// "~"-prefixed target is user-level and expands against env.Home; an absolute
// target is used verbatim; any other target is repo-level and joins env.Root.
func resolveRuleTarget(env provider.Env, target string) string {
	switch {
	case target == "~":
		return env.Home
	case strings.HasPrefix(target, "~/"):
		return filepath.Join(env.Home, target[2:])
	case filepath.IsAbs(target):
		return target
	default:
		return filepath.Join(env.Root, target)
	}
}

// fragmentFor returns where a rule's fragment file is written. It is
// co-located with the target: a user-level ("~") target keeps its fragment
// under env.Home so the target's @import line resolves correctly; every other
// target keeps it under env.Root.
func fragmentFor(env provider.Env, target, id string) string {
	base := env.Root
	if target == "~" || strings.HasPrefix(target, "~/") {
		base = env.Home
	}
	return filepath.Join(base, ".claude", "ainfra", id+".md")
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
				fragment := fragmentFor(env, target, c.ID)
				err = fsmerge.WriteOwnedFile(env.FS, fragment, []byte(content))
				if err != nil {
					return provider.ApplyResult{}, err
				}
				targetPath := resolveRuleTarget(env, target)
				importPath, relErr := filepath.Rel(filepath.Dir(targetPath), fragment)
				if relErr != nil {
					return provider.ApplyResult{}, relErr
				}
				err = fsmerge.EnsureImportLine(env.FS, targetPath, importPath)
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
