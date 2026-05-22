package resolve

import (
	"os"
	"path/filepath"
	"slices"
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

func TestPinPackageVersion(t *testing.T) {
	cases := []struct {
		name    string
		command string
		args    []string
		version string
		want    []string
	}{
		{"scoped npx", "npx", []string{"-y", "@upstash/context7-mcp"}, "2.3.0", []string{"-y", "@upstash/context7-mcp@2.3.0"}},
		{"unscoped npx", "npx", []string{"-y", "chrome-devtools-mcp"}, "1.0.1", []string{"-y", "chrome-devtools-mcp@1.0.1"}},
		{"uvx first arg", "uvx", []string{"meta-ads-mcp"}, "1.0.0", []string{"meta-ads-mcp@1.0.0"}},
		{"already versioned", "npx", []string{"-y", "@scope/pkg@9.9.9"}, "1.0.0", []string{"-y", "@scope/pkg@9.9.9"}},
		{"not a launcher", "metabase-server", []string{"x"}, "1.0.0", []string{"x"}},
		{"no version", "npx", []string{"-y", "pkg"}, "", []string{"-y", "pkg"}},
		{"empty args", "npx", nil, "1.0.0", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := pinPackageVersion(c.command, c.args, c.version)
			if !slices.Equal(got, c.want) {
				t.Errorf("pinPackageVersion(%q, %v, %q) = %v, want %v", c.command, c.args, c.version, got, c.want)
			}
		})
	}
}

func TestRenderResourcesMarketplacesAndPlugins(t *testing.T) {
	dir := t.TempDir()
	manifestYAML := `version: 1
marketplaces:
  my-org: { source: "github:my-org/plugins" }
plugins:
  my-plugin:
    marketplace: my-org
    version: "1.2.3"
`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(manifestYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	resources, err := RenderResources(dir)
	if err != nil {
		t.Fatalf("RenderResources: %v", err)
	}

	// Check marketplaces channel.
	mpResources, ok := resources["marketplaces"]
	if !ok {
		t.Fatal("missing marketplaces channel")
	}
	if len(mpResources) != 1 {
		t.Fatalf("marketplaces: got %d resources, want 1", len(mpResources))
	}
	mpRes := mpResources[0]
	if mpRes.ID != "my-org" {
		t.Errorf("marketplaces[0].ID = %q, want my-org", mpRes.ID)
	}
	if mpRes.Channel != "marketplaces" {
		t.Errorf("marketplaces[0].Channel = %q, want marketplaces", mpRes.Channel)
	}
	if mpRes.ContentHash == "" {
		t.Error("marketplaces[0].ContentHash is empty")
	}
	if got, ok := mpRes.Payload["source"]; !ok || got != "github:my-org/plugins" {
		t.Errorf("marketplaces[0].Payload[source] = %v, want github:my-org/plugins", got)
	}

	// Check plugins channel carries marketplace not source.
	pluginResources, ok := resources["plugins"]
	if !ok {
		t.Fatal("missing plugins channel")
	}
	if len(pluginResources) != 1 {
		t.Fatalf("plugins: got %d resources, want 1", len(pluginResources))
	}
	pRes := pluginResources[0]
	if pRes.ID != "my-plugin" {
		t.Errorf("plugins[0].ID = %q, want my-plugin", pRes.ID)
	}
	if got, ok := pRes.Payload["marketplace"]; !ok || got != "my-org" {
		t.Errorf("plugins[0].Payload[marketplace] = %v, want my-org", got)
	}
	if got, ok := pRes.Payload["version"]; !ok || got != "1.2.3" {
		t.Errorf("plugins[0].Payload[version] = %v, want 1.2.3", got)
	}
	if _, hasSource := pRes.Payload["source"]; hasSource {
		t.Error("plugins[0].Payload should not contain 'source' key")
	}
}

func TestRenderResources_EnabledFalse(t *testing.T) {
	dir := t.TempDir()
	manifestYAML := `version: 1
mcpServers:
  active-server:
    command: npx
    args: ["-y", "pkg"]
    version: "1.0.0"
  disabled-server:
    command: npx
    args: ["-y", "other"]
    version: "1.0.0"
    enabled: false
`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(manifestYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	resources, err := RenderResources(dir)
	if err != nil {
		t.Fatalf("RenderResources: %v", err)
	}

	mcp := resources["mcpServers"]
	if len(mcp) != 1 {
		t.Fatalf("got %d mcpServers, want 1 (disabled one omitted)", len(mcp))
	}
	if mcp[0].ID != "active-server" {
		t.Errorf("rendered server = %q, want active-server", mcp[0].ID)
	}
	// The pinned version must be applied to the launch args.
	args, ok := mcp[0].Payload["args"].([]string)
	if !ok || !slices.Equal(args, []string{"-y", "pkg@1.0.0"}) {
		t.Errorf("active-server args = %v, want [-y pkg@1.0.0]", mcp[0].Payload["args"])
	}
}
