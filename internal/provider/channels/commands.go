package channels

import (
	"errors"
	iofs "io/fs"
	"path/filepath"
	"strings"

	"github.com/MHilhorst/ainfra/internal/provider"
	"github.com/MHilhorst/ainfra/internal/provider/fsmerge"
)

// Commands reconciles standalone markdown files under <root>/.claude/commands/.
// Each file is fully owned by ainfra; the filename without .md extension is the resource ID.
type Commands struct{}

// Channel returns the channel name this provider manages.
func (Commands) Channel() string { return "commands" }

func commandsDir(env provider.Env) string {
	return filepath.Join(env.Root, ".claude", "commands")
}

func commandPath(env provider.Env, id string) string {
	return filepath.Join(commandsDir(env), id+".md")
}

// Observe lists *.md files in <root>/.claude/commands/ and returns a Resource
// per file. A missing directory is treated as no resources. ContentHash is left
// empty; the orchestrator backfills it from the ledger.
func (Commands) Observe(env provider.Env) ([]provider.Resource, error) {
	entries, err := env.FS.ReadDir(commandsDir(env))
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
			Channel: "commands",
		})
	}
	return resources, nil
}

// Apply executes the channel plan, writing or removing command files.
// When env.DryRun is true, the result is computed but no files are modified.
func (Commands) Apply(env provider.Env, plan provider.ChannelPlan) (provider.ApplyResult, error) {
	var applied []provider.Change

	for _, c := range plan.Changes {
		if c.Kind == provider.ChangeNoop {
			continue
		}

		if !env.DryRun {
			var err error
			switch c.Kind {
			case provider.ChangeCreate, provider.ChangeUpdate:
				content, _ := c.Resource.Payload["content"].(string)
				err = fsmerge.WriteOwnedFile(env.FS, commandPath(env, c.ID), []byte(content))
			case provider.ChangeDelete:
				err = env.FS.Remove(commandPath(env, c.ID))
			}
			if err != nil {
				return provider.ApplyResult{}, err
			}
		}

		applied = append(applied, c)
	}

	return provider.ApplyResult{
		Channel: "commands",
		Applied: applied,
	}, nil
}
