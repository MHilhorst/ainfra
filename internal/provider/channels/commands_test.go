package channels_test

import (
	"os"
	"testing"

	"github.com/MHilhorst/ainfra/internal/provider"
	"github.com/MHilhorst/ainfra/internal/provider/channels"
)

func TestCommandsChannel(t *testing.T) {
	p := channels.Commands{}
	if got := p.Channel(); got != "commands" {
		t.Fatalf("Channel() = %q, want %q", got, "commands")
	}
}

func TestCommandsObserve_MissingDir(t *testing.T) {
	mem := provider.NewMemFilesystem()
	env := provider.Env{FS: mem, Root: "/repo"}

	p := channels.Commands{}
	resources, err := p.Observe(env)
	if err != nil {
		t.Fatalf("Observe: unexpected error: %v", err)
	}
	if len(resources) != 0 {
		t.Fatalf("Observe: got %d resources, want 0", len(resources))
	}
}

func TestCommandsObserve_WithFiles(t *testing.T) {
	mem := provider.NewMemFilesystem()
	env := provider.Env{FS: mem, Root: "/repo"}

	if err := mem.MkdirAll("/repo/.claude/commands", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := mem.WriteFile("/repo/.claude/commands/greet.md", []byte("say hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := mem.WriteFile("/repo/.claude/commands/deploy.md", []byte("run deploy"), 0o644); err != nil {
		t.Fatal(err)
	}

	p := channels.Commands{}
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
		if r.Channel != "commands" {
			t.Errorf("resource %q: Channel = %q, want %q", r.ID, r.Channel, "commands")
		}
		if r.ContentHash != "" {
			t.Errorf("resource %q: ContentHash should be empty, got %q", r.ID, r.ContentHash)
		}
	}
	if !ids["greet"] {
		t.Error("expected resource with id 'greet'")
	}
	if !ids["deploy"] {
		t.Error("expected resource with id 'deploy'")
	}
}

func TestCommandsApply_Create(t *testing.T) {
	mem := provider.NewMemFilesystem()
	env := provider.Env{FS: mem, Root: "/repo"}

	plan := provider.ChannelPlan{
		Channel: "commands",
		Changes: []provider.Change{
			{
				Kind: provider.ChangeCreate,
				ID:   "greet",
				Resource: provider.Resource{
					ID:      "greet",
					Channel: "commands",
					Payload: map[string]any{
						"content": "say hello to the user",
					},
				},
			},
		},
	}

	p := channels.Commands{}
	result, err := p.Apply(env, plan)
	if err != nil {
		t.Fatalf("Apply: unexpected error: %v", err)
	}
	if result.Channel != "commands" {
		t.Errorf("result.Channel = %q, want %q", result.Channel, "commands")
	}
	if len(result.Applied) != 1 {
		t.Fatalf("result.Applied: got %d, want 1", len(result.Applied))
	}

	raw, err := mem.ReadFile("/repo/.claude/commands/greet.md")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(raw) != "say hello to the user" {
		t.Errorf("file content = %q, want %q", string(raw), "say hello to the user")
	}
}

func TestCommandsApply_Update_Overwrites(t *testing.T) {
	mem := provider.NewMemFilesystem()
	env := provider.Env{FS: mem, Root: "/repo"}

	if err := mem.MkdirAll("/repo/.claude/commands", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := mem.WriteFile("/repo/.claude/commands/greet.md", []byte("old content"), 0o644); err != nil {
		t.Fatal(err)
	}

	plan := provider.ChannelPlan{
		Channel: "commands",
		Changes: []provider.Change{
			{
				Kind: provider.ChangeUpdate,
				ID:   "greet",
				Resource: provider.Resource{
					ID:      "greet",
					Channel: "commands",
					Payload: map[string]any{
						"content": "new content",
					},
				},
			},
		},
	}

	p := channels.Commands{}
	_, err := p.Apply(env, plan)
	if err != nil {
		t.Fatalf("Apply: unexpected error: %v", err)
	}

	raw, err := mem.ReadFile("/repo/.claude/commands/greet.md")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(raw) != "new content" {
		t.Errorf("file content = %q, want %q", string(raw), "new content")
	}
}

func TestCommandsApply_Delete(t *testing.T) {
	mem := provider.NewMemFilesystem()
	env := provider.Env{FS: mem, Root: "/repo"}

	if err := mem.MkdirAll("/repo/.claude/commands", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := mem.WriteFile("/repo/.claude/commands/greet.md", []byte("say hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	plan := provider.ChannelPlan{
		Channel: "commands",
		Changes: []provider.Change{
			{
				Kind: provider.ChangeDelete,
				ID:   "greet",
				Resource: provider.Resource{
					ID:      "greet",
					Channel: "commands",
				},
			},
		},
	}

	p := channels.Commands{}
	result, err := p.Apply(env, plan)
	if err != nil {
		t.Fatalf("Apply: unexpected error: %v", err)
	}
	if len(result.Applied) != 1 {
		t.Fatalf("result.Applied: got %d, want 1", len(result.Applied))
	}

	_, err = mem.ReadFile("/repo/.claude/commands/greet.md")
	if !os.IsNotExist(err) {
		t.Errorf("file should have been removed, ReadFile err = %v", err)
	}
}

func TestCommandsApply_DryRun(t *testing.T) {
	mem := provider.NewMemFilesystem()
	env := provider.Env{FS: mem, Root: "/repo", DryRun: true}

	plan := provider.ChannelPlan{
		Channel: "commands",
		Changes: []provider.Change{
			{
				Kind: provider.ChangeCreate,
				ID:   "greet",
				Resource: provider.Resource{
					ID:      "greet",
					Channel: "commands",
					Payload: map[string]any{
						"content": "say hello",
					},
				},
			},
		},
	}

	p := channels.Commands{}
	result, err := p.Apply(env, plan)
	if err != nil {
		t.Fatalf("Apply: unexpected error: %v", err)
	}
	if len(result.Applied) != 1 {
		t.Fatalf("DryRun: expected 1 applied change described, got %d", len(result.Applied))
	}

	// File must not have been written.
	_, err = mem.ReadFile("/repo/.claude/commands/greet.md")
	if !os.IsNotExist(err) {
		t.Errorf("DryRun: file was created, should not have been")
	}
}
