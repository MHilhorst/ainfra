package claudecode_test

import (
	"encoding/json"
	"testing"

	"github.com/MHilhorst/ainfra/internal/provider"
	"github.com/MHilhorst/ainfra/internal/provider/claudecode"
)

func TestHooksChannel(t *testing.T) {
	p := claudecode.Hooks{}
	if got := p.Channel(); got != "hooks" {
		t.Fatalf("Channel() = %q, want %q", got, "hooks")
	}
}

// Observe always returns nil — the event-keyed settings.json schema cannot be
// mapped back to individual managed hooks, so hooks reconcile wholesale.
func TestHooksObserve_AlwaysNil(t *testing.T) {
	mem := provider.NewMemFilesystem()
	env := provider.Env{FS: mem, Root: "/repo"}
	settingsJSON := `{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"echo hi"}]}]}}`
	if err := mem.WriteFile("/repo/.claude/settings.json", []byte(settingsJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	resources, err := claudecode.Hooks{}.Observe(env)
	if err != nil {
		t.Fatalf("Observe: unexpected error: %v", err)
	}
	if resources != nil {
		t.Fatalf("Observe: got %v, want nil", resources)
	}
}

// hooksDoc reads settings.json and returns the parsed "hooks" object.
func hooksDoc(t *testing.T, mem *provider.MemFilesystem) map[string]any {
	t.Helper()
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
		t.Fatalf("hooks not an object: %v", doc["hooks"])
	}
	return hooks
}

// firstGroup returns the single matcher group for an event, asserting the
// event holds an array with exactly one group.
func firstGroup(t *testing.T, hooks map[string]any, event string) map[string]any {
	t.Helper()
	arr, ok := hooks[event].([]any)
	if !ok {
		t.Fatalf("hooks.%s is not an array: %v", event, hooks[event])
	}
	if len(arr) != 1 {
		t.Fatalf("hooks.%s: got %d groups, want 1", event, len(arr))
	}
	group, ok := arr[0].(map[string]any)
	if !ok {
		t.Fatalf("hooks.%s[0] is not an object", event)
	}
	return group
}

func TestHooksApply_Create(t *testing.T) {
	mem := provider.NewMemFilesystem()
	// A non-hooks key and a foreign event must both survive.
	existing := `{"permissions":{"allow":["X"]},"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"echo foreign"}]}]}}`
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
						"timeout": 30000,
					},
				},
			},
		},
	}

	result, err := claudecode.Hooks{}.Apply(env, plan)
	if err != nil {
		t.Fatalf("Apply: unexpected error: %v", err)
	}
	if result.Channel != "hooks" || len(result.Applied) != 1 {
		t.Fatalf("result = %+v, want channel hooks with 1 applied", result)
	}

	hooks := hooksDoc(t, mem)

	// Foreign event preserved (ainfra only owns PostToolUse here).
	if _, ok := hooks["SessionStart"]; !ok {
		t.Error("foreign SessionStart event was removed, should be preserved")
	}

	group := firstGroup(t, hooks, "PostToolUse")
	if group["matcher"] != "Edit" {
		t.Errorf("matcher = %v, want Edit", group["matcher"])
	}
	inner, ok := group["hooks"].([]any)
	if !ok || len(inner) != 1 {
		t.Fatalf("group.hooks = %v, want a 1-element array", group["hooks"])
	}
	hook := inner[0].(map[string]any)
	if hook["type"] != "command" {
		t.Errorf("hook.type = %v, want command", hook["type"])
	}
	if hook["command"] != "golangci-lint run" {
		t.Errorf("hook.command = %v, want %q", hook["command"], "golangci-lint run")
	}
	// 30000ms must be converted to 30 seconds.
	if hook["timeout"] != float64(30) {
		t.Errorf("hook.timeout = %v, want 30 (seconds)", hook["timeout"])
	}

	// The non-hooks key survives.
	doc := map[string]any{}
	raw, _ := mem.ReadFile("/repo/.claude/settings.json")
	_ = json.Unmarshal(raw, &doc)
	if _, ok := doc["permissions"]; !ok {
		t.Error("permissions key was removed, should be preserved")
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

	if _, err := (claudecode.Hooks{}).Apply(env, plan); err != nil {
		t.Fatalf("Apply: unexpected error: %v", err)
	}

	group := firstGroup(t, hooksDoc(t, mem), "SessionStart")
	if _, ok := group["matcher"]; ok {
		t.Error("matcher should be omitted when empty")
	}
}

func TestHooksApply_MultipleSameEvent(t *testing.T) {
	mem := provider.NewMemFilesystem()
	env := provider.Env{FS: mem, Root: "/repo"}

	mk := func(id, matcher string) provider.Change {
		return provider.Change{
			Kind: provider.ChangeCreate,
			ID:   id,
			Resource: provider.Resource{
				ID:      id,
				Channel: "hooks",
				Payload: map[string]any{"event": "PreToolUse", "matcher": matcher, "command": "echo " + id},
			},
		}
	}
	plan := provider.ChannelPlan{
		Channel: "hooks",
		Changes: []provider.Change{mk("a-guard", "Bash"), mk("b-guard", "Edit|Write")},
	}

	if _, err := (claudecode.Hooks{}).Apply(env, plan); err != nil {
		t.Fatalf("Apply: unexpected error: %v", err)
	}

	arr, ok := hooksDoc(t, mem)["PreToolUse"].([]any)
	if !ok || len(arr) != 2 {
		t.Fatalf("PreToolUse: got %v, want 2 matcher groups", hooksDoc(t, mem)["PreToolUse"])
	}
}

func TestHooksApply_InstallsScript(t *testing.T) {
	mem := provider.NewMemFilesystem()
	env := provider.Env{FS: mem, Root: "/repo"}

	plan := provider.ChannelPlan{
		Channel: "hooks",
		Changes: []provider.Change{
			{
				Kind: provider.ChangeCreate,
				ID:   "guard",
				Resource: provider.Resource{
					ID:      "guard",
					Channel: "hooks",
					Payload: map[string]any{
						"event":         "PreToolUse",
						"matcher":       "Bash",
						"command":       "bash .ainfra/run/guard.sh",
						"scriptName":    "guard.sh",
						"scriptContent": "#!/bin/sh\necho guard\n",
					},
				},
			},
		},
	}

	if _, err := (claudecode.Hooks{}).Apply(env, plan); err != nil {
		t.Fatalf("Apply: unexpected error: %v", err)
	}

	raw, err := mem.ReadFile("/repo/.ainfra/run/guard.sh")
	if err != nil {
		t.Fatalf("hook script not installed: %v", err)
	}
	if string(raw) != "#!/bin/sh\necho guard\n" {
		t.Errorf("installed script content = %q", string(raw))
	}
}

func TestHooksApply_NoopLeavesFileIdentical(t *testing.T) {
	mem := provider.NewMemFilesystem()
	original := `{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"echo hi"}]}]}}`
	if err := mem.WriteFile("/repo/.claude/settings.json", []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	env := provider.Env{FS: mem, Root: "/repo"}

	plan := provider.ChannelPlan{
		Channel: "hooks",
		Changes: []provider.Change{
			{Kind: provider.ChangeNoop, ID: "existing", Resource: provider.Resource{ID: "existing", Channel: "hooks"}},
		},
	}

	before, _ := mem.ReadFile("/repo/.claude/settings.json")
	result, err := claudecode.Hooks{}.Apply(env, plan)
	if err != nil {
		t.Fatalf("Apply: unexpected error: %v", err)
	}
	if len(result.Applied) != 0 {
		t.Errorf("noop plan: expected 0 applied changes, got %d", len(result.Applied))
	}
	after, _ := mem.ReadFile("/repo/.claude/settings.json")
	if string(before) != string(after) {
		t.Errorf("noop plan modified the file: before=%q after=%q", before, after)
	}
}

func TestHooksApply_DryRun(t *testing.T) {
	mem := provider.NewMemFilesystem()
	original := `{"hooks":{}}`
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
					Payload: map[string]any{"event": "PostToolUse", "command": "echo new"},
				},
			},
		},
	}

	result, err := claudecode.Hooks{}.Apply(env, plan)
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
