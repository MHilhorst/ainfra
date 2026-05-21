package manifest

import (
	"strings"
	"testing"

	"github.com/MHilhorst/ainfra/internal/diag"
)

func asDiagnostic(t *testing.T, err error) *diag.Diagnostic {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	d, ok := err.(*diag.Diagnostic)
	if !ok {
		t.Fatalf("error is %T, want *diag.Diagnostic: %v", err, err)
	}
	return d
}

func TestValidateRejectsFloatingMCPVersion(t *testing.T) {
	m := &Manifest{Version: 1, MCPServers: map[string]MCPServer{
		"s": {Command: "npx", Args: []string{"-y", "pkg@latest"}},
	}}
	d := asDiagnostic(t, Validate(m))
	if !strings.Contains(d.Summary, "pin an exact version") {
		t.Errorf("summary = %q", d.Summary)
	}
	if d.Path != "mcpServers.s" {
		t.Errorf("path = %q, want mcpServers.s", d.Path)
	}
	if d.Hint == "" {
		t.Error("expected a hint")
	}
}

func TestValidateAcceptsPinnedMCPVersion(t *testing.T) {
	m := &Manifest{Version: 1, MCPServers: map[string]MCPServer{
		"s": {Command: "npx", Args: []string{"-y", "pkg"}, Version: "1.2.3"},
	}}
	if err := Validate(m); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateRejectsUnknownTemplate(t *testing.T) {
	m := &Manifest{Version: 1, MCPServers: map[string]MCPServer{
		"s": {Template: "missing"},
	}}
	d := asDiagnostic(t, Validate(m))
	if !strings.Contains(d.Summary, "unknown template") {
		t.Errorf("summary = %q", d.Summary)
	}
}

func TestValidateRejectsUnknownHookEvent(t *testing.T) {
	m := &Manifest{Version: 1, Hooks: map[string]Hook{
		"h": {Event: "OnEverything", Command: "echo x"},
	}}
	d := asDiagnostic(t, Validate(m))
	if !strings.Contains(d.Summary, "event") {
		t.Errorf("summary = %q", d.Summary)
	}
}

func TestValidateRejectsHookWithoutCommand(t *testing.T) {
	m := &Manifest{Version: 1, Hooks: map[string]Hook{
		"h": {Event: "SessionStart"},
	}}
	d := asDiagnostic(t, Validate(m))
	if !strings.Contains(d.Summary, "command") {
		t.Errorf("summary = %q", d.Summary)
	}
}

func TestValidateRejectsCommandWithoutSource(t *testing.T) {
	m := &Manifest{Version: 1, Commands: map[string]Command{
		"c": {Description: "no source"},
	}}
	d := asDiagnostic(t, Validate(m))
	if !strings.Contains(d.Summary, "source") {
		t.Errorf("summary = %q", d.Summary)
	}
}

func TestValidateAcceptsValidHooksAndCommands(t *testing.T) {
	m := &Manifest{Version: 1,
		Hooks: map[string]Hook{
			"h": {Event: "PreToolUse", Matcher: "Bash", Command: "echo guard"},
		},
		Commands: map[string]Command{
			"c": {Source: "./commands/c.md", Description: "a command"},
		},
	}
	if err := Validate(m); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateAllSetsFileFromLayer(t *testing.T) {
	layers := map[Layer]*Manifest{
		LayerRepo: {Version: 1},
		LayerPersonal: {Version: 1, MCPServers: map[string]MCPServer{
			"bad": {Command: "npx"},
		}},
	}
	d := asDiagnostic(t, ValidateAll(layers))
	if d.File != "ainfra.personal.yaml" {
		t.Errorf("file = %q, want ainfra.personal.yaml", d.File)
	}
}

func TestValidateAllResolvesCrossLayerTemplate(t *testing.T) {
	// The personal layer uses a template defined only in the repo layer.
	layers := map[Layer]*Manifest{
		LayerRepo: {Version: 1, Templates: map[string]Template{"t": {}}},
		LayerPersonal: {Version: 1, MCPServers: map[string]MCPServer{
			"mine": {Template: "t"},
		}},
	}
	if err := ValidateAll(layers); err != nil {
		t.Fatalf("cross-layer template should validate: %v", err)
	}
}

func TestValidateRejectsRemoteSkillWithoutVersion(t *testing.T) {
	m := &Manifest{Version: 1, Skills: map[string]Skill{
		"s": {Source: "git+https://github.com/acme/skills.git@main#s"},
	}}
	d := asDiagnostic(t, Validate(m))
	if !strings.Contains(d.Summary, "pin an exact version") {
		t.Errorf("summary = %q", d.Summary)
	}
	if d.Path != "skills.s" {
		t.Errorf("path = %q", d.Path)
	}
}

func TestValidateAcceptsLocalSkillWithoutVersion(t *testing.T) {
	m := &Manifest{Version: 1, Skills: map[string]Skill{
		"s": {Source: "./skills/s"},
	}}
	if err := Validate(m); err != nil {
		t.Fatalf("local-path skill needs no version: %v", err)
	}
}

func TestValidateRejectsSkillWithoutSource(t *testing.T) {
	m := &Manifest{Version: 1, Skills: map[string]Skill{"s": {}}}
	d := asDiagnostic(t, Validate(m))
	if !strings.Contains(d.Summary, "source") {
		t.Errorf("summary = %q", d.Summary)
	}
	if d.Path != "skills.s" {
		t.Errorf("path = %q, want skills.s", d.Path)
	}
}

func TestValidateRejectsRemotePluginWithoutVersion(t *testing.T) {
	m := &Manifest{Version: 1, Plugins: map[string]Plugin{
		"p": {Source: "npm:@acme/p"},
	}}
	d := asDiagnostic(t, Validate(m))
	if !strings.Contains(d.Summary, "pin an exact version") {
		t.Errorf("summary = %q", d.Summary)
	}
}

func TestValidateAcceptsValidSkillsAndPlugins(t *testing.T) {
	m := &Manifest{Version: 1,
		Skills:  map[string]Skill{"s": {Source: "git+https://github.com/acme/skills.git", Version: "1.4.0"}},
		Plugins: map[string]Plugin{"p": {Source: "npm:@acme/p", Version: "2.0.1"}},
	}
	if err := Validate(m); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateRejectsRuleWithoutTarget(t *testing.T) {
	m := &Manifest{Version: 1, Rules: map[string]Rule{
		"r": {Source: "./rules/r.md"},
	}}
	d := asDiagnostic(t, Validate(m))
	if !strings.Contains(d.Summary, "target") {
		t.Errorf("summary = %q", d.Summary)
	}
}

func TestValidateRejectsRuleWithoutSource(t *testing.T) {
	m := &Manifest{Version: 1, Rules: map[string]Rule{
		"r": {Target: "CLAUDE.md"},
	}}
	d := asDiagnostic(t, Validate(m))
	if !strings.Contains(d.Summary, "source") {
		t.Errorf("summary = %q", d.Summary)
	}
}

func TestValidateRejectsEmptyDisabledBuiltin(t *testing.T) {
	m := &Manifest{Version: 1, Tools: &Tools{
		Builtins: ToolBuiltins{Disabled: []string{""}},
	}}
	d := asDiagnostic(t, Validate(m))
	if !strings.Contains(d.Summary, "empty") {
		t.Errorf("summary = %q", d.Summary)
	}
}

func TestValidateRejectsRemoteRuleWithoutVersion(t *testing.T) {
	m := &Manifest{Version: 1, Rules: map[string]Rule{
		"r": {Target: "CLAUDE.md", Source: "git+https://github.com/acme/rules.git"},
	}}
	d := asDiagnostic(t, Validate(m))
	if !strings.Contains(d.Summary, "pin an exact version") {
		t.Errorf("summary = %q", d.Summary)
	}
	if d.Path != "rules.r" {
		t.Errorf("path = %q, want rules.r", d.Path)
	}
}

func TestValidateRejectsEmptyDenyPermission(t *testing.T) {
	m := &Manifest{Version: 1, Tools: &Tools{
		Permissions: ToolPermissions{Deny: []string{"  "}},
	}}
	d := asDiagnostic(t, Validate(m))
	if !strings.Contains(d.Summary, "empty") {
		t.Errorf("summary = %q", d.Summary)
	}
	if !strings.Contains(d.Path, "tools.permissions.deny") {
		t.Errorf("path = %q, want tools.permissions.deny[...]", d.Path)
	}
}

func TestValidateAcceptsValidNewChannels(t *testing.T) {
	m := &Manifest{Version: 1,
		Rules: map[string]Rule{"r": {Target: "CLAUDE.md", Source: "./rules/r.md"}},
		Tools: &Tools{
			Builtins:    ToolBuiltins{Disabled: []string{"WebFetch"}},
			Permissions: ToolPermissions{Allow: []string{"Bash(go test:*)"}},
		},
	}
	if err := Validate(m); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
