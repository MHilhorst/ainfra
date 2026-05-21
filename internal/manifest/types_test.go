package manifest

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestManifestUnmarshalsMultiDBShape(t *testing.T) {
	src := []byte(`
version: 1
cliTools:
  ssh:
    versionConstraint: ">=8.0"
templates:
  t:
    params:
      host: { type: string, required: true }
mcpServers:
  analytics-db:
    template: t
    params: { host: a.example }
`)
	var m Manifest
	if err := yaml.Unmarshal(src, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m.Version != 1 {
		t.Errorf("version = %d, want 1", m.Version)
	}
	if _, ok := m.CLITools["ssh"]; !ok {
		t.Error("cliTools.ssh missing")
	}
	inst := m.MCPServers["analytics-db"]
	if inst.Template != "t" {
		t.Errorf("template = %q, want t", inst.Template)
	}
	if inst.Params["host"] != "a.example" {
		t.Errorf("params.host = %v", inst.Params["host"])
	}
}

func TestUnmarshalNewChannels(t *testing.T) {
	src := `version: 1
skills:
  debug:
    source: "git+https://github.com/acme/skills.git@v1.4.0#debug"
    version: "1.4.0"
plugins:
  tvt:
    source: "npm:@acme/tvt-plugin@2.0.1"
    version: "2.0.1"
rules:
  team:
    target: CLAUDE.md
    source: ./rules/team.md
    version: "1"
tools:
  builtins:
    disabled: [WebFetch]
  permissions:
    allow: ["Bash(go test:*)"]
    deny: ["Bash(rm -rf:*)"]
`
	var m Manifest
	if err := yaml.Unmarshal([]byte(src), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m.Skills["debug"].Version != "1.4.0" {
		t.Errorf("skill version = %q", m.Skills["debug"].Version)
	}
	if m.Plugins["tvt"].Source != "npm:@acme/tvt-plugin@2.0.1" {
		t.Errorf("plugin source = %q", m.Plugins["tvt"].Source)
	}
	if m.Rules["team"].Target != "CLAUDE.md" {
		t.Errorf("rule target = %q", m.Rules["team"].Target)
	}
	if m.Tools == nil || len(m.Tools.Builtins.Disabled) != 1 || m.Tools.Builtins.Disabled[0] != "WebFetch" {
		t.Errorf("tools.builtins.disabled = %+v", m.Tools)
	}
	if m.Tools.Permissions.Deny[0] != "Bash(rm -rf:*)" {
		t.Errorf("tools.permissions.deny = %+v", m.Tools.Permissions.Deny)
	}
}

func TestManifestUnmarshalsHooksAndCommands(t *testing.T) {
	src := []byte(`
version: 1
hooks:
  guard-branch:
    event: PreToolUse
    matcher: "Edit|Write"
    command: node guard.js
    timeout: 3000
    requires: [ { cliTool: node } ]
commands:
  ship:
    source: ./commands/ship.md
    description: Fast-path merge.
`)
	var m Manifest
	if err := yaml.Unmarshal(src, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	h := m.Hooks["guard-branch"]
	if h.Event != "PreToolUse" || h.Matcher != "Edit|Write" {
		t.Errorf("hook not parsed: %+v", h)
	}
	if h.Timeout != 3000 || h.Command != "node guard.js" {
		t.Errorf("hook fields wrong: %+v", h)
	}
	if len(h.Requires) != 1 || h.Requires[0].CLITool != "node" {
		t.Errorf("hook requires not parsed: %+v", h.Requires)
	}
	c := m.Commands["ship"]
	if c.Source != "./commands/ship.md" || c.Description != "Fast-path merge." {
		t.Errorf("command not parsed: %+v", c)
	}
}
