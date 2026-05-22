package codex_test

import (
	"strings"
	"testing"

	"github.com/MHilhorst/ainfra/internal/provider"
	"github.com/MHilhorst/ainfra/internal/provider/codex"
)

func TestMCPChannel(t *testing.T) {
	if got := (codex.MCP{}).Channel(); got != "mcpServers" {
		t.Fatalf("Channel() = %q, want mcpServers", got)
	}
}

func TestMCPObserveEmpty(t *testing.T) {
	mem := provider.NewMemFilesystem()
	env := provider.Env{FS: mem, Home: "/home"}
	got, err := (codex.MCP{}).Observe(env)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("Observe: got %d resources, want 0", len(got))
	}
}

func TestMCPObserveWithServers(t *testing.T) {
	mem := provider.NewMemFilesystem()
	mem.WriteFile("/home/.codex/config.toml",
		[]byte("[mcp_servers.a]\ncommand = \"cmd-a\"\n\n[mcp_servers.foreign]\ncommand = \"cmd-f\"\n"), 0o644)
	env := provider.Env{FS: mem, Home: "/home"}
	got, err := (codex.MCP{}).Observe(env)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	ids := map[string]bool{}
	for _, r := range got {
		ids[r.ID] = true
		if r.Channel != "mcpServers" {
			t.Errorf("resource %q: Channel = %q", r.ID, r.Channel)
		}
	}
	if !ids["a"] || !ids["foreign"] {
		t.Errorf("ids = %v, want a and foreign", ids)
	}
}

func TestMCPApplyCreate(t *testing.T) {
	mem := provider.NewMemFilesystem()
	env := provider.Env{FS: mem, Home: "/home"}
	plan := provider.ChannelPlan{
		Channel: "mcpServers",
		Changes: []provider.Change{{
			Kind: provider.ChangeCreate,
			ID:   "github",
			Resource: provider.Resource{
				ID:      "github",
				Channel: "mcpServers",
				Payload: map[string]any{
					"command":   "npx",
					"args":      []any{"-y", "server-github"},
					"env":       map[string]any{"TOKEN": "x"},
					"transport": "stdio",
				},
			},
		}},
	}
	result, err := (codex.MCP{}).Apply(env, plan)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(result.Applied) != 1 {
		t.Fatalf("Applied = %d, want 1", len(result.Applied))
	}
	out := string(mem.Files["/home/.codex/config.toml"])
	if !strings.Contains(out, "[mcp_servers.github]") {
		t.Errorf("missing table:\n%s", out)
	}
	if !strings.Contains(out, `command = "npx"`) {
		t.Errorf("missing command:\n%s", out)
	}
	if strings.Contains(out, "transport") || strings.Contains(out, "stdio") {
		t.Errorf("transport must not be written for codex:\n%s", out)
	}
}

func TestMCPApplyDryRunWritesNothing(t *testing.T) {
	mem := provider.NewMemFilesystem()
	env := provider.Env{FS: mem, Home: "/home", DryRun: true}
	plan := provider.ChannelPlan{
		Channel: "mcpServers",
		Changes: []provider.Change{{
			Kind:     provider.ChangeCreate,
			ID:       "github",
			Resource: provider.Resource{ID: "github", Channel: "mcpServers", Payload: map[string]any{"command": "npx"}},
		}},
	}
	if _, err := (codex.MCP{}).Apply(env, plan); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if _, ok := mem.Files["/home/.codex/config.toml"]; ok {
		t.Error("DryRun must not write the file")
	}
}
