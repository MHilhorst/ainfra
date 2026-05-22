package claudecode

import (
	"encoding/json"
	"errors"
	iofs "io/fs"
	"path/filepath"
	"strings"

	"github.com/MHilhorst/ainfra/internal/provider"
)

// Plugins installs and reconciles Claude Code plugins via the `claude` CLI.
// Resource.Payload keys consumed: "marketplace" (string), "version" (string).
type Plugins struct{}

// Channel returns the channel name this provider manages.
func (Plugins) Channel() string { return "plugins" }

// installedPluginsPath returns the path to Claude Code's installed_plugins.json
// under env.Home.
func installedPluginsPath(env provider.Env) string {
	return filepath.Join(env.Home, ".claude", "plugins", "installed_plugins.json")
}

// Observe reads installed_plugins.json and returns a Resource per installed
// plugin. The file keys plugins as "name@marketplace"; the resource ID is the
// bare name so it matches the manifest plugin key. ContentHash is left empty —
// the orchestrator backfills it from the ledger.
func (Plugins) Observe(env provider.Env) ([]provider.Resource, error) {
	raw, err := env.FS.ReadFile(installedPluginsPath(env))
	if errors.Is(err, iofs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var doc struct {
		Plugins map[string]json.RawMessage `json:"plugins"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}

	resources := make([]provider.Resource, 0, len(doc.Plugins))
	for key := range doc.Plugins {
		// key is "name@marketplace"; extract the bare name.
		name := pluginNameFromKey(key)
		resources = append(resources, provider.Resource{
			ID:      name,
			Channel: "plugins",
		})
	}
	return resources, nil
}

// pluginNameFromKey extracts the bare plugin name from a "name@marketplace" key.
func pluginNameFromKey(key string) string {
	if idx := strings.Index(key, "@"); idx >= 0 {
		return key[:idx]
	}
	return key
}

// Apply executes the channel plan for plugins via the `claude` CLI.
// Create: `claude plugin install <id>@<marketplace>` — "already installed" is success.
// Update: `claude plugin update <id>@<marketplace>` — best-effort, skip on failure.
// Delete: `claude plugin uninstall <id>`.
// Honors env.DryRun.
func (Plugins) Apply(env provider.Env, plan provider.ChannelPlan) (provider.ApplyResult, error) {
	var applied []provider.Change

	for _, c := range plan.Changes {
		if c.Kind == provider.ChangeNoop {
			continue
		}

		if !env.DryRun {
			marketplace, _ := c.Resource.Payload["marketplace"].(string)
			version, _ := c.Resource.Payload["version"].(string)

			switch c.Kind {
			case provider.ChangeCreate:
				target := c.ID + "@" + marketplace
				_, err := env.Runner.Run("claude", "plugin", "install", target)
				if err != nil && !isAlreadyInstalledError(err) {
					return provider.ApplyResult{}, err
				}

			case provider.ChangeUpdate:
				// Best-effort update: if a version is pinned, try to update.
				// Skip on failure rather than aborting the channel.
				if version != "" {
					target := c.ID + "@" + marketplace
					// Ignore the error — version reconciliation is best-effort.
					_, _ = env.Runner.Run("claude", "plugin", "update", target)
				}

			case provider.ChangeDelete:
				if _, err := env.Runner.Run("claude", "plugin", "uninstall", c.ID); err != nil {
					return provider.ApplyResult{}, err
				}
			}
		}

		applied = append(applied, c)
	}

	return provider.ApplyResult{
		Channel: "plugins",
		Applied: applied,
	}, nil
}

// isAlreadyInstalledError reports whether the error from `claude plugin install`
// indicates the plugin is already installed.
func isAlreadyInstalledError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "already installed")
}
