package manifest

import (
	"strings"
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
marketplaces:
  acme:
    source: "github:acme/plugins"
plugins:
  tvt:
    marketplace: "acme"
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
	if m.Marketplaces["acme"].Source != "github:acme/plugins" {
		t.Errorf("marketplace source = %q", m.Marketplaces["acme"].Source)
	}
	if m.Plugins["tvt"].Marketplace != "acme" {
		t.Errorf("plugin marketplace = %q", m.Plugins["tvt"].Marketplace)
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

func TestManifestUnmarshalsCredentialAndRequiresFields(t *testing.T) {
	src := []byte(`
version: 1
cliTools:
  aws-cli:
    versionConstraint: ">=2.0"
    env:
      AWS_REGION: eu-west-1
    secret:
      ssoToken: { mode: direct, ref: "op://Engineering/aws/sso" }
    requires:
      - precondition: aws-credentials
mcpServers:
  linear:
    transport: http
    url: https://mcp.linear.app/sse
    headers:
      Authorization: "Bearer xyz"
`)
	var m Manifest
	if err := yaml.Unmarshal(src, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	tool := m.CLITools["aws-cli"]
	if tool.Env["AWS_REGION"] != "eu-west-1" {
		t.Errorf("cliTool env = %v", tool.Env)
	}
	if _, ok := tool.Secret["ssoToken"]; !ok {
		t.Errorf("cliTool secret not parsed: %v", tool.Secret)
	}
	if len(tool.Requires) != 1 || tool.Requires[0].Precondition != "aws-credentials" {
		t.Errorf("cliTool requires not parsed: %v", tool.Requires)
	}
	srv := m.MCPServers["linear"]
	if srv.Transport != "http" || srv.URL != "https://mcp.linear.app/sse" {
		t.Errorf("http server not parsed: %+v", srv)
	}
	if srv.Headers["Authorization"] != "Bearer xyz" {
		t.Errorf("headers not parsed: %v", srv.Headers)
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

func TestStrictDecodeAcceptsAgentsGatingField(t *testing.T) {
	src := `
version: 1
agent: codex
mcpServers:
  github:
    command: npx
    args: ["-y", "@modelcontextprotocol/server-github"]
    version: "0.6.2"
    agents: [claude-code, codex]
hooks:
  gofmt:
    event: PostToolUse
    command: gofmt -w .
    agents: [claude-code]
tools:
  builtins:
    disabled: [WebFetch]
  agents: [claude-code]
`
	var m Manifest
	dec := yaml.NewDecoder(strings.NewReader(src))
	dec.KnownFields(true)
	if err := dec.Decode(&m); err != nil {
		t.Fatalf("strict decode rejected the agents field: %v", err)
	}
	if got := m.MCPServers["github"].Agents; len(got) != 2 {
		t.Errorf("mcpServers.github.agents = %v, want 2 entries", got)
	}
	if got := m.Hooks["gofmt"].Agents; len(got) != 1 || got[0] != "claude-code" {
		t.Errorf("hooks.gofmt.agents = %v, want [claude-code]", got)
	}
	if got := m.Tools.Agents; len(got) != 1 || got[0] != "claude-code" {
		t.Errorf("tools.agents = %v, want [claude-code]", got)
	}
}
