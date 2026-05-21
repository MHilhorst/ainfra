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
