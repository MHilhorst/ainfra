package codex_test

import (
	"strings"
	"testing"

	"github.com/MHilhorst/ainfra/internal/provider"
	"github.com/MHilhorst/ainfra/internal/provider/codex"
)

func TestRulesChannel(t *testing.T) {
	if got := (codex.Rules{}).Channel(); got != "rules" {
		t.Fatalf("Channel() = %q, want rules", got)
	}
}

func TestRulesObserve_Empty(t *testing.T) {
	mem := provider.NewMemFilesystem()
	env := provider.Env{FS: mem, Root: "/repo"}
	got, err := (codex.Rules{}).Observe(env)
	if err != nil {
		t.Fatalf("Observe: unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("Observe: got %d resources, want 0", len(got))
	}
}

func TestRulesApply_CreatePreservesUserContent(t *testing.T) {
	mem := provider.NewMemFilesystem()
	if err := mem.WriteFile("/repo/AGENTS.md", []byte("# Hand-written\n\nMy own notes.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	env := provider.Env{FS: mem, Root: "/repo"}
	plan := provider.ChannelPlan{
		Channel: "rules",
		Changes: []provider.Change{{
			Kind: provider.ChangeCreate,
			ID:   "incident-response",
			Resource: provider.Resource{
				ID:      "incident-response",
				Channel: "rules",
				Payload: map[string]any{"content": "Page the on-call engineer.", "target": "CLAUDE.md"},
			},
		}},
	}
	result, err := (codex.Rules{}).Apply(env, plan)
	if err != nil {
		t.Fatalf("Apply: unexpected error: %v", err)
	}
	if len(result.Applied) != 1 {
		t.Fatalf("Applied = %d, want 1", len(result.Applied))
	}
	out := string(mem.Files["/repo/AGENTS.md"])
	if !strings.Contains(out, "My own notes.") {
		t.Errorf("user content lost:\n%s", out)
	}
	if !strings.Contains(out, "<!-- ainfra:rule incident-response -->") {
		t.Errorf("rule marker missing:\n%s", out)
	}
	if !strings.Contains(out, "Page the on-call engineer.") {
		t.Errorf("rule content missing:\n%s", out)
	}
}

func TestRulesObserve_AfterApply(t *testing.T) {
	mem := provider.NewMemFilesystem()
	env := provider.Env{FS: mem, Root: "/repo"}
	plan := provider.ChannelPlan{
		Channel: "rules",
		Changes: []provider.Change{{
			Kind:     provider.ChangeCreate,
			ID:       "r1",
			Resource: provider.Resource{ID: "r1", Channel: "rules", Payload: map[string]any{"content": "Rule one."}},
		}},
	}
	if _, err := (codex.Rules{}).Apply(env, plan); err != nil {
		t.Fatalf("Apply: unexpected error: %v", err)
	}
	got, err := (codex.Rules{}).Observe(env)
	if err != nil {
		t.Fatalf("Observe: unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].ID != "r1" || got[0].Channel != "rules" {
		t.Fatalf("Observe = %+v, want one rules resource id r1", got)
	}
	if got[0].ContentHash != "" {
		t.Errorf("ContentHash should be empty, got %q", got[0].ContentHash)
	}
}

func TestRulesApply_Delete(t *testing.T) {
	mem := provider.NewMemFilesystem()
	env := provider.Env{FS: mem, Root: "/repo"}
	create := provider.ChannelPlan{Channel: "rules", Changes: []provider.Change{{
		Kind:     provider.ChangeCreate,
		ID:       "r1",
		Resource: provider.Resource{ID: "r1", Channel: "rules", Payload: map[string]any{"content": "Rule one."}},
	}}}
	if _, err := (codex.Rules{}).Apply(env, create); err != nil {
		t.Fatalf("Apply create: unexpected error: %v", err)
	}
	del := provider.ChannelPlan{Channel: "rules", Changes: []provider.Change{{
		Kind:     provider.ChangeDelete,
		ID:       "r1",
		Resource: provider.Resource{ID: "r1", Channel: "rules"},
	}}}
	if _, err := (codex.Rules{}).Apply(env, del); err != nil {
		t.Fatalf("Apply delete: unexpected error: %v", err)
	}
	out := string(mem.Files["/repo/AGENTS.md"])
	if strings.Contains(out, "ainfra:rule r1") || strings.Contains(out, "Rule one.") {
		t.Errorf("rule not removed:\n%s", out)
	}
}

func TestRulesApply_Noop(t *testing.T) {
	mem := provider.NewMemFilesystem()
	env := provider.Env{FS: mem, Root: "/repo"}
	plan := provider.ChannelPlan{Channel: "rules", Changes: []provider.Change{{
		Kind:     provider.ChangeNoop,
		ID:       "r1",
		Resource: provider.Resource{ID: "r1", Channel: "rules"},
	}}}
	result, err := (codex.Rules{}).Apply(env, plan)
	if err != nil {
		t.Fatalf("Apply: unexpected error: %v", err)
	}
	if len(result.Applied) != 0 {
		t.Errorf("Applied = %d, want 0 for a noop-only plan", len(result.Applied))
	}
	if _, ok := mem.Files["/repo/AGENTS.md"]; ok {
		t.Error("a noop-only plan must not write the file")
	}
}

func TestRulesApply_DryRunWritesNothing(t *testing.T) {
	mem := provider.NewMemFilesystem()
	env := provider.Env{FS: mem, Root: "/repo", DryRun: true}
	plan := provider.ChannelPlan{Channel: "rules", Changes: []provider.Change{{
		Kind:     provider.ChangeCreate,
		ID:       "r1",
		Resource: provider.Resource{ID: "r1", Channel: "rules", Payload: map[string]any{"content": "Rule one."}},
	}}}
	if _, err := (codex.Rules{}).Apply(env, plan); err != nil {
		t.Fatalf("Apply: unexpected error: %v", err)
	}
	if _, ok := mem.Files["/repo/AGENTS.md"]; ok {
		t.Error("DryRun must not write the file")
	}
}
