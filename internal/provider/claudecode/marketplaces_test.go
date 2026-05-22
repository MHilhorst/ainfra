package claudecode_test

import (
	"fmt"
	"testing"

	"github.com/MHilhorst/ainfra/internal/provider"
	"github.com/MHilhorst/ainfra/internal/provider/claudecode"
)

func TestMarketplacesChannel(t *testing.T) {
	m := claudecode.Marketplaces{}
	if got := m.Channel(); got != "marketplaces" {
		t.Fatalf("Channel() = %q, want marketplaces", got)
	}
}

func TestMarketplacesObserve_MissingFile(t *testing.T) {
	mem := provider.NewMemFilesystem()
	env := provider.Env{FS: mem, Home: "/home/user"}

	m := claudecode.Marketplaces{}
	resources, err := m.Observe(env)
	if err != nil {
		t.Fatalf("Observe: unexpected error: %v", err)
	}
	if len(resources) != 0 {
		t.Fatalf("Observe: got %d resources, want 0", len(resources))
	}
}

func TestMarketplacesObserve_WithMarketplaces(t *testing.T) {
	mem := provider.NewMemFilesystem()
	env := provider.Env{FS: mem, Home: "/home/user"}

	knownJSON := `{
		"my-org": {
			"source": {"source": "github", "repo": "my-org/plugins"},
			"installLocation": "/home/user/.claude/plugins/marketplaces/my-org",
			"lastUpdated": "2026-01-01T00:00:00.000Z"
		},
		"local-mp": {
			"source": {"source": "directory", "path": "/tmp/local"},
			"installLocation": "/tmp/local",
			"lastUpdated": "2026-01-01T00:00:00.000Z"
		}
	}`
	if err := mem.WriteFile("/home/user/.claude/plugins/known_marketplaces.json", []byte(knownJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	m := claudecode.Marketplaces{}
	resources, err := m.Observe(env)
	if err != nil {
		t.Fatalf("Observe: unexpected error: %v", err)
	}
	if len(resources) != 2 {
		t.Fatalf("Observe: got %d resources, want 2", len(resources))
	}

	ids := map[string]bool{}
	for _, r := range resources {
		ids[r.ID] = true
		if r.Channel != "marketplaces" {
			t.Errorf("resource %q: Channel = %q, want marketplaces", r.ID, r.Channel)
		}
	}
	if !ids["my-org"] {
		t.Error("expected resource with id 'my-org'")
	}
	if !ids["local-mp"] {
		t.Error("expected resource with id 'local-mp'")
	}
}

func TestMarketplacesApply_Create(t *testing.T) {
	runner := provider.NewFakeRunner()
	runner.Script["claude plugin marketplace add github:my-org/plugins"] = provider.FakeResult{}
	env := provider.Env{FS: provider.NewMemFilesystem(), Home: "/home/user", Runner: runner}

	plan := provider.ChannelPlan{
		Channel: "marketplaces",
		Changes: []provider.Change{
			{
				Kind: provider.ChangeCreate,
				ID:   "my-org",
				Resource: provider.Resource{
					ID:      "my-org",
					Channel: "marketplaces",
					Payload: map[string]any{"source": "github:my-org/plugins"},
				},
			},
		},
	}

	m := claudecode.Marketplaces{}
	result, err := m.Apply(env, plan)
	if err != nil {
		t.Fatalf("Apply: unexpected error: %v", err)
	}
	if len(result.Applied) != 1 {
		t.Fatalf("result.Applied: got %d, want 1", len(result.Applied))
	}
	if result.Channel != "marketplaces" {
		t.Errorf("result.Channel = %q, want marketplaces", result.Channel)
	}

	if len(runner.Calls) != 1 || runner.Calls[0] != "claude plugin marketplace add github:my-org/plugins" {
		t.Errorf("runner.Calls = %v, want [claude plugin marketplace add github:my-org/plugins]", runner.Calls)
	}
}

func TestMarketplacesApply_CreateAlreadyRegistered(t *testing.T) {
	runner := provider.NewFakeRunner()
	runner.Script["claude plugin marketplace add github:my-org/plugins"] = provider.FakeResult{
		Err: fmt.Errorf("marketplace already exists: my-org"),
	}
	env := provider.Env{FS: provider.NewMemFilesystem(), Home: "/home/user", Runner: runner}

	plan := provider.ChannelPlan{
		Channel: "marketplaces",
		Changes: []provider.Change{
			{
				Kind: provider.ChangeCreate,
				ID:   "my-org",
				Resource: provider.Resource{
					ID:      "my-org",
					Channel: "marketplaces",
					Payload: map[string]any{"source": "github:my-org/plugins"},
				},
			},
		},
	}

	m := claudecode.Marketplaces{}
	result, err := m.Apply(env, plan)
	if err != nil {
		t.Fatalf("Apply: expected no error for already-registered marketplace, got: %v", err)
	}
	if len(result.Applied) != 1 {
		t.Fatalf("result.Applied: got %d, want 1", len(result.Applied))
	}
}

func TestMarketplacesApply_Delete(t *testing.T) {
	runner := provider.NewFakeRunner()
	runner.Script["claude plugin marketplace remove my-org"] = provider.FakeResult{}
	env := provider.Env{FS: provider.NewMemFilesystem(), Home: "/home/user", Runner: runner}

	plan := provider.ChannelPlan{
		Channel: "marketplaces",
		Changes: []provider.Change{
			{
				Kind: provider.ChangeDelete,
				ID:   "my-org",
				Resource: provider.Resource{
					ID:      "my-org",
					Channel: "marketplaces",
				},
			},
		},
	}

	m := claudecode.Marketplaces{}
	result, err := m.Apply(env, plan)
	if err != nil {
		t.Fatalf("Apply: unexpected error: %v", err)
	}
	if len(result.Applied) != 1 {
		t.Fatalf("result.Applied: got %d, want 1", len(result.Applied))
	}
	if len(runner.Calls) != 1 || runner.Calls[0] != "claude plugin marketplace remove my-org" {
		t.Errorf("runner.Calls = %v, want [claude plugin marketplace remove my-org]", runner.Calls)
	}
}

func TestMarketplacesApply_DryRun(t *testing.T) {
	runner := provider.NewFakeRunner()
	env := provider.Env{FS: provider.NewMemFilesystem(), Home: "/home/user", Runner: runner, DryRun: true}

	plan := provider.ChannelPlan{
		Channel: "marketplaces",
		Changes: []provider.Change{
			{
				Kind: provider.ChangeCreate,
				ID:   "my-org",
				Resource: provider.Resource{
					ID:      "my-org",
					Channel: "marketplaces",
					Payload: map[string]any{"source": "github:my-org/plugins"},
				},
			},
		},
	}

	m := claudecode.Marketplaces{}
	result, err := m.Apply(env, plan)
	if err != nil {
		t.Fatalf("Apply: unexpected error: %v", err)
	}
	if len(result.Applied) != 1 {
		t.Fatalf("DryRun: expected 1 described change, got %d", len(result.Applied))
	}
	if len(runner.Calls) != 0 {
		t.Errorf("DryRun: runner was called %d times, want 0", len(runner.Calls))
	}
}
