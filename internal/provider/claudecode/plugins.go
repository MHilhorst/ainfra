package claudecode

import (
	"encoding/json"
	"errors"
	"fmt"
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
// bare name so it matches the manifest plugin key. ContentHash is populated
// from a recursive sha256 of the resolved version directory in Claude Code's
// plugin cache (~/.claude/plugins/cache/<name>@<marketplace>/<version>/); when
// the cache directory is absent the hash is left empty and the orchestrator
// backfills it from the ledger.
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
		name, marketplace := splitPluginKey(key)
		hash, err := hashPluginCacheDir(env, name, marketplace)
		if err != nil {
			return nil, err
		}
		resources = append(resources, provider.Resource{
			ID:          name,
			Channel:     "plugins",
			ContentHash: hash,
		})
	}
	return resources, nil
}

// splitPluginKey splits a "name@marketplace" key into its parts. When the key
// has no '@' the whole key is the name and the marketplace is "".
func splitPluginKey(key string) (string, string) {
	if idx := strings.Index(key, "@"); idx >= 0 {
		return key[:idx], key[idx+1:]
	}
	return key, ""
}

// pluginNameFromKey extracts the bare plugin name from a "name@marketplace" key.
func pluginNameFromKey(key string) string {
	name, _ := splitPluginKey(key)
	return name
}

// Apply executes the channel plan for plugins via the `claude` CLI.
//
// Create: `claude plugin install <id>@<marketplace>`. "Already installed" is
// success. After a successful install, the resolved version is read from the
// plugin's cached manifest and compared against the pinned version (when
// set); a mismatch is reported as a Warning, not a Failed change, because
// Claude Code is the source of truth for the cache key.
//
// Update: `claude plugin update <id>@<marketplace>`. Run for every
// ChangeUpdate regardless of whether a version is pinned — the SHA-versioned
// flow recommended in the plugins reference uses commit SHAs as the cache
// key and never has a `version` field. Failures are best-effort and don't
// abort the channel.
//
// Delete: `claude plugin uninstall <id>@<marketplace>`. Qualified with the
// marketplace so a plugin name shared across two registered marketplaces is
// unambiguous.
//
// Honors env.DryRun.
func (Plugins) Apply(env provider.Env, plan provider.ChannelPlan) (provider.ApplyResult, error) {
	var (
		applied  []provider.Change
		warnings []provider.ChangeWarning
	)

	for _, c := range plan.Changes {
		if c.Kind == provider.ChangeNoop {
			continue
		}

		marketplace, _ := c.Resource.Payload["marketplace"].(string)
		pinnedVersion, _ := c.Resource.Payload["version"].(string)

		if !env.DryRun {
			switch c.Kind {
			case provider.ChangeCreate:
				target := c.ID + "@" + marketplace
				_, err := env.Runner.Run("claude", "plugin", "install", target)
				if err != nil && !isAlreadyInstalledError(err) {
					return provider.ApplyResult{}, err
				}
				if w, ok := versionMismatchWarning(env, c, marketplace, pinnedVersion); ok {
					warnings = append(warnings, w)
				}

			case provider.ChangeUpdate:
				target := c.ID + "@" + marketplace
				// Best-effort update: Claude Code itself decides whether to
				// pull a new version based on its cache key, so the worst
				// case here is a no-op.
				_, _ = env.Runner.Run("claude", "plugin", "update", target)
				if w, ok := versionMismatchWarning(env, c, marketplace, pinnedVersion); ok {
					warnings = append(warnings, w)
				}

			case provider.ChangeDelete:
				target := c.ID
				if marketplace != "" {
					target = c.ID + "@" + marketplace
				}
				if _, err := env.Runner.Run("claude", "plugin", "uninstall", target); err != nil {
					return provider.ApplyResult{}, err
				}
			}
		}

		applied = append(applied, c)
	}

	return provider.ApplyResult{
		Channel:  "plugins",
		Applied:  applied,
		Warnings: warnings,
	}, nil
}

// versionMismatchWarning compares the pinned version against what Claude Code
// actually resolved in its plugin cache. It reports (warning, true) when a
// pin is set and the resolved version differs. When the pin is empty (the
// SHA-versioned flow), or the cache has not produced a manifest with a
// version field, no warning is produced.
func versionMismatchWarning(env provider.Env, c provider.Change, marketplace, pinned string) (provider.ChangeWarning, bool) {
	if pinned == "" || marketplace == "" {
		return provider.ChangeWarning{}, false
	}
	resolved, err := readResolvedPluginVersion(env, c.ID, marketplace)
	if err != nil || resolved == "" || resolved == pinned {
		return provider.ChangeWarning{}, false
	}
	return provider.ChangeWarning{
		Change: c,
		Reason: fmt.Sprintf("pinned version %q does not match Claude Code's resolved version %q; the cache key is owned by plugin.json/marketplace.json upstream", pinned, resolved),
	}, true
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
