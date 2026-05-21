package manifest

import (
	"fmt"
	"maps"
	"slices"

	"github.com/MHilhorst/ainfra/internal/diag"
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

// Validate runs static checks on a single manifest layer. It returns the first
// problem found as a *diag.Diagnostic; entries are checked in sorted-key order
// so that first problem is deterministic. The diagnostic's File is left empty
// — ValidateAll fills it from the layer. When a layer references a template
// declared in another layer, the caller (ValidateAll) injects the merged
// template map before calling Validate.
func Validate(m *Manifest) error {
	for _, id := range slices.Sorted(maps.Keys(m.MCPServers)) {
		srv := m.MCPServers[id]
		if srv.Template != "" {
			if _, ok := m.Templates[srv.Template]; !ok {
				return &diag.Diagnostic{
					Summary: fmt.Sprintf("unknown template %q", srv.Template),
					Path:    "mcpServers." + id,
					Detail:  fmt.Sprintf("Server %q references template %q, which is not defined.", id, srv.Template),
					Hint:    "Define it under templates:, or correct the name.",
				}
			}
			continue
		}
		if packageLaunchers[srv.Command] && srv.Version == "" {
			return &diag.Diagnostic{
				Summary: "package-launched server must pin an exact version",
				Path:    "mcpServers." + id,
				Detail:  fmt.Sprintf("Server %q launches via %s but declares no version.", id, srv.Command),
				Hint:    `Add a version field, e.g.  version: "1.2.3"`,
			}
		}
	}
	for _, id := range slices.Sorted(maps.Keys(m.Templates)) {
		tmpl := m.Templates[id]
		if srv := tmpl.Produces.MCPServer; srv != nil {
			if packageLaunchers[srv.Command] && srv.Version == "" {
				return &diag.Diagnostic{
					Summary: "package-launched server must pin an exact version",
					Path:    "templates." + id,
					Detail:  fmt.Sprintf("Template %q produces a server launched via %s with no version.", id, srv.Command),
					Hint:    `Add a version field to the template body, e.g.  version: "1.2.3"`,
				}
			}
		}
	}
	for _, id := range slices.Sorted(maps.Keys(m.Hooks)) {
		h := m.Hooks[id]
		if !hookEvents[h.Event] {
			return &diag.Diagnostic{
				Summary: fmt.Sprintf("unknown or missing hook event %q", h.Event),
				Path:    "hooks." + id,
				Detail:  "A hook must bind to a Claude Code lifecycle event.",
				Hint:    "Valid events: SessionStart, SessionEnd, UserPromptSubmit, PreToolUse, PostToolUse, Notification, Stop, SubagentStop, PreCompact.",
			}
		}
		if h.Command == "" {
			return &diag.Diagnostic{
				Summary: "hook declares no command",
				Path:    "hooks." + id,
				Detail:  fmt.Sprintf("Hook %q binds to %s but has nothing to run.", id, h.Event),
				Hint:    "Add a command field.",
			}
		}
	}
	for _, id := range slices.Sorted(maps.Keys(m.Commands)) {
		if m.Commands[id].Source == "" {
			return &diag.Diagnostic{
				Summary: "command declares no source",
				Path:    "commands." + id,
				Detail:  fmt.Sprintf("Command %q has no source file.", id),
				Hint:    "Add a source field pointing at the command's .md file.",
			}
		}
	}
	return nil
}

// ValidateAll validates every present layer. It builds a cross-layer template
// map first, so a lower layer may reference a template defined in a higher
// one, then tags each diagnostic with the offending layer's file name.
func ValidateAll(layers map[Layer]*Manifest) error {
	order := []Layer{LayerTeam, LayerRepo, LayerPersonal}
	allTemplates := map[string]Template{}
	for _, ln := range order {
		if m, ok := layers[ln]; ok {
			for name, tmpl := range m.Templates {
				if _, exists := allTemplates[name]; !exists {
					allTemplates[name] = tmpl
				}
			}
		}
	}
	fileFor := map[Layer]string{
		LayerRepo:     "ainfra.yaml",
		LayerPersonal: "ainfra.personal.yaml",
		LayerTeam:     "(team layer)",
	}
	for _, ln := range order {
		m, ok := layers[ln]
		if !ok {
			continue
		}
		toValidate := m
		if len(m.Templates) < len(allTemplates) {
			copied := *m
			copied.Templates = allTemplates
			toValidate = &copied
		}
		if err := Validate(toValidate); err != nil {
			if d, ok := err.(*diag.Diagnostic); ok && d.File == "" {
				d.File = fileFor[ln]
			}
			return err
		}
	}
	return nil
}
