package claudecode

import (
	"encoding/json"
	"errors"
	"fmt"
	iofs "io/fs"
	"path/filepath"
	"strings"

	"github.com/MHilhorst/ainfra/internal/lockfile"
	"github.com/MHilhorst/ainfra/internal/provider"
)

// pluginContentHash derives a plugin's reconcile hash from its marketplace and
// version. It MUST stay byte-identical to the desired-hash construction in
// resolve/pipeline.go — the diff compares the two directly, so any divergence
// silently breaks reconciliation rather than failing loudly.
func pluginContentHash(marketplace, version string) string {
	return lockfile.ContentHash(map[string]any{
		"marketplace": marketplace, "version": version,
	})
}

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
// bare name so it matches the manifest plugin key.
//
// ContentHash is derived from {marketplace, installed version} — deliberately
// the same shape resolve/pipeline.go uses to build the lockfile's desired hash.
// The two must be computed identically or the diff is meaningless: an in-sync
// plugin has to hash equal to produce Noop, and a moved pin has to hash
// different to produce Update.
//
// Claude Code records the resolved version in installed_plugins.json, which is
// authoritative — it is the same string it uses as the cache directory name
// (the semver from plugin.json, else the marketplace entry's version, else the
// commit SHA, else "unknown"). An unpinned plugin therefore observes as its
// resolved SHA and can never equal the desired hash of an empty version, which
// is what keeps `claude plugin update` running on every install for the
// SHA-versioned flow.
func (Plugins) Observe(env provider.Env) ([]provider.Resource, error) {
	installed, err := readInstalledPlugins(env)
	if err != nil {
		return nil, err
	}

	resources := make([]provider.Resource, 0, len(installed))
	for key, installs := range installed {
		// key is "name@marketplace"; extract the bare name.
		name, marketplace := splitPluginKey(key)
		resources = append(resources, provider.Resource{
			ID:          name,
			Channel:     "plugins",
			ContentHash: pluginContentHash(marketplace, resolvedVersion(installs)),
		})
	}
	return resources, nil
}

// readInstalledPlugins parses Claude Code's installed_plugins.json into its
// "name@marketplace" -> installs map. A missing file yields a nil map and no
// error: nothing is installed yet.
func readInstalledPlugins(env provider.Env) (map[string][]installedPlugin, error) {
	raw, err := env.FS.ReadFile(installedPluginsPath(env))
	if errors.Is(err, iofs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var doc struct {
		Plugins map[string][]installedPlugin `json:"plugins"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	return doc.Plugins, nil
}

// installedPlugin is one entry of an installed_plugins.json plugin array. Claude
// Code stores an array because the same plugin can be installed at more than one
// scope (user, project).
type installedPlugin struct {
	Scope   string `json:"scope"`
	Version string `json:"version"`
}

// resolvedVersion picks the version ainfra reconciles against. ainfra installs
// at user scope, so a user-scope entry wins; otherwise the first entry is used.
// An empty list observes as "" — treated as "installed but version unknown",
// which never matches a desired hash and so re-runs the update.
func resolvedVersion(installs []installedPlugin) string {
	for _, in := range installs {
		if in.Scope == "user" {
			return in.Version
		}
	}
	if len(installs) > 0 {
		return installs[0].Version
	}
	return ""
}

// readResolvedPluginVersion returns the version Claude Code actually resolved
// this plugin to, per installed_plugins.json. Empty means "not installed, or no
// resolvable version" and the caller treats it as nothing to compare against.
func readResolvedPluginVersion(env provider.Env, name, marketplace string) (string, error) {
	installed, err := readInstalledPlugins(env)
	if err != nil {
		return "", err
	}
	return resolvedVersion(installed[name+"@"+marketplace]), nil
}

// splitPluginKey splits a "name@marketplace" key into its parts. When the key
// has no '@' the whole key is the name and the marketplace is "".
func splitPluginKey(key string) (string, string) {
	if idx := strings.Index(key, "@"); idx >= 0 {
		return key[:idx], key[idx+1:]
	}
	return key, ""
}

// Apply executes the channel plan for plugins via the `claude` CLI.
//
// Create: `claude plugin install <id>@<marketplace>`. "Already installed" is
// success. After a successful install, the resolved version is read from
// installed_plugins.json and compared against the pinned version (when set); a
// mismatch is reported as a Warning, not a Failed change, because Claude Code
// is the source of truth for the cache key.
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

			case provider.ChangeUpdate, provider.ChangeRefresh:
				target := c.ID + "@" + marketplace
				// Best-effort update: Claude Code itself decides whether to
				// pull a new version based on its cache key, so the worst
				// case here is a no-op. ChangeRefresh is the unpinned form of
				// the same operation and runs identically.
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
// actually resolved, per installed_plugins.json. It reports (warning, true)
// when a pin is set and the resolved version differs. When the pin is empty
// (the SHA-versioned flow), or nothing is installed yet, no warning is
// produced.
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
