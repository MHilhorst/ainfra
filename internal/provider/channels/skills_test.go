package channels_test

import (
	"errors"
	"testing"

	"github.com/MHilhorst/ainfra/internal/provider"
	"github.com/MHilhorst/ainfra/internal/provider/channels"
	"github.com/MHilhorst/ainfra/internal/provider/fetch"
)

func TestSkillsChannel(t *testing.T) {
	p := channels.Skills{}
	if got := p.Channel(); got != "skills" {
		t.Fatalf("Channel() = %q, want %q", got, "skills")
	}
}

func TestSkillsObserve_Empty(t *testing.T) {
	mem := provider.NewMemFilesystem()
	env := provider.Env{FS: mem, Root: "/repo"}

	p := channels.Skills{}
	resources, err := p.Observe(env)
	if err != nil {
		t.Fatalf("Observe: unexpected error: %v", err)
	}
	if len(resources) != 0 {
		t.Fatalf("Observe: got %d resources, want 0", len(resources))
	}
}

func TestSkillsObserve_WithSkills(t *testing.T) {
	mem := provider.NewMemFilesystem()
	env := provider.Env{FS: mem, Root: "/repo"}

	// create two skill directories, each with at least one file
	if err := mem.MkdirAll("/repo/.claude/skills/skill-a", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := mem.WriteFile("/repo/.claude/skills/skill-a/prompt.md", []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := mem.MkdirAll("/repo/.claude/skills/skill-b", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := mem.WriteFile("/repo/.claude/skills/skill-b/prompt.md", []byte("world"), 0o644); err != nil {
		t.Fatal(err)
	}

	p := channels.Skills{}
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
		if r.Channel != "skills" {
			t.Errorf("resource %q: Channel = %q, want %q", r.ID, r.Channel, "skills")
		}
	}
	if !ids["skill-a"] {
		t.Error("expected resource with id 'skill-a'")
	}
	if !ids["skill-b"] {
		t.Error("expected resource with id 'skill-b'")
	}
}

func TestSkillsObserve_EmptySubdirNotCounted(t *testing.T) {
	mem := provider.NewMemFilesystem()
	env := provider.Env{FS: mem, Root: "/repo"}

	// create a skill directory but without any files
	if err := mem.MkdirAll("/repo/.claude/skills/empty-skill", 0o755); err != nil {
		t.Fatal(err)
	}

	p := channels.Skills{}
	resources, err := p.Observe(env)
	if err != nil {
		t.Fatalf("Observe: unexpected error: %v", err)
	}
	if len(resources) != 0 {
		t.Fatalf("Observe: got %d resources, want 0 (empty skill dir should not count)", len(resources))
	}
}

func TestSkillsApply_Create(t *testing.T) {
	mem := provider.NewMemFilesystem()
	fake := fetch.FakeFetcher{
		Bundles: map[string]fetch.Bundle{
			"my-source": {
				"prompt.md":    []byte("# My Skill"),
				"config.json":  []byte(`{"version":"1"}`),
			},
		},
	}
	env := provider.Env{FS: mem, Root: "/repo", Fetch: fake}

	plan := provider.ChannelPlan{
		Channel: "skills",
		Changes: []provider.Change{
			{
				Kind: provider.ChangeCreate,
				ID:   "my-skill",
				Resource: provider.Resource{
					ID:      "my-skill",
					Channel: "skills",
					Payload: map[string]any{
						"source":  "my-source",
						"version": "v1.0",
					},
				},
			},
		},
	}

	p := channels.Skills{}
	result, err := p.Apply(env, plan)
	if err != nil {
		t.Fatalf("Apply: unexpected error: %v", err)
	}
	if result.Channel != "skills" {
		t.Errorf("result.Channel = %q, want %q", result.Channel, "skills")
	}
	if len(result.Applied) != 1 {
		t.Fatalf("result.Applied: got %d, want 1", len(result.Applied))
	}

	// verify files were written
	content, err := mem.ReadFile("/repo/.claude/skills/my-skill/prompt.md")
	if err != nil {
		t.Fatalf("ReadFile prompt.md: %v", err)
	}
	if string(content) != "# My Skill" {
		t.Errorf("prompt.md content = %q, want %q", content, "# My Skill")
	}

	content, err = mem.ReadFile("/repo/.claude/skills/my-skill/config.json")
	if err != nil {
		t.Fatalf("ReadFile config.json: %v", err)
	}
	if string(content) != `{"version":"1"}` {
		t.Errorf("config.json content = %q, want %q", content, `{"version":"1"}`)
	}
}

func TestSkillsApply_Delete(t *testing.T) {
	mem := provider.NewMemFilesystem()
	env := provider.Env{FS: mem, Root: "/repo"}

	// pre-populate a skill directory
	if err := mem.MkdirAll("/repo/.claude/skills/old-skill", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := mem.WriteFile("/repo/.claude/skills/old-skill/prompt.md", []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	plan := provider.ChannelPlan{
		Channel: "skills",
		Changes: []provider.Change{
			{
				Kind: provider.ChangeDelete,
				ID:   "old-skill",
				Resource: provider.Resource{
					ID:      "old-skill",
					Channel: "skills",
				},
			},
		},
	}

	p := channels.Skills{}
	result, err := p.Apply(env, plan)
	if err != nil {
		t.Fatalf("Apply: unexpected error: %v", err)
	}
	if len(result.Applied) != 1 {
		t.Fatalf("result.Applied: got %d, want 1", len(result.Applied))
	}

	// verify file was removed
	_, err = mem.ReadFile("/repo/.claude/skills/old-skill/prompt.md")
	if err == nil {
		t.Error("expected file to be removed but it still exists")
	}
}

func TestSkillsApply_DryRun(t *testing.T) {
	mem := provider.NewMemFilesystem()
	fake := fetch.FakeFetcher{
		Bundles: map[string]fetch.Bundle{
			"my-source": {
				"prompt.md": []byte("# My Skill"),
			},
		},
	}
	env := provider.Env{FS: mem, Root: "/repo", Fetch: fake, DryRun: true}

	plan := provider.ChannelPlan{
		Channel: "skills",
		Changes: []provider.Change{
			{
				Kind: provider.ChangeCreate,
				ID:   "my-skill",
				Resource: provider.Resource{
					ID:      "my-skill",
					Channel: "skills",
					Payload: map[string]any{
						"source":  "my-source",
						"version": "v1.0",
					},
				},
			},
		},
	}

	p := channels.Skills{}
	result, err := p.Apply(env, plan)
	if err != nil {
		t.Fatalf("Apply: unexpected error: %v", err)
	}
	if len(result.Applied) != 1 {
		t.Fatalf("DryRun: expected 1 applied change described, got %d", len(result.Applied))
	}

	// file must not have been written
	_, err = mem.ReadFile("/repo/.claude/skills/my-skill/prompt.md")
	if err == nil {
		t.Error("DryRun: file was written, should not have been")
	}
}

func TestSkillsApply_DeleteNestedBundle(t *testing.T) {
	mem := provider.NewMemFilesystem()
	env := provider.Env{FS: mem, Root: "/repo"}

	// Pre-populate a skill with a nested file to simulate a bundle that had subdirectories.
	if err := mem.MkdirAll("/repo/.claude/skills/nested-skill", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := mem.WriteFile("/repo/.claude/skills/nested-skill/prompt.md", []byte("top"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := mem.MkdirAll("/repo/.claude/skills/nested-skill/examples", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := mem.WriteFile("/repo/.claude/skills/nested-skill/examples/demo.md", []byte("demo"), 0o644); err != nil {
		t.Fatal(err)
	}

	plan := provider.ChannelPlan{
		Channel: "skills",
		Changes: []provider.Change{
			{
				Kind: provider.ChangeDelete,
				ID:   "nested-skill",
				Resource: provider.Resource{
					ID:      "nested-skill",
					Channel: "skills",
				},
			},
		},
	}

	p := channels.Skills{}
	result, err := p.Apply(env, plan)
	if err != nil {
		t.Fatalf("Apply: unexpected error: %v", err)
	}
	if len(result.Applied) != 1 {
		t.Fatalf("result.Applied: got %d, want 1", len(result.Applied))
	}

	// Both files (including nested) must be gone.
	if _, err := mem.ReadFile("/repo/.claude/skills/nested-skill/prompt.md"); err == nil {
		t.Error("prompt.md still exists after Delete")
	}
	if _, err := mem.ReadFile("/repo/.claude/skills/nested-skill/examples/demo.md"); err == nil {
		t.Error("examples/demo.md still exists after Delete")
	}
}

func TestSkillsApply_BundleKeyEscape(t *testing.T) {
	mem := provider.NewMemFilesystem()
	fake := fetch.FakeFetcher{
		Bundles: map[string]fetch.Bundle{
			"evil-source": {
				"../escape.md": []byte("escaped"),
			},
		},
	}
	env := provider.Env{FS: mem, Root: "/repo", Fetch: fake}

	plan := provider.ChannelPlan{
		Channel: "skills",
		Changes: []provider.Change{
			{
				Kind: provider.ChangeCreate,
				ID:   "evil-skill",
				Resource: provider.Resource{
					ID:      "evil-skill",
					Channel: "skills",
					Payload: map[string]any{
						"source":  "evil-source",
						"version": "",
					},
				},
			},
		},
	}

	p := channels.Skills{}
	_, err := p.Apply(env, plan)
	if err == nil {
		t.Fatal("Apply: expected error for bundle key escaping skill directory, got nil")
	}
}

func TestSkillsApply_FetchError(t *testing.T) {
	mem := provider.NewMemFilesystem()
	fetchErr := errors.New("network failure")
	fake := fetch.FakeFetcher{Err: fetchErr}
	env := provider.Env{FS: mem, Root: "/repo", Fetch: fake}

	plan := provider.ChannelPlan{
		Channel: "skills",
		Changes: []provider.Change{
			{
				Kind: provider.ChangeCreate,
				ID:   "my-skill",
				Resource: provider.Resource{
					ID:      "my-skill",
					Channel: "skills",
					Payload: map[string]any{
						"source":  "my-source",
						"version": "v1.0",
					},
				},
			},
		},
	}

	p := channels.Skills{}
	_, err := p.Apply(env, plan)
	if err == nil {
		t.Fatal("Apply: expected error from fetch failure, got nil")
	}
	if !errors.Is(err, fetchErr) {
		t.Errorf("Apply: error = %v, want wrapping %v", err, fetchErr)
	}
}
