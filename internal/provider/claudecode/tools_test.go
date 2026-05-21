package claudecode_test

import (
	"encoding/json"
	"testing"

	"github.com/MHilhorst/ainfra/internal/provider"
	"github.com/MHilhorst/ainfra/internal/provider/claudecode"
)

func TestToolsChannel(t *testing.T) {
	p := claudecode.Tools{}
	if got := p.Channel(); got != "tools" {
		t.Fatalf("Channel() = %q, want %q", got, "tools")
	}
}

func TestToolsObserve_Empty(t *testing.T) {
	mem := provider.NewMemFilesystem()
	env := provider.Env{FS: mem, Root: "/repo"}

	p := claudecode.Tools{}
	resources, err := p.Observe(env)
	if err != nil {
		t.Fatalf("Observe: unexpected error: %v", err)
	}
	if len(resources) != 0 {
		t.Fatalf("Observe: got %d resources, want 0", len(resources))
	}
}

func TestToolsObserve_WithManagedKeys(t *testing.T) {
	mem := provider.NewMemFilesystem()
	env := provider.Env{FS: mem, Root: "/repo"}

	settingsJSON := `{"permissions":{"allow":["Bash"],"deny":[]},"disabledTools":["computer"]}`
	if err := mem.WriteFile("/repo/.claude/settings.json", []byte(settingsJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	p := claudecode.Tools{}
	resources, err := p.Observe(env)
	if err != nil {
		t.Fatalf("Observe: unexpected error: %v", err)
	}
	if len(resources) != 1 {
		t.Fatalf("Observe: got %d resources, want 1", len(resources))
	}
	if resources[0].ID != "tools" {
		t.Errorf("resource ID = %q, want %q", resources[0].ID, "tools")
	}
	if resources[0].Channel != "tools" {
		t.Errorf("resource Channel = %q, want %q", resources[0].Channel, "tools")
	}
}

func TestToolsObserve_NoManagedKeys(t *testing.T) {
	mem := provider.NewMemFilesystem()
	env := provider.Env{FS: mem, Root: "/repo"}

	settingsJSON := `{"hooks":{"some-hook":{"event":"SessionStart","command":"echo hi"}}}`
	if err := mem.WriteFile("/repo/.claude/settings.json", []byte(settingsJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	p := claudecode.Tools{}
	resources, err := p.Observe(env)
	if err != nil {
		t.Fatalf("Observe: unexpected error: %v", err)
	}
	if len(resources) != 0 {
		t.Fatalf("Observe: got %d resources, want 0 (no managed keys present)", len(resources))
	}
}

func TestToolsApply_Create(t *testing.T) {
	mem := provider.NewMemFilesystem()
	existing := `{"hooks":{"foreign":{"event":"SessionStart","command":"echo hi"}}}`
	if err := mem.WriteFile("/repo/.claude/settings.json", []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	env := provider.Env{FS: mem, Root: "/repo"}

	// Use []any as in previous tests — should still work.
	plan := provider.ChannelPlan{
		Channel: "tools",
		Changes: []provider.Change{
			{
				Kind: provider.ChangeCreate,
				ID:   "tools",
				Resource: provider.Resource{
					ID:      "tools",
					Channel: "tools",
					Payload: map[string]any{
						"disabled": []any{"computer", "bash"},
						"allow":    []any{"Read", "Write"},
						"deny":     []any{"Bash(rm *)"},
					},
				},
			},
		},
	}

	p := claudecode.Tools{}
	result, err := p.Apply(env, plan)
	if err != nil {
		t.Fatalf("Apply: unexpected error: %v", err)
	}
	if result.Channel != "tools" {
		t.Errorf("result.Channel = %q, want %q", result.Channel, "tools")
	}
	if len(result.Applied) != 1 {
		t.Fatalf("result.Applied: got %d, want 1", len(result.Applied))
	}

	raw, err := mem.ReadFile("/repo/.claude/settings.json")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	// foreign hook must be preserved
	if _, ok := doc["hooks"]; !ok {
		t.Error("hooks key was removed, should be preserved")
	}

	perms, ok := doc["permissions"].(map[string]any)
	if !ok {
		t.Fatal("permissions not a map")
	}
	if perms["allow"] == nil {
		t.Error("permissions.allow should be present")
	}
	if perms["deny"] == nil {
		t.Error("permissions.deny should be present")
	}

	if doc["disabledTools"] == nil {
		t.Error("disabledTools should be present")
	}
}

// TestToolsApply_Create_StringSlice exercises the []string path that the
// renderer actually produces (m.Tools.Builtins.Disabled is []string, not []any).
func TestToolsApply_Create_StringSlice(t *testing.T) {
	mem := provider.NewMemFilesystem()
	existing := `{"hooks":{"foreign":{"event":"SessionStart","command":"echo hi"}}}`
	if err := mem.WriteFile("/repo/.claude/settings.json", []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	env := provider.Env{FS: mem, Root: "/repo"}

	// disabled is []string — the type RenderResources actually puts in the payload.
	plan := provider.ChannelPlan{
		Channel: "tools",
		Changes: []provider.Change{
			{
				Kind: provider.ChangeCreate,
				ID:   "tools",
				Resource: provider.Resource{
					ID:      "tools",
					Channel: "tools",
					Payload: map[string]any{
						"disabled": []string{"WebSearch", "computer"},
						"allow":    []string{"Read", "Write"},
						"deny":     []string{"Bash(rm *)"},
					},
				},
			},
		},
	}

	p := claudecode.Tools{}
	result, err := p.Apply(env, plan)
	if err != nil {
		t.Fatalf("Apply: unexpected error: %v", err)
	}
	if len(result.Applied) != 1 {
		t.Fatalf("result.Applied: got %d, want 1", len(result.Applied))
	}

	raw, err := mem.ReadFile("/repo/.claude/settings.json")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if doc["disabledTools"] == nil {
		t.Error("disabledTools should be present when disabled is []string")
	}
	dt, ok := doc["disabledTools"].([]any)
	if !ok {
		t.Fatalf("disabledTools type = %T, want []any (JSON array)", doc["disabledTools"])
	}
	if len(dt) != 2 {
		t.Errorf("disabledTools length = %d, want 2", len(dt))
	}

	perms, ok := doc["permissions"].(map[string]any)
	if !ok {
		t.Fatal("permissions not a map")
	}
	if perms["allow"] == nil {
		t.Error("permissions.allow should be present")
	}
}

func TestToolsApply_Delete(t *testing.T) {
	mem := provider.NewMemFilesystem()
	existing := `{"permissions":{"allow":["Read"],"deny":[]},"disabledTools":["computer"],"hooks":{"f":{"event":"SessionStart","command":"echo hi"}}}`
	if err := mem.WriteFile("/repo/.claude/settings.json", []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	env := provider.Env{FS: mem, Root: "/repo"}

	plan := provider.ChannelPlan{
		Channel: "tools",
		Changes: []provider.Change{
			{
				Kind: provider.ChangeDelete,
				ID:   "tools",
				Resource: provider.Resource{
					ID:      "tools",
					Channel: "tools",
				},
			},
		},
	}

	p := claudecode.Tools{}
	_, err := p.Apply(env, plan)
	if err != nil {
		t.Fatalf("Apply: unexpected error: %v", err)
	}

	raw, err := mem.ReadFile("/repo/.claude/settings.json")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if _, ok := doc["permissions"]; ok {
		t.Error("permissions should have been removed")
	}
	if _, ok := doc["disabledTools"]; ok {
		t.Error("disabledTools should have been removed")
	}
	// foreign hook should be preserved
	if _, ok := doc["hooks"]; !ok {
		t.Error("hooks was removed, should be preserved")
	}
}

func TestToolsApply_DryRun(t *testing.T) {
	mem := provider.NewMemFilesystem()
	original := `{"hooks":{"existing":{"event":"SessionStart","command":"echo hi"}}}`
	if err := mem.WriteFile("/repo/.claude/settings.json", []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	env := provider.Env{FS: mem, Root: "/repo", DryRun: true}

	plan := provider.ChannelPlan{
		Channel: "tools",
		Changes: []provider.Change{
			{
				Kind: provider.ChangeCreate,
				ID:   "tools",
				Resource: provider.Resource{
					ID:      "tools",
					Channel: "tools",
					Payload: map[string]any{
						"disabled": []any{"computer"},
						"allow":    []any{"Read"},
						"deny":     []any{},
					},
				},
			},
		},
	}

	p := claudecode.Tools{}
	result, err := p.Apply(env, plan)
	if err != nil {
		t.Fatalf("Apply: unexpected error: %v", err)
	}
	if len(result.Applied) != 1 {
		t.Fatalf("DryRun: expected 1 applied change described, got %d", len(result.Applied))
	}

	raw, _ := mem.ReadFile("/repo/.claude/settings.json")
	if string(raw) != original {
		t.Error("DryRun: file was modified, should not have been")
	}
}
