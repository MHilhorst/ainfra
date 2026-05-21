package claudecode_test

import (
	"encoding/json"
	"testing"

	"github.com/MHilhorst/ainfra/internal/provider"
	"github.com/MHilhorst/ainfra/internal/provider/claudecode"
)

func TestPluginsChannel(t *testing.T) {
	p := claudecode.Plugins{}
	if got := p.Channel(); got != "plugins" {
		t.Fatalf("Channel() = %q, want %q", got, "plugins")
	}
}

func TestPluginsObserve_Empty(t *testing.T) {
	mem := provider.NewMemFilesystem()
	env := provider.Env{FS: mem, Root: "/repo"}

	p := claudecode.Plugins{}
	resources, err := p.Observe(env)
	if err != nil {
		t.Fatalf("Observe: unexpected error: %v", err)
	}
	if len(resources) != 0 {
		t.Fatalf("Observe: got %d resources, want 0", len(resources))
	}
}

func TestPluginsObserve_WithPlugins(t *testing.T) {
	mem := provider.NewMemFilesystem()
	env := provider.Env{FS: mem, Root: "/repo"}

	pluginsJSON := `{"plugins":{"plugin-a":{"source":"src-a","version":"v1"},"plugin-b":{"source":"src-b","version":"v2"}}}`
	if err := mem.WriteFile("/repo/.claude/ainfra/plugins.json", []byte(pluginsJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	p := claudecode.Plugins{}
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
		if r.Channel != "plugins" {
			t.Errorf("resource %q: Channel = %q, want %q", r.ID, r.Channel, "plugins")
		}
	}
	if !ids["plugin-a"] {
		t.Error("expected resource with id 'plugin-a'")
	}
	if !ids["plugin-b"] {
		t.Error("expected resource with id 'plugin-b'")
	}
}

func TestPluginsApply_Create(t *testing.T) {
	mem := provider.NewMemFilesystem()
	// pre-populate with a foreign plugin entry
	existing := `{"plugins":{"foreign-plugin":{"source":"foreign-src","version":"v0"}}}`
	if err := mem.WriteFile("/repo/.claude/ainfra/plugins.json", []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	env := provider.Env{FS: mem, Root: "/repo"}

	plan := provider.ChannelPlan{
		Channel: "plugins",
		Changes: []provider.Change{
			{
				Kind: provider.ChangeCreate,
				ID:   "my-plugin",
				Resource: provider.Resource{
					ID:      "my-plugin",
					Channel: "plugins",
					Payload: map[string]any{
						"source":  "my-src",
						"version": "v1.0",
					},
				},
			},
		},
	}

	p := claudecode.Plugins{}
	result, err := p.Apply(env, plan)
	if err != nil {
		t.Fatalf("Apply: unexpected error: %v", err)
	}
	if result.Channel != "plugins" {
		t.Errorf("result.Channel = %q, want %q", result.Channel, "plugins")
	}
	if len(result.Applied) != 1 {
		t.Fatalf("result.Applied: got %d, want 1", len(result.Applied))
	}

	raw, err := mem.ReadFile("/repo/.claude/ainfra/plugins.json")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	plugins, ok := doc["plugins"].(map[string]any)
	if !ok {
		t.Fatal("plugins not a map")
	}
	if _, ok := plugins["foreign-plugin"]; !ok {
		t.Error("foreign-plugin was removed, should have been preserved")
	}
	myPlugin, ok := plugins["my-plugin"].(map[string]any)
	if !ok {
		t.Fatal("my-plugin not present or not a map")
	}
	if myPlugin["source"] != "my-src" {
		t.Errorf("source = %v, want %q", myPlugin["source"], "my-src")
	}
	if myPlugin["version"] != "v1.0" {
		t.Errorf("version = %v, want %q", myPlugin["version"], "v1.0")
	}
}

func TestPluginsApply_Delete(t *testing.T) {
	mem := provider.NewMemFilesystem()
	existing := `{"plugins":{"my-plugin":{"source":"my-src","version":"v1"},"foreign-plugin":{"source":"foreign-src","version":"v0"}}}`
	if err := mem.WriteFile("/repo/.claude/ainfra/plugins.json", []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	env := provider.Env{FS: mem, Root: "/repo"}

	plan := provider.ChannelPlan{
		Channel: "plugins",
		Changes: []provider.Change{
			{
				Kind: provider.ChangeDelete,
				ID:   "my-plugin",
				Resource: provider.Resource{
					ID:      "my-plugin",
					Channel: "plugins",
				},
			},
		},
	}

	p := claudecode.Plugins{}
	_, err := p.Apply(env, plan)
	if err != nil {
		t.Fatalf("Apply: unexpected error: %v", err)
	}

	raw, err := mem.ReadFile("/repo/.claude/ainfra/plugins.json")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	plugins, ok := doc["plugins"].(map[string]any)
	if !ok {
		t.Fatal("plugins not a map")
	}
	if _, ok := plugins["my-plugin"]; ok {
		t.Error("my-plugin should have been deleted")
	}
	if _, ok := plugins["foreign-plugin"]; !ok {
		t.Error("foreign-plugin was removed, should have been preserved")
	}
}

func TestPluginsApply_NoopLeavesFileIdentical(t *testing.T) {
	mem := provider.NewMemFilesystem()
	original := `{"plugins":{"existing-plugin":{"source":"src","version":"v1"}}}`
	if err := mem.WriteFile("/repo/.claude/ainfra/plugins.json", []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	env := provider.Env{FS: mem, Root: "/repo"}

	plan := provider.ChannelPlan{
		Channel: "plugins",
		Changes: []provider.Change{
			{
				Kind: provider.ChangeNoop,
				ID:   "existing-plugin",
				Resource: provider.Resource{
					ID:      "existing-plugin",
					Channel: "plugins",
					Payload: map[string]any{"source": "src", "version": "v1"},
				},
			},
		},
	}

	before, _ := mem.ReadFile("/repo/.claude/ainfra/plugins.json")

	p := claudecode.Plugins{}
	result, err := p.Apply(env, plan)
	if err != nil {
		t.Fatalf("Apply: unexpected error: %v", err)
	}
	if len(result.Applied) != 0 {
		t.Errorf("noop plan: expected 0 applied changes, got %d", len(result.Applied))
	}

	after, _ := mem.ReadFile("/repo/.claude/ainfra/plugins.json")
	if string(before) != string(after) {
		t.Errorf("noop plan modified the file: before=%q after=%q", before, after)
	}
}

func TestPluginsApply_DryRun(t *testing.T) {
	mem := provider.NewMemFilesystem()
	original := `{"plugins":{"existing-plugin":{"source":"src","version":"v1"}}}`
	if err := mem.WriteFile("/repo/.claude/ainfra/plugins.json", []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	env := provider.Env{FS: mem, Root: "/repo", DryRun: true}

	plan := provider.ChannelPlan{
		Channel: "plugins",
		Changes: []provider.Change{
			{
				Kind: provider.ChangeCreate,
				ID:   "new-plugin",
				Resource: provider.Resource{
					ID:      "new-plugin",
					Channel: "plugins",
					Payload: map[string]any{"source": "new-src", "version": "v2"},
				},
			},
		},
	}

	p := claudecode.Plugins{}
	result, err := p.Apply(env, plan)
	if err != nil {
		t.Fatalf("Apply: unexpected error: %v", err)
	}
	if len(result.Applied) != 1 {
		t.Fatalf("DryRun: expected 1 applied change described, got %d", len(result.Applied))
	}

	raw, _ := mem.ReadFile("/repo/.claude/ainfra/plugins.json")
	if string(raw) != original {
		t.Error("DryRun: file was modified, should not have been")
	}
}
