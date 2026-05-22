package claudecode_test

import (
	"os"
	"strings"
	"testing"

	"github.com/MHilhorst/ainfra/internal/provider"
	"github.com/MHilhorst/ainfra/internal/provider/claudecode"
)

func TestRulesChannel(t *testing.T) {
	p := claudecode.Rules{}
	if got := p.Channel(); got != "rules" {
		t.Fatalf("Channel() = %q, want %q", got, "rules")
	}
}

func TestRulesObserve_MissingDir(t *testing.T) {
	mem := provider.NewMemFilesystem()
	env := provider.Env{FS: mem, Root: "/repo"}

	p := claudecode.Rules{}
	resources, err := p.Observe(env)
	if err != nil {
		t.Fatalf("Observe: unexpected error: %v", err)
	}
	if len(resources) != 0 {
		t.Fatalf("Observe: got %d resources, want 0", len(resources))
	}
}

func TestRulesObserve_ScansHomeDir(t *testing.T) {
	mem := provider.NewMemFilesystem()
	env := provider.Env{FS: mem, Root: "/repo", Home: "/home/dev"}

	// A "~"-target rule's fragment lives under Home, not Root.
	if err := mem.WriteFile("/home/dev/.claude/ainfra/team-claude-md.md", []byte("team rules"), 0o644); err != nil {
		t.Fatal(err)
	}

	resources, err := claudecode.Rules{}.Observe(env)
	if err != nil {
		t.Fatalf("Observe: unexpected error: %v", err)
	}
	if len(resources) != 1 || resources[0].ID != "team-claude-md" {
		t.Fatalf("Observe: got %v, want one resource id team-claude-md", resources)
	}
}

func TestRulesObserve_WithFiles(t *testing.T) {
	mem := provider.NewMemFilesystem()
	env := provider.Env{FS: mem, Root: "/repo"}

	if err := mem.MkdirAll("/repo/.claude/ainfra", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := mem.WriteFile("/repo/.claude/ainfra/no-todos.md", []byte("do not write TODO comments"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := mem.WriteFile("/repo/.claude/ainfra/be-concise.md", []byte("be concise"), 0o644); err != nil {
		t.Fatal(err)
	}

	p := claudecode.Rules{}
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
		if r.Channel != "rules" {
			t.Errorf("resource %q: Channel = %q, want %q", r.ID, r.Channel, "rules")
		}
		if r.ContentHash != "" {
			t.Errorf("resource %q: ContentHash should be empty, got %q", r.ID, r.ContentHash)
		}
	}
	if !ids["no-todos"] {
		t.Error("expected resource with id 'no-todos'")
	}
	if !ids["be-concise"] {
		t.Error("expected resource with id 'be-concise'")
	}
}

func TestRulesApply_Create(t *testing.T) {
	mem := provider.NewMemFilesystem()
	env := provider.Env{FS: mem, Root: "/repo"}

	plan := provider.ChannelPlan{
		Channel: "rules",
		Changes: []provider.Change{
			{
				Kind: provider.ChangeCreate,
				ID:   "no-todos",
				Resource: provider.Resource{
					ID:      "no-todos",
					Channel: "rules",
					Payload: map[string]any{
						"target":  "CLAUDE.md",
						"content": "do not write TODO comments",
					},
				},
			},
		},
	}

	p := claudecode.Rules{}
	result, err := p.Apply(env, plan)
	if err != nil {
		t.Fatalf("Apply: unexpected error: %v", err)
	}
	if result.Channel != "rules" {
		t.Errorf("result.Channel = %q, want %q", result.Channel, "rules")
	}
	if len(result.Applied) != 1 {
		t.Fatalf("result.Applied: got %d, want 1", len(result.Applied))
	}

	// Fragment file must be written.
	raw, err := mem.ReadFile("/repo/.claude/ainfra/no-todos.md")
	if err != nil {
		t.Fatalf("ReadFile fragment: %v", err)
	}
	if string(raw) != "do not write TODO comments" {
		t.Errorf("fragment content = %q, want %q", string(raw), "do not write TODO comments")
	}

	// Target file must contain the import line.
	targetRaw, err := mem.ReadFile("/repo/CLAUDE.md")
	if err != nil {
		t.Fatalf("ReadFile target: %v", err)
	}
	if !strings.Contains(string(targetRaw), "@.claude/ainfra/no-todos.md") {
		t.Errorf("target file missing import line, got: %q", string(targetRaw))
	}
}

func TestRulesApply_Create_HomeTarget(t *testing.T) {
	mem := provider.NewMemFilesystem()
	env := provider.Env{FS: mem, Root: "/repo", Home: "/home/dev"}

	plan := provider.ChannelPlan{
		Channel: "rules",
		Changes: []provider.Change{
			{
				Kind: provider.ChangeCreate,
				ID:   "team-claude-md",
				Resource: provider.Resource{
					ID:      "team-claude-md",
					Channel: "rules",
					Payload: map[string]any{
						"target":  "~/.claude/CLAUDE.md",
						"content": "team rules",
					},
				},
			},
		},
	}

	p := claudecode.Rules{}
	if _, err := p.Apply(env, plan); err != nil {
		t.Fatalf("Apply: unexpected error: %v", err)
	}

	// A "~" target expands against Home: the fragment is co-located there.
	raw, err := mem.ReadFile("/home/dev/.claude/ainfra/team-claude-md.md")
	if err != nil {
		t.Fatalf("ReadFile home fragment: %v", err)
	}
	if string(raw) != "team rules" {
		t.Errorf("fragment content = %q, want %q", string(raw), "team rules")
	}

	// The target is the home file, and its import line is relative to it.
	targetRaw, err := mem.ReadFile("/home/dev/.claude/CLAUDE.md")
	if err != nil {
		t.Fatalf("ReadFile home target: %v", err)
	}
	if !strings.Contains(string(targetRaw), "@ainfra/team-claude-md.md") {
		t.Errorf("home target missing relative import line, got: %q", string(targetRaw))
	}
}

func TestRulesApply_Create_Idempotent(t *testing.T) {
	mem := provider.NewMemFilesystem()
	env := provider.Env{FS: mem, Root: "/repo"}

	plan := provider.ChannelPlan{
		Channel: "rules",
		Changes: []provider.Change{
			{
				Kind: provider.ChangeCreate,
				ID:   "no-todos",
				Resource: provider.Resource{
					ID:      "no-todos",
					Channel: "rules",
					Payload: map[string]any{
						"target":  "CLAUDE.md",
						"content": "do not write TODO comments",
					},
				},
			},
		},
	}

	p := claudecode.Rules{}

	// Apply twice.
	if _, err := p.Apply(env, plan); err != nil {
		t.Fatalf("first Apply: unexpected error: %v", err)
	}
	if _, err := p.Apply(env, plan); err != nil {
		t.Fatalf("second Apply: unexpected error: %v", err)
	}

	targetRaw, err := mem.ReadFile("/repo/CLAUDE.md")
	if err != nil {
		t.Fatalf("ReadFile target: %v", err)
	}
	count := strings.Count(string(targetRaw), "@.claude/ainfra/no-todos.md")
	if count != 1 {
		t.Errorf("import line appears %d times, want exactly 1; content: %q", count, string(targetRaw))
	}
}

func TestRulesApply_Delete(t *testing.T) {
	mem := provider.NewMemFilesystem()
	env := provider.Env{FS: mem, Root: "/repo"}

	if err := mem.MkdirAll("/repo/.claude/ainfra", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := mem.WriteFile("/repo/.claude/ainfra/no-todos.md", []byte("do not write TODO comments"), 0o644); err != nil {
		t.Fatal(err)
	}

	plan := provider.ChannelPlan{
		Channel: "rules",
		Changes: []provider.Change{
			{
				Kind: provider.ChangeDelete,
				ID:   "no-todos",
				Resource: provider.Resource{
					ID:      "no-todos",
					Channel: "rules",
					Payload: map[string]any{
						"target": "CLAUDE.md",
					},
				},
			},
		},
	}

	p := claudecode.Rules{}
	result, err := p.Apply(env, plan)
	if err != nil {
		t.Fatalf("Apply: unexpected error: %v", err)
	}
	if len(result.Applied) != 1 {
		t.Fatalf("result.Applied: got %d, want 1", len(result.Applied))
	}

	_, err = mem.ReadFile("/repo/.claude/ainfra/no-todos.md")
	if !os.IsNotExist(err) {
		t.Errorf("fragment file should have been removed, ReadFile err = %v", err)
	}
}

func TestRulesApplyDefaultsEmptyTarget(t *testing.T) {
	mem := provider.NewMemFilesystem()
	env := provider.Env{FS: mem, Root: "/repo"}

	plan := provider.ChannelPlan{
		Channel: "rules",
		Changes: []provider.Change{
			{
				Kind: provider.ChangeCreate,
				ID:   "no-todos",
				Resource: provider.Resource{
					ID:      "no-todos",
					Channel: "rules",
					Payload: map[string]any{
						"content": "do not write TODO comments",
						// no "target" key — should default to CLAUDE.md
					},
				},
			},
		},
	}

	p := claudecode.Rules{}
	result, err := p.Apply(env, plan)
	if err != nil {
		t.Fatalf("Apply: unexpected error for missing target: %v", err)
	}
	if len(result.Applied) != 1 {
		t.Fatalf("result.Applied: got %d, want 1", len(result.Applied))
	}

	// Fragment file must be written.
	raw, err := mem.ReadFile("/repo/.claude/ainfra/no-todos.md")
	if err != nil {
		t.Fatalf("ReadFile fragment: %v", err)
	}
	if string(raw) != "do not write TODO comments" {
		t.Errorf("fragment content = %q, want %q", string(raw), "do not write TODO comments")
	}

	// Default target CLAUDE.md must contain the import line.
	targetRaw, err := mem.ReadFile("/repo/CLAUDE.md")
	if err != nil {
		t.Fatalf("ReadFile default target: %v", err)
	}
	if !strings.Contains(string(targetRaw), "@.claude/ainfra/no-todos.md") {
		t.Errorf("default target CLAUDE.md missing import line, got: %q", string(targetRaw))
	}
}

func TestRulesApply_DryRun(t *testing.T) {
	mem := provider.NewMemFilesystem()
	env := provider.Env{FS: mem, Root: "/repo", DryRun: true}

	plan := provider.ChannelPlan{
		Channel: "rules",
		Changes: []provider.Change{
			{
				Kind: provider.ChangeCreate,
				ID:   "no-todos",
				Resource: provider.Resource{
					ID:      "no-todos",
					Channel: "rules",
					Payload: map[string]any{
						"target":  "CLAUDE.md",
						"content": "do not write TODO comments",
					},
				},
			},
		},
	}

	p := claudecode.Rules{}
	result, err := p.Apply(env, plan)
	if err != nil {
		t.Fatalf("Apply: unexpected error: %v", err)
	}
	if len(result.Applied) != 1 {
		t.Fatalf("DryRun: expected 1 applied change described, got %d", len(result.Applied))
	}

	// Fragment must not have been written.
	_, err = mem.ReadFile("/repo/.claude/ainfra/no-todos.md")
	if !os.IsNotExist(err) {
		t.Errorf("DryRun: fragment file was created, should not have been")
	}

	// Target must not have been written.
	_, err = mem.ReadFile("/repo/CLAUDE.md")
	if !os.IsNotExist(err) {
		t.Errorf("DryRun: target file was created, should not have been")
	}
}
