package resolve

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/MHilhorst/ainfra/internal/lockfile"
	"github.com/MHilhorst/ainfra/internal/manifest"
	"github.com/MHilhorst/ainfra/internal/provider"
)

// RenderResources resolves the manifest at dir and returns, per channel, the
// desired provider.Resource values with Payload populated so providers can
// render artifacts.
//
// It is the back-compatible entry point: a resolution context with the
// default identity ("human") and invocation path (".") is used. Callers that
// need to filter by caller identity or invocation path should call
// RenderResourcesFor.
// readSourceForLayer reads a sourced asset's bytes, resolving relative paths
// against the right base directory for the layer. Repo and team layer sources
// resolve against the install dir; personal-layer sources may come from the
// repo's ainfra.personal.yaml OR the user's global personal.yaml at
// $XDG_CONFIG_HOME/ainfra/personal.yaml — try the install dir first, then
// the XDG dir as a fallback. Remote sources and empty sources return "".
func readSourceForLayer(dir string, layerName manifest.Layer, source string) string {
	content, _ := readSourceForLayerExists(dir, layerName, source)
	return content
}

// readSourceForLayerExists is like readSourceForLayer but distinguishes
// "source not readable" (ok=false) from "source read successfully but
// empty" (ok=true, content=""). Hash sites that fall back to a manifest-
// shape hash when the source file is missing need this signal so a
// deliberately-empty source file produces a stable empty-content hash
// instead of looping into perpetual drift.
func readSourceForLayerExists(dir string, layerName manifest.Layer, source string) (string, bool) {
	if source == "" || isRemoteSource(source) {
		return "", false
	}
	// Absolute and ~-rooted paths bypass layer-relative resolution. XDG
	// personal manifests commonly emit absolute paths (e.g.
	// /Users/foo/.claude/commands/x.md) or ~/-rooted paths;
	// filepath.Join(dir, "/abs/path") would yield the wrong result.
	if filepath.IsAbs(source) || strings.HasPrefix(source, "~") {
		expanded := source
		if strings.HasPrefix(source, "~/") || source == "~" {
			if home, err := os.UserHomeDir(); err == nil {
				expanded = filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(source, "~"), "/"))
			}
		}
		if raw, err := os.ReadFile(expanded); err == nil {
			return string(raw), true
		}
		return "", false
	}
	if raw, err := os.ReadFile(filepath.Join(dir, source)); err == nil {
		return string(raw), true
	}
	if layerName == manifest.LayerPersonal {
		if xdgDir, err := xdgConfigAinfra(); err == nil {
			if raw, err := os.ReadFile(filepath.Join(xdgDir, source)); err == nil {
				return string(raw), true
			}
		}
	}
	return "", false
}

// xdgConfigAinfra returns $XDG_CONFIG_HOME/ainfra (or ~/.config/ainfra when
// XDG_CONFIG_HOME is unset). Local to render.go to avoid importing the
// internal/xdg package from this leaf module.
func xdgConfigAinfra() (string, error) {
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return filepath.Join(v, "ainfra"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "ainfra"), nil
}

func RenderResources(dir string, runner provider.CommandRunner) (map[string][]provider.Resource, error) {
	return RenderResourcesFor(dir, runner, DefaultContext())
}

// RenderResourcesFor is the context-aware form of RenderResources. Each entry's
// scope selector is evaluated against ctx; entries whose selector does not
// match are filtered out of the rendered set (they are still in the lockfile,
// but this invocation neither plans nor applies them).
//
// It calls RunLock(dir, runner) to write current lockfiles, then reads them to
// obtain each entry's ContentHash, Layer, and Requires. The manifest is re-read
// from the layers to build Payload fields. The function therefore relies on a
// writable working directory; callers should treat the resulting lockfiles as
// the source of truth for content hashes.
func RenderResourcesFor(dir string, runner provider.CommandRunner, ctx ResolutionContext) (map[string][]provider.Resource, error) {
	if err := RunLock(dir, runner); err != nil {
		return nil, err
	}

	committed, err := lockfile.Read(filepath.Join(dir, "ainfra.lock"))
	if err != nil {
		return nil, err
	}
	personal, err := lockfile.Read(filepath.Join(dir, "ainfra.personal.lock"))
	if err != nil {
		return nil, err
	}

	// Merge both locks into one index keyed by channel+id.
	merged := mergeLockEntries(committed, personal)

	layers, err := manifest.LoadLayers(dir)
	if err != nil {
		return nil, err
	}

	result := map[string][]provider.Resource{}

	// Accumulate resources per channel across all layers in priority order.
	seen := map[string]map[string]bool{}
	for _, layerName := range []manifest.Layer{manifest.LayerTeam, manifest.LayerRepo, manifest.LayerPersonal} {
		m, ok := layers[layerName]
		if !ok {
			continue
		}

		// mcpServers
		for _, id := range slices.Sorted(maps.Keys(m.MCPServers)) {
			if !markSeen(seen, "mcpServers", id) {
				continue
			}
			srv := m.MCPServers[id]
			// A server with enabled: false is omitted from .mcp.json.
			if srv.Enabled != nil && !*srv.Enabled {
				continue
			}
			if !SelectorMatches(srv.Scope, ctx) {
				continue
			}
			entry := merged.mcpServers[id]
			var args []string
			var envMap map[string]string
			var headersMap map[string]string
			cmd := srv.Command
			version := srv.Version
			transport := srv.Transport
			url := srv.URL

			// For templated servers, use the instantiated values from the lock.
			if srv.Template != "" {
				// The instantiated fields are not stored in the lock directly;
				// we rebuild them by re-instantiating just enough to get command/args/env.
				allTemplates := collectTemplates(layers)
				tmpl := allTemplates[srv.Template]
				resolved := entry.Resolved
				inst, err := Instantiate(id, srv, tmpl, resolved)
				if err == nil && inst.MCPServer != nil {
					cmd = inst.MCPServer.Command
					args = inst.MCPServer.Args
					version = inst.MCPServer.Version
					envMap = inst.MCPServer.Env
					transport = inst.MCPServer.Transport
					url = inst.MCPServer.URL
					headersMap = inst.MCPServer.Headers
				}
				// A template may also produce a background service; emit it as
				// a backgroundServices resource so the channel can reconcile it.
				if err == nil && inst.Service != nil {
					sid := inst.Service.ID
					if markSeen(seen, "backgroundServices", sid) {
						sEntry := merged.backgroundServices[sid]
						result["backgroundServices"] = append(result["backgroundServices"], provider.Resource{
							ID:          sid,
							Channel:     "backgroundServices",
							Layer:       sEntry.Layer,
							ContentHash: sEntry.ContentHash,
							Requires:    sEntry.Requires,
							Payload: map[string]any{
								"kind": inst.Service.Kind,
								"spec": inst.Service.Spec,
							},
						})
					}
				}
			} else {
				args = srv.Args
				envMap = srv.Env
				headersMap = srv.Headers
			}

			// Pin the package version into the launch args so package-launched
			// servers install the locked version instead of floating to latest.
			args = pinPackageVersion(cmd, args, version)

			secSrv := &manifest.MCPServer{Env: envMap, Headers: headersMap, URL: url}
			if _, err := substituteSecrets(secSrv, "mcpServers", id, manifest.Layer(entry.Layer), srv.Secret, collectSecrets(layers)); err != nil {
				return nil, err
			}
			envMap, headersMap, url = secSrv.Env, secSrv.Headers, secSrv.URL

			payload := map[string]any{
				"command":   cmd,
				"args":      args,
				"env":       envMap,
				"transport": transport,
				"url":       url,
				"headers":   headersMap,
			}
			result["mcpServers"] = append(result["mcpServers"], provider.Resource{
				ID:          id,
				Channel:     "mcpServers",
				Layer:       entry.Layer,
				ContentHash: entry.ContentHash,
				Requires:    entry.Requires,
				Payload:     payload,
			})
		}

		// hooks
		for _, id := range slices.Sorted(maps.Keys(m.Hooks)) {
			if !markSeen(seen, "hooks", id) {
				continue
			}
			h := m.Hooks[id]
			if !SelectorMatches(h.Scope, ctx) {
				continue
			}
			entry := merged.hooks[id]
			payload := map[string]any{
				"event":   h.Event,
				"matcher": h.Matcher,
				"command": h.Command,
				"timeout": h.Timeout,
			}
			// A hook may carry a bundled source script; the channel installs
			// it alongside the hook so `command` can reference it.
			if h.Source != "" && !isRemoteSource(h.Source) {
				if content := readSourceForLayer(dir, layerName, h.Source); content != "" {
					payload["scriptName"] = filepath.Base(h.Source)
					payload["scriptContent"] = content
				}
			}
			result["hooks"] = append(result["hooks"], provider.Resource{
				ID:          id,
				Channel:     "hooks",
				Layer:       entry.Layer,
				ContentHash: entry.ContentHash,
				Requires:    entry.Requires,
				Payload:     payload,
			})
		}

		// commands
		for _, id := range slices.Sorted(maps.Keys(m.Commands)) {
			if !markSeen(seen, "commands", id) {
				continue
			}
			c := m.Commands[id]
			if !SelectorMatches(c.Scope, ctx) {
				continue
			}
			entry := merged.commands[id]
			content := readSourceForLayer(dir, layerName, c.Source)
			result["commands"] = append(result["commands"], provider.Resource{
				ID:          id,
				Channel:     "commands",
				Layer:       entry.Layer,
				ContentHash: entry.ContentHash,
				Requires:    entry.Requires,
				Payload: map[string]any{
					"content": content,
				},
			})
		}

		// rules
		// Resolve vars once per layer pass (lazy — only when a templated rule is found).
		var resolvedVars map[string]string
		var resolvedVarsErr error
		var resolvedVarsComputed bool
		getResolvedVars := func() (map[string]string, error) {
			if !resolvedVarsComputed {
				resolvedVarsComputed = true
				allVars := collectVars(layers)
				resolvedVars, resolvedVarsErr = resolveVars(allVars, runner)
			}
			return resolvedVars, resolvedVarsErr
		}
		for _, id := range slices.Sorted(maps.Keys(m.Rules)) {
			if !markSeen(seen, "rules", id) {
				continue
			}
			r := m.Rules[id]
			if !SelectorMatches(r.Scope, ctx) {
				continue
			}
			entry := merged.rules[id]
			content := readSourceForLayer(dir, layerName, r.Source)
			if r.Template && content != "" {
				rv, err := getResolvedVars()
				if err != nil {
					return nil, err
				}
				substituted, err := substituteVars(content, rv)
				if err != nil {
					return nil, fmt.Errorf("rule %q: %w", id, err)
				}
				content = substituted
			}
			ruleTarget := r.Target
			if ruleTarget == "" {
				ruleTarget = "CLAUDE.md"
			}
			result["rules"] = append(result["rules"], provider.Resource{
				ID:          id,
				Channel:     "rules",
				Layer:       entry.Layer,
				ContentHash: entry.ContentHash,
				Requires:    entry.Requires,
				Payload: map[string]any{
					"target":  ruleTarget,
					"content": content,
				},
			})
		}

		// skills
		for _, id := range slices.Sorted(maps.Keys(m.Skills)) {
			if !markSeen(seen, "skills", id) {
				continue
			}
			s := m.Skills[id]
			if !SelectorMatches(s.Scope, ctx) {
				continue
			}
			entry := merged.skills[id]
			result["skills"] = append(result["skills"], provider.Resource{
				ID:          id,
				Channel:     "skills",
				Layer:       entry.Layer,
				ContentHash: entry.ContentHash,
				Requires:    entry.Requires,
				Payload: map[string]any{
					"source":  s.Source,
					"version": s.Version,
				},
			})
		}

		// marketplaces
		for _, id := range slices.Sorted(maps.Keys(m.Marketplaces)) {
			if !markSeen(seen, "marketplaces", id) {
				continue
			}
			mp := m.Marketplaces[id]
			entry := merged.marketplaces[id]
			result["marketplaces"] = append(result["marketplaces"], provider.Resource{
				ID:          id,
				Channel:     "marketplaces",
				Layer:       entry.Layer,
				ContentHash: entry.ContentHash,
				Requires:    entry.Requires,
				Payload: map[string]any{
					"source": mp.Source,
				},
			})
		}

		// plugins
		for _, id := range slices.Sorted(maps.Keys(m.Plugins)) {
			if !markSeen(seen, "plugins", id) {
				continue
			}
			p := m.Plugins[id]
			if !SelectorMatches(p.Scope, ctx) {
				continue
			}
			entry := merged.plugins[id]
			result["plugins"] = append(result["plugins"], provider.Resource{
				ID:          id,
				Channel:     "plugins",
				Layer:       entry.Layer,
				ContentHash: entry.ContentHash,
				Requires:    entry.Requires,
				Payload: map[string]any{
					"marketplace": p.Marketplace,
					"version":     p.Version,
				},
			})
		}

		// tools (fixed ID "tools" so desired matches the ID Observe returns)
		if m.Tools != nil {
			if !markSeen(seen, "tools", "tools") {
				continue
			}
			entry := merged.tools["tools"]
			toolsPayload := map[string]any{}
			if m.Tools.Builtins != nil {
				toolsPayload["disabled"] = m.Tools.Builtins.Disabled
			}
			if m.Tools.Permissions != nil {
				toolsPayload["allow"] = m.Tools.Permissions.Allow
				toolsPayload["ask"] = m.Tools.Permissions.Ask
				toolsPayload["deny"] = m.Tools.Permissions.Deny
			}
			result["tools"] = append(result["tools"], provider.Resource{
				ID:          "tools",
				Channel:     "tools",
				Layer:       entry.Layer,
				ContentHash: entry.ContentHash,
				Requires:    entry.Requires,
				Payload:     toolsPayload,
			})
		}

		// cliTools
		for _, id := range slices.Sorted(maps.Keys(m.CLITools)) {
			if !markSeen(seen, "cliTools", id) {
				continue
			}
			t := m.CLITools[id]
			if !SelectorMatches(t.Scope, ctx) {
				continue
			}
			entry := merged.cliTools[id]
			result["cliTools"] = append(result["cliTools"], provider.Resource{
				ID:          id,
				Channel:     "cliTools",
				Layer:       entry.Layer,
				ContentHash: entry.ContentHash,
				Requires:    entry.Requires,
				Payload: map[string]any{
					"install": t.Install,
					"check":   t.Check,
				},
			})
		}

		// backgroundServices
		for _, id := range slices.Sorted(maps.Keys(m.BackgroundServices)) {
			if !markSeen(seen, "backgroundServices", id) {
				continue
			}
			svc := m.BackgroundServices[id]
			entry := merged.backgroundServices[id]
			result["backgroundServices"] = append(result["backgroundServices"], provider.Resource{
				ID:          id,
				Channel:     "backgroundServices",
				Layer:       entry.Layer,
				ContentHash: entry.ContentHash,
				Requires:    entry.Requires,
				Payload: map[string]any{
					"kind": svc.Kind,
					"spec": svc.Spec,
				},
			})
		}
	}

	// Sort each channel's slice by ID for deterministic output.
	for ch := range result {
		sort.Slice(result[ch], func(i, j int) bool {
			return result[ch][i].ID < result[ch][j].ID
		})
	}

	return result, nil
}

// markSeen records that id has been seen for a channel and returns true if it
// was not already seen. First-seen wins (higher-priority layers come first).
func markSeen(seen map[string]map[string]bool, channel, id string) bool {
	if seen[channel] == nil {
		seen[channel] = map[string]bool{}
	}
	if seen[channel][id] {
		return false
	}
	seen[channel][id] = true
	return true
}

// isRemoteSource reports whether a source string refers to a remote location
// that cannot be read as a local file.
func isRemoteSource(source string) bool {
	return strings.HasPrefix(source, "git+") ||
		strings.HasPrefix(source, "npm:") ||
		strings.HasPrefix(source, "https://") ||
		strings.HasPrefix(source, "http://")
}

// packageLaunchers are commands that launch a server from a package registry,
// where a pinned version belongs in the package argument itself.
var packageLaunchers = map[string]bool{"npx": true, "uvx": true, "pipx": true}

// pinPackageVersion appends @version to the package argument of a
// package-launched server (npx/uvx/pipx), so the runtime command installs the
// pinned version instead of floating to latest. The package is the first
// non-flag argument; an argument that already carries a version is left alone.
func pinPackageVersion(command string, args []string, version string) []string {
	if version == "" || !packageLaunchers[command] || len(args) == 0 {
		return args
	}
	out := append([]string(nil), args...)
	for i, a := range out {
		if strings.HasPrefix(a, "-") {
			continue
		}
		if !argHasVersion(a) {
			out[i] = a + "@" + version
		}
		break
	}
	return out
}

// argHasVersion reports whether a package argument already carries a @version
// suffix. A leading @ (a scoped package) is skipped before the check.
func argHasVersion(arg string) bool {
	s := arg
	if strings.HasPrefix(s, "@") {
		s = s[1:]
	}
	return strings.Contains(s, "@")
}

// mergedEntries holds entry maps from both committed and personal lockfiles.
type mergedEntries struct {
	mcpServers         map[string]lockfile.Entry
	backgroundServices map[string]lockfile.Entry
	hooks              map[string]lockfile.Entry
	commands           map[string]lockfile.Entry
	cliTools           map[string]lockfile.Entry
	skills             map[string]lockfile.Entry
	marketplaces       map[string]lockfile.Entry
	plugins            map[string]lockfile.Entry
	rules              map[string]lockfile.Entry
	tools              map[string]lockfile.Entry
}

func mergeLockEntries(committed, personal *lockfile.Lock) mergedEntries {
	merge := func(a, b map[string]lockfile.Entry) map[string]lockfile.Entry {
		out := make(map[string]lockfile.Entry, len(a)+len(b))
		for k, v := range a {
			out[k] = v
		}
		for k, v := range b {
			out[k] = v
		}
		return out
	}
	return mergedEntries{
		mcpServers:         merge(committed.Entries.MCPServers, personal.Entries.MCPServers),
		backgroundServices: merge(committed.Entries.BackgroundServices, personal.Entries.BackgroundServices),
		hooks:              merge(committed.Entries.Hooks, personal.Entries.Hooks),
		commands:           merge(committed.Entries.Commands, personal.Entries.Commands),
		cliTools:           merge(committed.Entries.CLITools, personal.Entries.CLITools),
		skills:             merge(committed.Entries.Skills, personal.Entries.Skills),
		marketplaces:       merge(committed.Entries.Marketplaces, personal.Entries.Marketplaces),
		plugins:            merge(committed.Entries.Plugins, personal.Entries.Plugins),
		rules:              merge(committed.Entries.Rules, personal.Entries.Rules),
		tools:              merge(committed.Entries.Tools, personal.Entries.Tools),
	}
}

// collectSecrets merges top-level secrets: from all layers; higher layers take
// precedence (same logic as collectTemplates).
func collectSecrets(layers map[manifest.Layer]*manifest.Manifest) map[string]manifest.Secret {
	all := map[string]manifest.Secret{}
	for _, layerName := range []manifest.Layer{manifest.LayerTeam, manifest.LayerRepo, manifest.LayerPersonal} {
		m, ok := layers[layerName]
		if !ok {
			continue
		}
		for name, s := range m.Secrets {
			if _, exists := all[name]; !exists {
				all[name] = s
			}
		}
	}
	return all
}

// collectTemplates merges templates from all layers; lower layers take
// precedence (same logic as RunLock).
func collectTemplates(layers map[manifest.Layer]*manifest.Manifest) map[string]manifest.Template {
	all := map[string]manifest.Template{}
	for _, layerName := range []manifest.Layer{manifest.LayerTeam, manifest.LayerRepo, manifest.LayerPersonal} {
		m, ok := layers[layerName]
		if !ok {
			continue
		}
		for name, tmpl := range m.Templates {
			if _, exists := all[name]; !exists {
				all[name] = tmpl
			}
		}
	}
	return all
}
