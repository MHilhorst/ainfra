package manifest

import (
	"fmt"
	"maps"
	"slices"
)

// packageLaunchers are commands that launch a server from a package registry;
// such servers must pin an exact version (spec §5.1).
var packageLaunchers = map[string]bool{"npx": true, "uvx": true, "pipx": true}

// hookEvents are the Claude Code lifecycle events a hook may bind to (spec §11).
var hookEvents = map[string]bool{
	"SessionStart": true, "SessionEnd": true, "UserPromptSubmit": true,
	"PreToolUse": true, "PostToolUse": true, "Notification": true,
	"Stop": true, "SubagentStop": true, "PreCompact": true,
}

// Validate runs static checks on a single resolved manifest. Entries are
// checked in sorted-key order so the first reported error is deterministic.
func Validate(m *Manifest) error {
	for _, id := range slices.Sorted(maps.Keys(m.MCPServers)) {
		srv := m.MCPServers[id]
		if srv.Template != "" {
			if _, ok := m.Templates[srv.Template]; !ok {
				return fmt.Errorf("mcpServers.%s: unknown template %q", id, srv.Template)
			}
			continue
		}
		if packageLaunchers[srv.Command] && srv.Version == "" {
			return fmt.Errorf("mcpServers.%s: package-launched servers must pin an exact version", id)
		}
	}
	for _, id := range slices.Sorted(maps.Keys(m.Templates)) {
		tmpl := m.Templates[id]
		if srv := tmpl.Produces.MCPServer; srv != nil {
			if packageLaunchers[srv.Command] && srv.Version == "" {
				return fmt.Errorf("templates.%s: package-launched servers must pin an exact version", id)
			}
		}
	}
	for _, id := range slices.Sorted(maps.Keys(m.Hooks)) {
		h := m.Hooks[id]
		if !hookEvents[h.Event] {
			return fmt.Errorf("hooks.%s: unknown or missing event %q", id, h.Event)
		}
		if h.Command == "" {
			return fmt.Errorf("hooks.%s: a hook must declare a command", id)
		}
	}
	for _, id := range slices.Sorted(maps.Keys(m.Commands)) {
		if m.Commands[id].Source == "" {
			return fmt.Errorf("commands.%s: a command must declare a source", id)
		}
	}
	return nil
}
