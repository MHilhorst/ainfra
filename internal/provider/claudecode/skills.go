package claudecode

import (
	"errors"
	"fmt"
	iofs "io/fs"
	"path/filepath"
	"strings"

	"github.com/MHilhorst/ainfra/internal/provider"
	"github.com/MHilhorst/ainfra/internal/provider/fsmerge"
)

// Skills reconciles skill bundles under <root>/.claude/skills/<id>/.
// Each skill directory is fully owned by ainfra. Resource.Payload keys:
// "source" (string), "version" (string).
type Skills struct{}

// Channel returns the channel name this provider manages.
func (Skills) Channel() string { return "skills" }

func skillsDir(env provider.Env) string {
	return filepath.Join(env.Root, ".claude", "skills")
}

func skillDir(env provider.Env, id string) string {
	return filepath.Join(skillsDir(env), id)
}

// Observe lists subdirectories of <root>/.claude/skills/ and returns a Resource
// per skill that contains at least one file. A missing directory is treated as
// no resources.
func (Skills) Observe(env provider.Env) ([]provider.Resource, error) {
	entries, err := env.FS.ReadDir(skillsDir(env))
	if errors.Is(err, iofs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var resources []provider.Resource
	for _, name := range entries {
		dir := filepath.Join(skillsDir(env), name)
		files, err := env.FS.ReadDir(dir)
		if err != nil || len(files) == 0 {
			continue
		}
		resources = append(resources, provider.Resource{
			ID:      name,
			Channel: "skills",
		})
	}
	return resources, nil
}

// Apply executes the channel plan. For Create/Update it fetches the bundle via
// env.Fetch and writes each file under the skill directory using
// fsmerge.WriteOwnedFile. For Delete it removes every file in the skill
// directory. Honors env.DryRun.
func (Skills) Apply(env provider.Env, plan provider.ChannelPlan) (provider.ApplyResult, error) {
	var applied []provider.Change

	for _, c := range plan.Changes {
		if c.Kind == provider.ChangeNoop {
			continue
		}

		if !env.DryRun {
			switch c.Kind {
			case provider.ChangeCreate, provider.ChangeUpdate:
				if env.Fetch == nil {
					return provider.ApplyResult{}, fmt.Errorf("skills: env.Fetch is nil; cannot fetch bundle for %q", c.ID)
				}
				source, _ := c.Resource.Payload["source"].(string)
				version, _ := c.Resource.Payload["version"].(string)
				bundle, fetchErr := env.Fetch.Fetch(source, version)
				if fetchErr != nil {
					return provider.ApplyResult{}, fetchErr
				}
				dir := skillDir(env, c.ID)
				for relPath, content := range bundle {
					abs := filepath.Join(dir, relPath)
					if abs != dir && !strings.HasPrefix(abs, dir+string(filepath.Separator)) {
						return provider.ApplyResult{}, fmt.Errorf("skills: bundle key %q escapes the skill directory", relPath)
					}
					if writeErr := fsmerge.WriteOwnedFile(env.FS, abs, content); writeErr != nil {
						return provider.ApplyResult{}, writeErr
					}
				}
			case provider.ChangeDelete:
				skillDir := skillDir(env, c.ID)
				if removeErr := env.FS.RemoveAll(skillDir); removeErr != nil {
					return provider.ApplyResult{}, removeErr
				}
			}
		}

		applied = append(applied, c)
	}

	return provider.ApplyResult{
		Channel: "skills",
		Applied: applied,
	}, nil
}
