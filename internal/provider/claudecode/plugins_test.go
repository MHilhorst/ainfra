package claudecode_test

import (
	"fmt"
	"testing"

	"github.com/MHilhorst/ainfra/internal/provider"
	"github.com/MHilhorst/ainfra/internal/provider/claudecode"
)

func TestPluginsChannel(t *testing.T) {
	p := claudecode.Plugins{}
	if got := p.Channel(); got != "plugins" {
		t.Fatalf("Channel() = %q, want plugins", got)
	}
}

func TestPluginsObserve_MissingFile(t *testing.T) {
	mem := provider.NewMemFilesystem()
	env := provider.Env{FS: mem, Home: "/home/user"}

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
	env := provider.Env{FS: mem, Home: "/home/user"}

	installedJSON := `{
		"version": 2,
		"plugins": {
			"tvt-config@trein-vertraging": [{"scope":"user","installPath":"/tmp/tvt","version":"1.0.0","installedAt":"2026-01-01T00:00:00Z","lastUpdated":"2026-01-01T00:00:00Z"}],
			"claude-ads@trein-vertraging": [{"scope":"user","installPath":"/tmp/ads","version":"1.0.0","installedAt":"2026-01-01T00:00:00Z","lastUpdated":"2026-01-01T00:00:00Z"}]
		}
	}`
	if err := mem.WriteFile("/home/user/.claude/plugins/installed_plugins.json", []byte(installedJSON), 0o644); err != nil {
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
			t.Errorf("resource %q: Channel = %q, want plugins", r.ID, r.Channel)
		}
		if r.ContentHash != "" {
			t.Errorf("resource %q: ContentHash should be empty (backfilled by orchestrator), got %q", r.ID, r.ContentHash)
		}
	}
	if !ids["tvt-config"] {
		t.Error("expected resource with id 'tvt-config' (bare name without @marketplace)")
	}
	if !ids["claude-ads"] {
		t.Error("expected resource with id 'claude-ads' (bare name without @marketplace)")
	}
}

func TestPluginsApply_Create(t *testing.T) {
	runner := provider.NewFakeRunner()
	runner.Script["claude plugin install tvt-config@trein-vertraging"] = provider.FakeResult{}
	env := provider.Env{FS: provider.NewMemFilesystem(), Home: "/home/user", Runner: runner}

	plan := provider.ChannelPlan{
		Channel: "plugins",
		Changes: []provider.Change{
			{
				Kind: provider.ChangeCreate,
				ID:   "tvt-config",
				Resource: provider.Resource{
					ID:      "tvt-config",
					Channel: "plugins",
					Payload: map[string]any{
						"marketplace": "trein-vertraging",
						"version":     "",
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
	if len(result.Applied) != 1 {
		t.Fatalf("result.Applied: got %d, want 1", len(result.Applied))
	}
	if result.Channel != "plugins" {
		t.Errorf("result.Channel = %q, want plugins", result.Channel)
	}
	if len(runner.Calls) != 1 || runner.Calls[0] != "claude plugin install tvt-config@trein-vertraging" {
		t.Errorf("runner.Calls = %v, want [claude plugin install tvt-config@trein-vertraging]", runner.Calls)
	}
}

func TestPluginsApply_CreateAlreadyInstalled(t *testing.T) {
	runner := provider.NewFakeRunner()
	runner.Script["claude plugin install tvt-config@trein-vertraging"] = provider.FakeResult{
		Err: fmt.Errorf("plugin already installed: tvt-config"),
	}
	env := provider.Env{FS: provider.NewMemFilesystem(), Home: "/home/user", Runner: runner}

	plan := provider.ChannelPlan{
		Channel: "plugins",
		Changes: []provider.Change{
			{
				Kind: provider.ChangeCreate,
				ID:   "tvt-config",
				Resource: provider.Resource{
					ID:      "tvt-config",
					Channel: "plugins",
					Payload: map[string]any{"marketplace": "trein-vertraging"},
				},
			},
		},
	}

	p := claudecode.Plugins{}
	result, err := p.Apply(env, plan)
	if err != nil {
		t.Fatalf("Apply: expected no error for already-installed plugin, got: %v", err)
	}
	if len(result.Applied) != 1 {
		t.Fatalf("result.Applied: got %d, want 1", len(result.Applied))
	}
}

func TestPluginsApply_UpdateWithVersion(t *testing.T) {
	runner := provider.NewFakeRunner()
	runner.Script["claude plugin update tvt-config@trein-vertraging"] = provider.FakeResult{}
	env := provider.Env{FS: provider.NewMemFilesystem(), Home: "/home/user", Runner: runner}

	plan := provider.ChannelPlan{
		Channel: "plugins",
		Changes: []provider.Change{
			{
				Kind: provider.ChangeUpdate,
				ID:   "tvt-config",
				Resource: provider.Resource{
					ID:      "tvt-config",
					Channel: "plugins",
					Payload: map[string]any{
						"marketplace": "trein-vertraging",
						"version":     "2.0.0",
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
	if len(result.Applied) != 1 {
		t.Fatalf("result.Applied: got %d, want 1", len(result.Applied))
	}
	if len(runner.Calls) != 1 || runner.Calls[0] != "claude plugin update tvt-config@trein-vertraging" {
		t.Errorf("runner.Calls = %v, want [claude plugin update tvt-config@trein-vertraging]", runner.Calls)
	}
}

func TestPluginsApply_UpdateFailureDoesNotAbort(t *testing.T) {
	runner := provider.NewFakeRunner()
	runner.Script["claude plugin update tvt-config@trein-vertraging"] = provider.FakeResult{
		Err: fmt.Errorf("update failed: network error"),
	}
	env := provider.Env{FS: provider.NewMemFilesystem(), Home: "/home/user", Runner: runner}

	plan := provider.ChannelPlan{
		Channel: "plugins",
		Changes: []provider.Change{
			{
				Kind: provider.ChangeUpdate,
				ID:   "tvt-config",
				Resource: provider.Resource{
					ID:      "tvt-config",
					Channel: "plugins",
					Payload: map[string]any{
						"marketplace": "trein-vertraging",
						"version":     "2.0.0",
					},
				},
			},
		},
	}

	p := claudecode.Plugins{}
	result, err := p.Apply(env, plan)
	if err != nil {
		t.Fatalf("Apply: update failure should not abort, got error: %v", err)
	}
	if len(result.Applied) != 1 {
		t.Fatalf("result.Applied: got %d, want 1", len(result.Applied))
	}
}

func TestPluginsApply_Delete(t *testing.T) {
	runner := provider.NewFakeRunner()
	runner.Script["claude plugin uninstall tvt-config"] = provider.FakeResult{}
	env := provider.Env{FS: provider.NewMemFilesystem(), Home: "/home/user", Runner: runner}

	plan := provider.ChannelPlan{
		Channel: "plugins",
		Changes: []provider.Change{
			{
				Kind: provider.ChangeDelete,
				ID:   "tvt-config",
				Resource: provider.Resource{
					ID:      "tvt-config",
					Channel: "plugins",
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
		t.Fatalf("result.Applied: got %d, want 1", len(result.Applied))
	}
	if len(runner.Calls) != 1 || runner.Calls[0] != "claude plugin uninstall tvt-config" {
		t.Errorf("runner.Calls = %v, want [claude plugin uninstall tvt-config]", runner.Calls)
	}
}

func TestPluginsApply_DryRun(t *testing.T) {
	runner := provider.NewFakeRunner()
	env := provider.Env{FS: provider.NewMemFilesystem(), Home: "/home/user", Runner: runner, DryRun: true}

	plan := provider.ChannelPlan{
		Channel: "plugins",
		Changes: []provider.Change{
			{
				Kind: provider.ChangeCreate,
				ID:   "tvt-config",
				Resource: provider.Resource{
					ID:      "tvt-config",
					Channel: "plugins",
					Payload: map[string]any{"marketplace": "trein-vertraging"},
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
		t.Fatalf("DryRun: expected 1 described change, got %d", len(result.Applied))
	}
	if len(runner.Calls) != 0 {
		t.Errorf("DryRun: runner was called %d times, want 0", len(runner.Calls))
	}
}

func TestPluginsApply_NoLegacyPluginsJSON(t *testing.T) {
	// Verify the new provider does NOT write plugins.json.
	runner := provider.NewFakeRunner()
	runner.Script["claude plugin install tvt-config@trein-vertraging"] = provider.FakeResult{}
	mem := provider.NewMemFilesystem()
	env := provider.Env{FS: mem, Home: "/home/user", Runner: runner, Root: "/repo"}

	plan := provider.ChannelPlan{
		Channel: "plugins",
		Changes: []provider.Change{
			{
				Kind: provider.ChangeCreate,
				ID:   "tvt-config",
				Resource: provider.Resource{
					ID:      "tvt-config",
					Channel: "plugins",
					Payload: map[string]any{"marketplace": "trein-vertraging"},
				},
			},
		},
	}

	p := claudecode.Plugins{}
	if _, err := p.Apply(env, plan); err != nil {
		t.Fatalf("Apply: unexpected error: %v", err)
	}

	// The legacy plugins.json must NOT exist.
	legacyPath := "/repo/.claude/ainfra/plugins.json"
	if _, err := mem.ReadFile(legacyPath); err == nil {
		t.Errorf("legacy plugins.json was written at %s, expected it to not exist", legacyPath)
	}
}
