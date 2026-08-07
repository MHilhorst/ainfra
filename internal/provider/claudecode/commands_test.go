package claudecode_test

import (
	"os"
	"testing"

	"github.com/MHilhorst/ainfra/internal/provider"
	"github.com/MHilhorst/ainfra/internal/provider/claudecode"
)

func TestCommandsChannel(t *testing.T) {
	p := claudecode.Commands{}
	if got := p.Channel(); got != "commands" {
		t.Fatalf("Channel() = %q, want %q", got, "commands")
	}
}

func TestCommandsObserve_MissingDir(t *testing.T) {
	mem := provider.NewMemFilesystem()
	env := provider.Env{FS: mem, Root: "/repo"}

	p := claudecode.Commands{}
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

	p := claudecode.Commands{}
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
		if r.ContentHash == "" {
			t.Errorf("resource %q: ContentHash should be populated (drift detection)", r.ID)
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

	p := claudecode.Commands{}
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

	p := claudecode.Commands{}
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

	p := claudecode.Commands{}
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

// userTargetEnv is an Env with a home distinct from the repo, so the two
// candidate command directories are actually different paths.
func userTargetEnv(mem *provider.MemFilesystem) provider.Env {
	return provider.Env{FS: mem, Root: "/repo", Home: "/home/dev"}
}

func userCommandPlan(kind provider.ChangeKind, id, content, target string) provider.ChannelPlan {
	return provider.ChannelPlan{
		Channel: "commands",
		Changes: []provider.Change{{
			Kind: kind,
			ID:   id,
			Resource: provider.Resource{
				ID:      id,
				Channel: "commands",
				Payload: map[string]any{"content": content, "target": target},
			},
		}},
	}
}

func TestCommandsApply_UserTargetWritesToHome(t *testing.T) {
	mem := provider.NewMemFilesystem()
	env := userTargetEnv(mem)

	p := claudecode.Commands{}
	if _, err := p.Apply(env, userCommandPlan(provider.ChangeCreate, "pr", "open a PR", claudecode.UserCommandsTarget)); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	raw, err := mem.ReadFile("/home/dev/.claude/commands/pr.md")
	if err != nil {
		t.Fatalf("user-wide command not written: %v", err)
	}
	if string(raw) != "open a PR" {
		t.Errorf("content = %q, want %q", raw, "open a PR")
	}
	if _, err := mem.ReadFile("/repo/.claude/commands/pr.md"); !os.IsNotExist(err) {
		t.Errorf("a user-targeted command must not land in the repo, err = %v", err)
	}
}

func TestCommandsApply_RetargetRemovesTheOldCopy(t *testing.T) {
	mem := provider.NewMemFilesystem()
	env := userTargetEnv(mem)
	if err := mem.MkdirAll("/repo/.claude/commands", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := mem.WriteFile("/repo/.claude/commands/pr.md", []byte("open a PR"), 0o644); err != nil {
		t.Fatal(err)
	}

	p := claudecode.Commands{}
	if _, err := p.Apply(env, userCommandPlan(provider.ChangeUpdate, "pr", "open a PR", claudecode.UserCommandsTarget)); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if _, err := mem.ReadFile("/home/dev/.claude/commands/pr.md"); err != nil {
		t.Fatalf("command not written to the new target: %v", err)
	}
	// Claude Code would load both copies, and Observe's de-duplication prefers
	// the repo one — so leaving it behind is a drift that never converges.
	if _, err := mem.ReadFile("/repo/.claude/commands/pr.md"); !os.IsNotExist(err) {
		t.Errorf("stale repo copy survived the retarget, err = %v", err)
	}
}

func TestCommandsApply_DeleteRemovesBothLocations(t *testing.T) {
	mem := provider.NewMemFilesystem()
	env := userTargetEnv(mem)
	for _, dir := range []string{"/repo/.claude/commands", "/home/dev/.claude/commands"} {
		if err := mem.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := mem.WriteFile(dir+"/pr.md", []byte("open a PR"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// A delete's resource comes from the applied ledger, which carries no
	// payload — so the provider cannot know which target it was installed to.
	plan := provider.ChannelPlan{
		Channel: "commands",
		Changes: []provider.Change{{
			Kind:     provider.ChangeDelete,
			ID:       "pr",
			Resource: provider.Resource{ID: "pr", Channel: "commands"},
		}},
	}

	p := claudecode.Commands{}
	if _, err := p.Apply(env, plan); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	for _, path := range []string{"/repo/.claude/commands/pr.md", "/home/dev/.claude/commands/pr.md"} {
		if _, err := mem.ReadFile(path); !os.IsNotExist(err) {
			t.Errorf("%s survived the delete, err = %v", path, err)
		}
	}
}

func TestCommandsObserve_FindsUserWideCommands(t *testing.T) {
	mem := provider.NewMemFilesystem()
	env := userTargetEnv(mem)
	if err := mem.MkdirAll("/home/dev/.claude/commands", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := mem.WriteFile("/home/dev/.claude/commands/pr.md", []byte("open a PR"), 0o644); err != nil {
		t.Fatal(err)
	}

	p := claudecode.Commands{}
	resources, err := p.Observe(env)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if len(resources) != 1 || resources[0].ID != "pr" {
		t.Fatalf("Observe = %+v, want one resource 'pr'", resources)
	}
	// The hash has to reproduce what the pipeline computes for a user-targeted
	// command, or the diff reports drift no run can resolve.
	want := claudecode.CommandContentHash("open a PR", claudecode.UserCommandsTarget)
	if resources[0].ContentHash != want {
		t.Errorf("ContentHash = %q, want %q", resources[0].ContentHash, want)
	}
}

func TestCommandsObserve_RepoCopyWinsOverUserWide(t *testing.T) {
	mem := provider.NewMemFilesystem()
	env := userTargetEnv(mem)
	for _, dir := range []string{"/repo/.claude/commands", "/home/dev/.claude/commands"} {
		if err := mem.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := mem.WriteFile("/repo/.claude/commands/pr.md", []byte("repo version"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := mem.WriteFile("/home/dev/.claude/commands/pr.md", []byte("user version"), 0o644); err != nil {
		t.Fatal(err)
	}

	p := claudecode.Commands{}
	resources, err := p.Observe(env)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if len(resources) != 1 {
		t.Fatalf("Observe returned %d resources, want 1 (de-duplicated by id)", len(resources))
	}
	want := claudecode.CommandContentHash("repo version", "")
	if resources[0].ContentHash != want {
		t.Errorf("the narrower repo declaration should win, ContentHash = %q, want %q", resources[0].ContentHash, want)
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

	p := claudecode.Commands{}
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
