package resolve

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRenderResources(t *testing.T) {
	dir := t.TempDir()

	// Create source files for command and rule.
	cmdContent := []byte("# ship command\nFast-path merge to the default branch.")
	ruleContent := []byte("# Team rules\nFollow PSR-12.")
	if err := os.WriteFile(filepath.Join(dir, "ship.md"), cmdContent, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "team.md"), ruleContent, 0o644); err != nil {
		t.Fatal(err)
	}

	manifestYAML := `version: 1
mcpServers:
  github:
    command: npx
    args: ["-y", "@modelcontextprotocol/server-github"]
    version: "2025.4.0"
    transport: stdio
    env:
      GITHUB_TOKEN: "token123"
hooks:
  guard:
    event: PreToolUse
    matcher: "Edit|Write"
    command: "node guard.js"
    timeout: 5000
commands:
  ship:
    source: ./ship.md
    description: Fast-path merge to the default branch.
rules:
  team:
    target: CLAUDE.md
    source: ./team.md
`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(manifestYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	resources, err := RenderResources(dir)
	if err != nil {
		t.Fatalf("RenderResources: %v", err)
	}

	// Check mcpServers channel.
	mcpResources, ok := resources["mcpServers"]
	if !ok {
		t.Fatal("missing mcpServers channel")
	}
	if len(mcpResources) != 1 {
		t.Fatalf("mcpServers: got %d resources, want 1", len(mcpResources))
	}
	mcpRes := mcpResources[0]
	if mcpRes.ID != "github" {
		t.Errorf("mcpServers[0].ID = %q, want %q", mcpRes.ID, "github")
	}
	if mcpRes.Channel != "mcpServers" {
		t.Errorf("mcpServers[0].Channel = %q, want %q", mcpRes.Channel, "mcpServers")
	}
	if mcpRes.ContentHash == "" {
		t.Error("mcpServers[0].ContentHash is empty")
	}
	if got, ok := mcpRes.Payload["command"]; !ok || got != "npx" {
		t.Errorf("mcpServers[0].Payload[command] = %v, want %q", got, "npx")
	}
	if got, ok := mcpRes.Payload["transport"]; !ok || got != "stdio" {
		t.Errorf("mcpServers[0].Payload[transport] = %v, want %q", got, "stdio")
	}

	// Check hooks channel.
	hookResources, ok := resources["hooks"]
	if !ok {
		t.Fatal("missing hooks channel")
	}
	if len(hookResources) != 1 {
		t.Fatalf("hooks: got %d resources, want 1", len(hookResources))
	}
	hookRes := hookResources[0]
	if hookRes.ID != "guard" {
		t.Errorf("hooks[0].ID = %q, want %q", hookRes.ID, "guard")
	}
	if got, ok := hookRes.Payload["event"]; !ok || got != "PreToolUse" {
		t.Errorf("hooks[0].Payload[event] = %v, want %q", got, "PreToolUse")
	}
	if got, ok := hookRes.Payload["command"]; !ok || got != "node guard.js" {
		t.Errorf("hooks[0].Payload[command] = %v, want %q", got, "node guard.js")
	}

	// Check commands channel — Payload["content"] must equal the source file bytes.
	cmdResources, ok := resources["commands"]
	if !ok {
		t.Fatal("missing commands channel")
	}
	if len(cmdResources) != 1 {
		t.Fatalf("commands: got %d resources, want 1", len(cmdResources))
	}
	cmdRes := cmdResources[0]
	if cmdRes.ID != "ship" {
		t.Errorf("commands[0].ID = %q, want %q", cmdRes.ID, "ship")
	}
	gotContent, ok := cmdRes.Payload["content"]
	if !ok {
		t.Fatal("commands[0].Payload missing content key")
	}
	if gotContent.(string) != string(cmdContent) {
		t.Errorf("commands[0].Payload[content] = %q, want %q", gotContent, cmdContent)
	}

	// Check rules channel — Payload["target"] and Payload["content"].
	ruleResources, ok := resources["rules"]
	if !ok {
		t.Fatal("missing rules channel")
	}
	if len(ruleResources) != 1 {
		t.Fatalf("rules: got %d resources, want 1", len(ruleResources))
	}
	ruleRes := ruleResources[0]
	if ruleRes.ID != "team" {
		t.Errorf("rules[0].ID = %q, want %q", ruleRes.ID, "team")
	}
	if got, ok := ruleRes.Payload["target"]; !ok || got != "CLAUDE.md" {
		t.Errorf("rules[0].Payload[target] = %v, want %q", got, "CLAUDE.md")
	}
	gotRuleContent, ok := ruleRes.Payload["content"]
	if !ok {
		t.Fatal("rules[0].Payload missing content key")
	}
	if gotRuleContent.(string) != string(ruleContent) {
		t.Errorf("rules[0].Payload[content] = %q, want %q", gotRuleContent, ruleContent)
	}
}
