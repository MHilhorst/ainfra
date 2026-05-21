package channels_test

import (
	"encoding/json"
	"testing"

	"github.com/MHilhorst/ainfra/internal/provider"
	"github.com/MHilhorst/ainfra/internal/provider/channels"
)

func TestHooksChannel(t *testing.T) {
	p := channels.Hooks{}
	if got := p.Channel(); got != "hooks" {
		t.Fatalf("Channel() = %q, want %q", got, "hooks")
	}
}

func TestHooksObserve_Empty(t *testing.T) {
	mem := provider.NewMemFilesystem()
	env := provider.Env{FS: mem, Root: "/repo"}

	p := channels.Hooks{}
	resources, err := p.Observe(env)
	if err != nil {
		t.Fatalf("Observe: unexpected error: %v", err)
	}
	if len(resources) != 0 {
		t.Fatalf("Observe: got %d resources, want 0", len(resources))
	}
}

func TestHooksObserve_WithHooks(t *testing.T) {
	mem := provider.NewMemFilesystem()
	env := provider.Env{FS: mem, Root: "/repo"}

	settingsJSON := `{"hooks":{"on-save":{"event":"PostToolUse","command":"echo saved"},"on-start":{"event":"SessionStart","command":"echo start"}}}`
	if err := mem.WriteFile("/repo/.claude/settings.json", []byte(settingsJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	p := channels.Hooks{}
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
		if r.Channel != "hooks" {
			t.Errorf("resource %q: Channel = %q, want %q", r.ID, r.Channel, "hooks")
		}
		if r.ContentHash != "" {
			t.Errorf("resource %q: ContentHash should be empty, got %q", r.ID, r.ContentHash)
		}
	}
	if !ids["on-save"] {
		t.Error("expected resource with id 'on-save'")
	}
	if !ids["on-start"] {
		t.Error("expected resource with id 'on-start'")
	}
}

func TestHooksApply_Create(t *testing.T) {
	mem := provider.NewMemFilesystem()
	existing := `{"hooks":{"foreign-hook":{"event":"SessionStart","command":"echo foreign"}}}`
	if err := mem.WriteFile("/repo/.claude/settings.json", []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	env := provider.Env{FS: mem, Root: "/repo"}

	plan := provider.ChannelPlan{
		Channel: "hooks",
		Changes: []provider.Change{
			{
				Kind: provider.ChangeCreate,
				ID:   "lint-hook",
				Resource: provider.Resource{
					ID:      "lint-hook",
					Channel: "hooks",
					Payload: map[string]any{
						"event":   "PostToolUse",
						"matcher": "Edit",
						"command": "golangci-lint run",
						"timeout": 30,
					},
				},
			},
		},
	}

	p := channels.Hooks{}
	result, err := p.Apply(env, plan)
	if err != nil {
		t.Fatalf("Apply: unexpected error: %v", err)
	}
	if result.Channel != "hooks" {
		t.Errorf("result.Channel = %q, want %q", result.Channel, "hooks")
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
	hooks, ok := doc["hooks"].(map[string]any)
	if !ok {
		t.Fatal("hooks not a map")
	}
	if _, ok := hooks["foreign-hook"]; !ok {
		t.Error("foreign-hook was removed, should have been preserved")
	}
	hook, ok := hooks["lint-hook"].(map[string]any)
	if !ok {
		t.Fatal("lint-hook not present or not a map")
	}
	if hook["event"] != "PostToolUse" {
		t.Errorf("event = %v, want %q", hook["event"], "PostToolUse")
	}
	if hook["matcher"] != "Edit" {
		t.Errorf("matcher = %v, want %q", hook["matcher"], "Edit")
	}
	if hook["command"] != "golangci-lint run" {
		t.Errorf("command = %v, want %q", hook["command"], "golangci-lint run")
	}
	// timeout stored as number; json.Unmarshal produces float64
	if hook["timeout"] == nil {
		t.Error("timeout should be present")
	}
}

func TestHooksApply_Create_EmptyMatcher(t *testing.T) {
	mem := provider.NewMemFilesystem()
	env := provider.Env{FS: mem, Root: "/repo"}

	plan := provider.ChannelPlan{
		Channel: "hooks",
		Changes: []provider.Change{
			{
				Kind: provider.ChangeCreate,
				ID:   "session-hook",
				Resource: provider.Resource{
					ID:      "session-hook",
					Channel: "hooks",
					Payload: map[string]any{
						"event":   "SessionStart",
						"command": "echo hi",
					},
				},
			},
		},
	}

	p := channels.Hooks{}
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
	hooks := doc["hooks"].(map[string]any)
	hook := hooks["session-hook"].(map[string]any)
	if _, ok := hook["matcher"]; ok {
		t.Error("matcher should be omitted when empty")
	}
}

func TestHooksApply_Delete(t *testing.T) {
	mem := provider.NewMemFilesystem()
	existing := `{"hooks":{"managed-hook":{"event":"PostToolUse","command":"echo managed"},"foreign-hook":{"event":"SessionStart","command":"echo foreign"}}}`
	if err := mem.WriteFile("/repo/.claude/settings.json", []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	env := provider.Env{FS: mem, Root: "/repo"}

	plan := provider.ChannelPlan{
		Channel: "hooks",
		Changes: []provider.Change{
			{
				Kind: provider.ChangeDelete,
				ID:   "managed-hook",
				Resource: provider.Resource{
					ID:      "managed-hook",
					Channel: "hooks",
				},
			},
		},
	}

	p := channels.Hooks{}
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
	hooks, ok := doc["hooks"].(map[string]any)
	if !ok {
		t.Fatal("hooks not a map")
	}
	if _, ok := hooks["managed-hook"]; ok {
		t.Error("managed-hook should have been deleted")
	}
	if _, ok := hooks["foreign-hook"]; !ok {
		t.Error("foreign-hook was removed, should have been preserved")
	}
}

func TestHooksApply_DryRun(t *testing.T) {
	mem := provider.NewMemFilesystem()
	original := `{"hooks":{"existing":{"event":"SessionStart","command":"echo hi"}}}`
	if err := mem.WriteFile("/repo/.claude/settings.json", []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	env := provider.Env{FS: mem, Root: "/repo", DryRun: true}

	plan := provider.ChannelPlan{
		Channel: "hooks",
		Changes: []provider.Change{
			{
				Kind: provider.ChangeCreate,
				ID:   "new-hook",
				Resource: provider.Resource{
					ID:      "new-hook",
					Channel: "hooks",
					Payload: map[string]any{
						"event":   "PostToolUse",
						"command": "echo new",
					},
				},
			},
		},
	}

	p := channels.Hooks{}
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
