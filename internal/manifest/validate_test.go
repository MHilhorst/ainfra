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

func TestValidateRejectsSkillWithoutSource(t *testing.T) {
	m := &Manifest{Version: 1, Skills: map[string]Skill{"s": {Version: "1.0.0"}}}
	d := asDiagnostic(t, Validate(m))
	if !strings.Contains(d.Summary, "skill declares no source") {
		t.Errorf("summary = %q", d.Summary)
	}
	if d.Path != "skills.s" {
		t.Errorf("path = %q, want skills.s", d.Path)
	}
}

func TestValidateRejectsPluginWithoutSource(t *testing.T) {
	m := &Manifest{Version: 1, Plugins: map[string]Plugin{"p": {}}}
	d := asDiagnostic(t, Validate(m))
	if !strings.Contains(d.Summary, "plugin declares no source") {
		t.Errorf("summary = %q", d.Summary)
	}
}

func TestValidateAcceptsRuleWithoutTarget(t *testing.T) {
	// A rule's destination is renderer-owned (multi-agent renderers spec §3.3);
	// an explicit target is an optional override, not a requirement.
	m := &Manifest{Version: 1, Rules: map[string]Rule{"r": {Source: "./r.md"}}}
	if err := Validate(m); err != nil {
		t.Fatalf("a rule without an explicit target must validate: %v", err)
	}
}

func TestValidateRejectsEmptyPermissionPattern(t *testing.T) {
	m := &Manifest{Version: 1, Tools: &Tools{
		Permissions: &Permissions{Allow: []string{"  "}},
	}}
	d := asDiagnostic(t, Validate(m))
	if d.Path != "tools.permissions.allow" {
		t.Errorf("path = %q, want tools.permissions.allow", d.Path)
	}
}

func TestValidateAcceptsWellFormedChannels(t *testing.T) {
	m := &Manifest{Version: 1,
		Skills:  map[string]Skill{"s": {Source: "github:acme/skills/x", Version: "1.0.0"}},
		Plugins: map[string]Plugin{"p": {Source: "npm:@acme/p@1.0.0"}},
		Rules:   map[string]Rule{"r": {Target: "CLAUDE.md", Source: "./r.md"}},
		Tools: &Tools{
			Builtins:    &Builtins{Disabled: []string{"WebFetch"}},
			Permissions: &Permissions{Allow: []string{"Bash(go build:*)"}, Deny: []string{"Bash(rm:*)"}},
		},
	}
	if err := Validate(m); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateAllRejectsUnknownAgent(t *testing.T) {
	layers := map[Layer]*Manifest{
		LayerRepo: {Version: 1, Agent: "emacs-doctor"},
	}
	d := asDiagnostic(t, ValidateAll(layers))
	if !strings.Contains(d.Summary, "unknown agent") {
		t.Errorf("summary = %q, want it to mention an unknown agent", d.Summary)
	}
	if d.Path != "agent" {
		t.Errorf("path = %q, want agent", d.Path)
	}
	if d.File != "ainfra.yaml" {
		t.Errorf("file = %q, want ainfra.yaml", d.File)
	}
	if d.Hint == "" {
		t.Error("expected a hint listing valid agents")
	}
}

func TestValidateAllAcceptsKnownAgent(t *testing.T) {
	layers := map[Layer]*Manifest{
		LayerRepo: {Version: 1, Agent: "codex"},
	}
	if err := ValidateAll(layers); err != nil {
		t.Fatalf("unexpected error for a known agent: %v", err)
	}
}

func TestValidateAllAcceptsChannelGatedAwayFromAgent(t *testing.T) {
	layers := map[Layer]*Manifest{
		LayerRepo: {Version: 1, Agent: "codex",
			Hooks: map[string]Hook{
				"gofmt": {Event: "PostToolUse", Command: "gofmt -w .", Agents: []string{"claude-code"}},
			}},
	}
	if err := ValidateAll(layers); err != nil {
		t.Fatalf("a hook gated to claude-code only must validate under agent codex: %v", err)
	}
}

func TestValidateAllRejectsUngatedChannelUnsupportedByAgent(t *testing.T) {
	layers := map[Layer]*Manifest{
		LayerRepo: {Version: 1, Agent: "codex",
			Hooks: map[string]Hook{
				"gofmt": {Event: "PostToolUse", Command: "gofmt -w ."},
			}},
	}
	d := asDiagnostic(t, ValidateAll(layers))
	if !strings.Contains(d.Summary, "hooks") {
		t.Errorf("summary = %q, want it to name the hooks channel", d.Summary)
	}
	if d.Path != "hooks.gofmt" {
		t.Errorf("path = %q, want hooks.gofmt", d.Path)
	}
	if d.File != "ainfra.yaml" {
		t.Errorf("file = %q, want ainfra.yaml", d.File)
	}
	if d.Hint == "" {
		t.Error("expected a hint suggesting agents: gating")
	}
}

func TestValidateAllRejectsEntryGatedToAnAgentThatCannotRenderIt(t *testing.T) {
	layers := map[Layer]*Manifest{
		LayerRepo: {Version: 1, Agent: "codex",
			Hooks: map[string]Hook{
				"gofmt": {Event: "PostToolUse", Command: "gofmt -w .", Agents: []string{"codex"}},
			}},
	}
	d := asDiagnostic(t, ValidateAll(layers))
	if !strings.Contains(d.Summary, "cannot render") {
		t.Errorf("summary = %q, want it to say the agent cannot render the channel", d.Summary)
	}
	if d.Path != "hooks.gofmt" {
		t.Errorf("path = %q, want hooks.gofmt", d.Path)
	}
}

func TestValidateAllRejectsUnknownAgentInGatingList(t *testing.T) {
	layers := map[Layer]*Manifest{
		LayerRepo: {Version: 1,
			MCPServers: map[string]MCPServer{
				"github": {Command: "npx", Version: "0.6.2", Agents: []string{"emacs-doctor"}},
			}},
	}
	d := asDiagnostic(t, ValidateAll(layers))
	if !strings.Contains(d.Summary, "unknown agent") {
		t.Errorf("summary = %q, want it to mention an unknown agent", d.Summary)
	}
	if d.Path != "mcpServers.github" {
		t.Errorf("path = %q, want mcpServers.github", d.Path)
	}
}
