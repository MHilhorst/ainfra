package channels

import (
	"encoding/json"
	"errors"
	iofs "io/fs"
	"path/filepath"

	"github.com/MHilhorst/ainfra/internal/provider"
	"github.com/MHilhorst/ainfra/internal/provider/fsmerge"
)

// Plugins records the desired plugin set in an ainfra-managed file under
// <root>/.claude/ainfra/plugins.json. Actually installing plugins via the
// Claude Code marketplace is a follow-up; this provider records the declared
// plugin set. Resource.Payload keys: "source" (string), "version" (string).
type Plugins struct{}

// Channel returns the channel name this provider manages.
func (Plugins) Channel() string { return "plugins" }

func pluginsPath(env provider.Env) string {
	return filepath.Join(env.Root, ".claude", "ainfra", "plugins.json")
}

// Observe reads plugins.json and returns a Resource for each key under
// "plugins". A missing file is treated as no resources.
func (Plugins) Observe(env provider.Env) ([]provider.Resource, error) {
	raw, err := env.FS.ReadFile(pluginsPath(env))
	if errors.Is(err, iofs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}

	plugins, ok := doc["plugins"].(map[string]any)
	if !ok {
		return nil, nil
	}

	resources := make([]provider.Resource, 0, len(plugins))
	for key := range plugins {
		resources = append(resources, provider.Resource{
			ID:      key,
			Channel: "plugins",
		})
	}
	return resources, nil
}

// Apply executes the channel plan against plugins.json. When env.DryRun is
// true, it computes the result but does not write the file.
func (Plugins) Apply(env provider.Env, plan provider.ChannelPlan) (provider.ApplyResult, error) {
	desired := map[string]any{}
	ownedKeys := make([]string, 0, len(plan.Changes))
	var applied []provider.Change

	for _, c := range plan.Changes {
		if c.Kind == provider.ChangeNoop {
			continue
		}
		ownedKeys = append(ownedKeys, c.ID)
		applied = append(applied, c)

		if c.Kind == provider.ChangeCreate || c.Kind == provider.ChangeUpdate {
			source, _ := c.Resource.Payload["source"].(string)
			version, _ := c.Resource.Payload["version"].(string)
			desired[c.ID] = map[string]any{
				"source":  source,
				"version": version,
			}
		}
		// ChangeDelete: contributes to ownedKeys but not desired, so the merge removes it.
	}

	if len(ownedKeys) == 0 {
		return provider.ApplyResult{Channel: "plugins"}, nil
	}

	if !env.DryRun {
		if err := fsmerge.MergeJSONKeys(env.FS, pluginsPath(env), "plugins", desired, ownedKeys); err != nil {
			return provider.ApplyResult{}, err
		}
	}

	return provider.ApplyResult{
		Channel: "plugins",
		Applied: applied,
	}, nil
}
