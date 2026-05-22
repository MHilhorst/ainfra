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

func TestMCPObserve_Empty(t *testing.T) {
	mem := provider.NewMemFilesystem()
	env := provider.Env{FS: mem, Home: "/home"}
	got, err := (codex.MCP{}).Observe(env)
	if err != nil {
		t.Fatalf("Observe: unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("Observe: got %d resources, want 0", len(got))
	}
}

func TestMCPObserve_WithServers(t *testing.T) {
	mem := provider.NewMemFilesystem()
	if err := mem.WriteFile("/home/.codex/config.toml",
		[]byte("[mcp_servers.a]\ncommand = \"cmd-a\"\n\n[mcp_servers.foreign]\ncommand = \"cmd-f\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	env := provider.Env{FS: mem, Home: "/home"}
	got, err := (codex.MCP{}).Observe(env)
	if err != nil {
		t.Fatalf("Observe: unexpected error: %v", err)
	}
	ids := map[string]bool{}
	for _, r := range got {
		ids[r.ID] = true
		if r.Channel != "mcpServers" {
			t.Errorf("resource %q: Channel = %q, want mcpServers", r.ID, r.Channel)
		}
		if r.ContentHash != "" {
			t.Errorf("resource %q: ContentHash should be empty, got %q", r.ID, r.ContentHash)
		}
	}
	if !ids["a"] || !ids["foreign"] {
		t.Errorf("ids = %v, want a and foreign", ids)
	}
}

func TestMCPApply_Create(t *testing.T) {
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
		t.Fatalf("Apply: unexpected error: %v", err)
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

func TestMCPApply_Delete(t *testing.T) {
	mem := provider.NewMemFilesystem()
	if err := mem.WriteFile("/home/.codex/config.toml",
		[]byte("[mcp_servers.github]\ncommand = \"old\"\n\n[mcp_servers.foreign]\ncommand = \"keep\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	env := provider.Env{FS: mem, Home: "/home"}
	plan := provider.ChannelPlan{
		Channel: "mcpServers",
		Changes: []provider.Change{{
			Kind:     provider.ChangeDelete,
			ID:       "github",
			Resource: provider.Resource{ID: "github", Channel: "mcpServers"},
		}},
	}
	if _, err := (codex.MCP{}).Apply(env, plan); err != nil {
		t.Fatalf("Apply: unexpected error: %v", err)
	}
	out := string(mem.Files["/home/.codex/config.toml"])
	if strings.Contains(out, "[mcp_servers.github]") {
		t.Errorf("deleted server 'github' still present:\n%s", out)
	}
	if !strings.Contains(out, "[mcp_servers.foreign]") {
		t.Errorf("foreign server 'foreign' should be preserved:\n%s", out)
	}
}

func TestMCPApply_Noop(t *testing.T) {
	mem := provider.NewMemFilesystem()
	env := provider.Env{FS: mem, Home: "/home"}
	plan := provider.ChannelPlan{
		Channel: "mcpServers",
		Changes: []provider.Change{{
			Kind:     provider.ChangeNoop,
			ID:       "github",
			Resource: provider.Resource{ID: "github", Channel: "mcpServers"},
		}},
	}
	result, err := (codex.MCP{}).Apply(env, plan)
	if err != nil {
		t.Fatalf("Apply: unexpected error: %v", err)
	}
	if len(result.Applied) != 0 {
		t.Errorf("Applied = %d, want 0 for a noop-only plan", len(result.Applied))
	}
	if _, ok := mem.Files["/home/.codex/config.toml"]; ok {
		t.Error("a noop-only plan must not write the file")
	}
}

func TestMCPApply_DryRunWritesNothing(t *testing.T) {
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
		t.Fatalf("Apply: unexpected error: %v", err)
	}
	if _, ok := mem.Files["/home/.codex/config.toml"]; ok {
		t.Error("DryRun must not write the file")
	}
}
