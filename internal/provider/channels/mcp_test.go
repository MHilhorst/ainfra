package channels_test

import (
	"encoding/json"
	"testing"

	"github.com/MHilhorst/ainfra/internal/provider"
	"github.com/MHilhorst/ainfra/internal/provider/channels"
)

func TestMCPChannel(t *testing.T) {
	p := channels.MCP{}
	if got := p.Channel(); got != "mcpServers" {
		t.Fatalf("Channel() = %q, want %q", got, "mcpServers")
	}
}

func TestMCPObserve_Empty(t *testing.T) {
	mem := provider.NewMemFilesystem()
	env := provider.Env{FS: mem, Root: "/repo"}

	p := channels.MCP{}
	resources, err := p.Observe(env)
	if err != nil {
		t.Fatalf("Observe: unexpected error: %v", err)
	}
	if len(resources) != 0 {
		t.Fatalf("Observe: got %d resources, want 0", len(resources))
	}
}

func TestMCPObserve_WithServers(t *testing.T) {
	mem := provider.NewMemFilesystem()
	env := provider.Env{FS: mem, Root: "/repo"}

	mcpJSON := `{"mcpServers":{"a":{"command":"cmd-a"},"foreign":{"command":"cmd-f"}}}`
	if err := mem.WriteFile("/repo/.mcp.json", []byte(mcpJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	p := channels.MCP{}
	resources, err := p.Observe(env)
	if err != nil {
		t.Fatalf("Observe: unexpected error: %v", err)
	}
	if len(resources) != 2 {
		t.Fatalf("Observe: got %d resources, want 2", len(resources))
	}

	ids := map[string]bool{}
	for _, r := range resources {
		ids[r.ID] = true
		if r.Channel != "mcpServers" {
			t.Errorf("resource %q: Channel = %q, want %q", r.ID, r.Channel, "mcpServers")
		}
		if r.ContentHash != "" {
			t.Errorf("resource %q: ContentHash should be empty, got %q", r.ID, r.ContentHash)
		}
	}
	if !ids["a"] {
		t.Error("expected resource with id 'a'")
	}
	if !ids["foreign"] {
		t.Error("expected resource with id 'foreign'")
	}
}

func TestMCPApply_Create(t *testing.T) {
	mem := provider.NewMemFilesystem()
	// pre-populate with a foreign key
	existing := `{"mcpServers":{"foreign":{"command":"foreign-cmd"}}}`
	if err := mem.WriteFile("/repo/.mcp.json", []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	env := provider.Env{FS: mem, Root: "/repo"}

	plan := provider.ChannelPlan{
		Channel: "mcpServers",
		Changes: []provider.Change{
			{
				Kind: provider.ChangeCreate,
				ID:   "myserver",
				Resource: provider.Resource{
					ID:      "myserver",
					Channel: "mcpServers",
					Payload: map[string]any{
						"command":   "npx",
						"args":      []any{"-y", "@modelcontextprotocol/server-everything"},
						"env":       map[string]any{"API_KEY": "secret"},
						"transport": "stdio",
					},
				},
			},
		},
	}

	p := channels.MCP{}
	result, err := p.Apply(env, plan)
	if err != nil {
		t.Fatalf("Apply: unexpected error: %v", err)
	}
	if result.Channel != "mcpServers" {
		t.Errorf("result.Channel = %q, want %q", result.Channel, "mcpServers")
	}
	if len(result.Applied) != 1 {
		t.Fatalf("result.Applied: got %d, want 1", len(result.Applied))
	}

	raw, err := mem.ReadFile("/repo/.mcp.json")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	servers, ok := doc["mcpServers"].(map[string]any)
	if !ok {
		t.Fatal("mcpServers not a map")
	}
	if _, ok := servers["foreign"]; !ok {
		t.Error("foreign key was removed, should have been preserved")
	}
	myserver, ok := servers["myserver"].(map[string]any)
	if !ok {
		t.Fatal("myserver not present or not a map")
	}
	if myserver["command"] != "npx" {
		t.Errorf("command = %v, want %q", myserver["command"], "npx")
	}
	if myserver["type"] != "stdio" {
		t.Errorf("type = %v, want %q", myserver["type"], "stdio")
	}
}

func TestMCPApply_Delete(t *testing.T) {
	mem := provider.NewMemFilesystem()
	existing := `{"mcpServers":{"myserver":{"command":"npx"},"foreign":{"command":"other"}}}`
	if err := mem.WriteFile("/repo/.mcp.json", []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	env := provider.Env{FS: mem, Root: "/repo"}

	plan := provider.ChannelPlan{
		Channel: "mcpServers",
		Changes: []provider.Change{
			{
				Kind: provider.ChangeDelete,
				ID:   "myserver",
				Resource: provider.Resource{
					ID:      "myserver",
					Channel: "mcpServers",
				},
			},
		},
	}

	p := channels.MCP{}
	_, err := p.Apply(env, plan)
	if err != nil {
		t.Fatalf("Apply: unexpected error: %v", err)
	}

	raw, err := mem.ReadFile("/repo/.mcp.json")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	servers, ok := doc["mcpServers"].(map[string]any)
	if !ok {
		t.Fatal("mcpServers not a map")
	}
	if _, ok := servers["myserver"]; ok {
		t.Error("myserver should have been deleted")
	}
	if _, ok := servers["foreign"]; !ok {
		t.Error("foreign key was removed, should have been preserved")
	}
}

func TestMCPApply_NoopLeavesFileIdentical(t *testing.T) {
	mem := provider.NewMemFilesystem()
	original := `{"mcpServers":{"existing":{"command":"cmd"}}}`
	if err := mem.WriteFile("/repo/.mcp.json", []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	env := provider.Env{FS: mem, Root: "/repo"}

	// All noop changes — ownedKeys will be empty after filtering.
	plan := provider.ChannelPlan{
		Channel: "mcpServers",
		Changes: []provider.Change{
			{
				Kind: provider.ChangeNoop,
				ID:   "existing",
				Resource: provider.Resource{
					ID:      "existing",
					Channel: "mcpServers",
					Payload: map[string]any{"command": "cmd"},
				},
			},
		},
	}

	before, _ := mem.ReadFile("/repo/.mcp.json")

	p := channels.MCP{}
	result, err := p.Apply(env, plan)
	if err != nil {
		t.Fatalf("Apply: unexpected error: %v", err)
	}
	if len(result.Applied) != 0 {
		t.Errorf("noop plan: expected 0 applied changes, got %d", len(result.Applied))
	}

	after, _ := mem.ReadFile("/repo/.mcp.json")
	if string(before) != string(after) {
		t.Errorf("noop plan modified the file: before=%q after=%q", before, after)
	}
}

func TestMCPApply_DryRun(t *testing.T) {
	mem := provider.NewMemFilesystem()
	original := `{"mcpServers":{"existing":{"command":"cmd"}}}`
	if err := mem.WriteFile("/repo/.mcp.json", []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	env := provider.Env{FS: mem, Root: "/repo", DryRun: true}

	plan := provider.ChannelPlan{
		Channel: "mcpServers",
		Changes: []provider.Change{
			{
				Kind: provider.ChangeCreate,
				ID:   "newserver",
				Resource: provider.Resource{
					ID:      "newserver",
					Channel: "mcpServers",
					Payload: map[string]any{"command": "new-cmd"},
				},
			},
		},
	}

	p := channels.MCP{}
	result, err := p.Apply(env, plan)
	if err != nil {
		t.Fatalf("Apply: unexpected error: %v", err)
	}
	if len(result.Applied) != 1 {
		t.Fatalf("DryRun: expected 1 applied change described, got %d", len(result.Applied))
	}

	// file must not have been modified
	raw, _ := mem.ReadFile("/repo/.mcp.json")
	if string(raw) != original {
		t.Error("DryRun: file was modified, should not have been")
	}
}
