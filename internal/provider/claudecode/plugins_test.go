package claudecode_test

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/MHilhorst/ainfra/internal/lockfile"
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
		// The exact hash value is pinned by
		// TestPluginsObserve_HashIsComparableToDesired.
		if r.ContentHash == "" {
			t.Errorf("resource %q: ContentHash is empty; an installed plugin must\n"+
				"hash to {marketplace, version} so the diff can compare it to desired", r.ID)
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
		// Production shape: message in the output, bare *exec.ExitError.
		Output: []byte("plugin already installed: tvt-config"),
		Err:    fmt.Errorf("exit status 1"),
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

func TestPluginsApply_UpdateRunsWithoutPinnedVersion(t *testing.T) {
	// SHA-versioned plugins (plugin.json has no version field) are the
	// recommended flow per the plugins reference. ChangeUpdate must still
	// invoke `claude plugin update` so users see new commits.
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
						// no "version" — SHA-versioned plugin
					},
				},
			},
		},
	}

	p := claudecode.Plugins{}
	if _, err := p.Apply(env, plan); err != nil {
		t.Fatalf("Apply: unexpected error: %v", err)
	}
	if len(runner.Calls) != 1 || runner.Calls[0] != "claude plugin update tvt-config@trein-vertraging" {
		t.Errorf("runner.Calls = %v, want a single update call", runner.Calls)
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
	// Uninstall is qualified with the marketplace so a name shared between
	// two registered marketplaces is unambiguous.
	runner := provider.NewFakeRunner()
	runner.Script["claude plugin uninstall tvt-config@trein-vertraging"] = provider.FakeResult{}
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
		t.Fatalf("result.Applied: got %d, want 1", len(result.Applied))
	}
	want := "claude plugin uninstall tvt-config@trein-vertraging"
	if len(runner.Calls) != 1 || runner.Calls[0] != want {
		t.Errorf("runner.Calls = %v, want [%s]", runner.Calls, want)
	}
}

// A plugin that is already uninstalled is the state we wanted. Claude Code
// still exits 1, and letting that surface aborted the whole plugins channel --
// which the orchestrator reports as a failure of every plugin in the batch, so
// one stale delete made five healthy plugins look broken.
func TestPluginsApply_DeleteAlreadyUninstalledIsNotAnError(t *testing.T) {
	runner := provider.NewFakeRunner()
	// Production shape: ExecRunner is CombinedOutput(), so the CLI's message
	// arrives in the output bytes and the error is a bare *exec.ExitError.
	// Scripting the text into the error instead is what hid this bug.
	runner.Script["claude plugin uninstall gone@somewhere"] = provider.FakeResult{
		Output: []byte(`✘ Failed to uninstall plugin "gone@somewhere": Plugin "gone@somewhere" not found in installed plugins`),
		Err:    errors.New("exit status 1"),
	}
	runner.Script["claude plugin update keeper@trein-vertraging"] = provider.FakeResult{}
	env := provider.Env{FS: provider.NewMemFilesystem(), Home: "/home/user", Runner: runner}

	plan := provider.ChannelPlan{
		Channel: "plugins",
		Changes: []provider.Change{
			{
				Kind: provider.ChangeDelete,
				ID:   "gone",
				Resource: provider.Resource{
					ID: "gone", Channel: "plugins",
					Payload: map[string]any{"marketplace": "somewhere"},
				},
			},
			{
				Kind: provider.ChangeRefresh,
				ID:   "keeper",
				Resource: provider.Resource{
					ID: "keeper", Channel: "plugins",
					Payload: map[string]any{"marketplace": "trein-vertraging"},
				},
			},
		},
	}

	p := claudecode.Plugins{}
	result, err := p.Apply(env, plan)
	if err != nil {
		t.Fatalf("Apply: already-uninstalled plugin must not fail the channel: %v", err)
	}
	if len(result.Applied) != 2 {
		t.Fatalf("result.Applied = %d, want 2 (the stale delete must not drop the rest)", len(result.Applied))
	}
	// The change following the stale delete still has to run.
	if !slices.Contains(runner.Calls, "claude plugin update keeper@trein-vertraging") {
		t.Errorf("runner.Calls = %v, want the refresh after the stale delete to have run", runner.Calls)
	}
}

// A genuine uninstall failure still fails loudly.
func TestPluginsApply_DeleteRealFailureStillErrors(t *testing.T) {
	runner := provider.NewFakeRunner()
	runner.Script["claude plugin uninstall tvt-config@trein-vertraging"] = provider.FakeResult{
		Err: errors.New("EACCES: permission denied"),
	}
	env := provider.Env{FS: provider.NewMemFilesystem(), Home: "/home/user", Runner: runner}

	plan := provider.ChannelPlan{
		Channel: "plugins",
		Changes: []provider.Change{
			{
				Kind: provider.ChangeDelete,
				ID:   "tvt-config",
				Resource: provider.Resource{
					ID: "tvt-config", Channel: "plugins",
					Payload: map[string]any{"marketplace": "trein-vertraging"},
				},
			},
		},
	}

	p := claudecode.Plugins{}
	if _, err := p.Apply(env, plan); err == nil {
		t.Fatal("Apply: expected a real uninstall failure to surface")
	}
}

func TestPluginsApply_DeleteFallsBackToBareName(t *testing.T) {
	// When no marketplace is recorded (legacy ledger state), uninstall
	// passes just the bare name and lets Claude Code resolve it.
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
	if _, err := p.Apply(env, plan); err != nil {
		t.Fatalf("Apply: unexpected error: %v", err)
	}
	if len(runner.Calls) != 1 || runner.Calls[0] != "claude plugin uninstall tvt-config" {
		t.Errorf("runner.Calls = %v, want bare-name uninstall", runner.Calls)
	}
}

func TestPluginsApply_VersionMismatchWarning(t *testing.T) {
	// After install, the plugin's resolved version is read from
	// installed_plugins.json — the file Claude Code actually maintains. When it
	// differs from the manifest pin, Apply returns a Warning (not a Failed) so
	// the user sees the unenforceable pin without breaking apply.
	runner := provider.NewFakeRunner()
	runner.Script["claude plugin install tvt-config@trein-vertraging"] = provider.FakeResult{}
	mem := provider.NewMemFilesystem()
	if err := mem.WriteFile("/home/user/.claude/plugins/installed_plugins.json",
		[]byte(`{"version":2,"plugins":{"tvt-config@trein-vertraging":[{"scope":"user","version":"1.5.0"}]}}`),
		0o644); err != nil {
		t.Fatal(err)
	}
	env := provider.Env{FS: mem, Home: "/home/user", Runner: runner}

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
	if len(result.Warnings) != 1 {
		t.Fatalf("result.Warnings: got %d, want 1", len(result.Warnings))
	}
	if result.Warnings[0].Change.ID != "tvt-config" {
		t.Errorf("warning change ID = %q, want tvt-config", result.Warnings[0].Change.ID)
	}
	if !contains(result.Warnings[0].Reason, "1.5.0") || !contains(result.Warnings[0].Reason, "2.0.0") {
		t.Errorf("warning reason should mention both versions, got %q", result.Warnings[0].Reason)
	}
}

func TestPluginsApply_NoWarningOnVersionMatch(t *testing.T) {
	runner := provider.NewFakeRunner()
	runner.Script["claude plugin install tvt-config@trein-vertraging"] = provider.FakeResult{}
	mem := provider.NewMemFilesystem()
	if err := mem.WriteFile("/home/user/.claude/plugins/installed_plugins.json",
		[]byte(`{"version":2,"plugins":{"tvt-config@trein-vertraging":[{"scope":"user","version":"2.0.0"}]}}`),
		0o644); err != nil {
		t.Fatal(err)
	}
	env := provider.Env{FS: mem, Home: "/home/user", Runner: runner}

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
	if len(result.Warnings) != 0 {
		t.Errorf("result.Warnings: got %v, want none on matching version", result.Warnings)
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
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

// TestPluginsObserve_HashIsComparableToDesired pins the contract that makes the
// plugins diff work at all: the lockfile's desired hash is derived from
// {marketplace, version} (resolve/pipeline.go), so Observe must derive the
// machine's hash the same way from the installed version. Hashing anything else
// (e.g. the cache directory's file contents) yields a value that can never equal
// desired, and the diff degrades to "always update" or, when empty, to a ledger
// backfill that silently noops forever.
func TestPluginsObserve_HashIsComparableToDesired(t *testing.T) {
	mem := provider.NewMemFilesystem()
	env := provider.Env{FS: mem, Home: "/home/user"}

	installedJSON := `{
		"version": 2,
		"plugins": {
			"tvt-config@trein-vertraging": [{"scope":"user","installPath":"/x","version":"2.16.0"}]
		}
	}`
	if err := mem.WriteFile("/home/user/.claude/plugins/installed_plugins.json", []byte(installedJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	resources, err := claudecode.Plugins{}.Observe(env)
	if err != nil {
		t.Fatalf("Observe: unexpected error: %v", err)
	}
	if len(resources) != 1 {
		t.Fatalf("Observe: got %d resources, want 1", len(resources))
	}

	// This is exactly how resolve/pipeline.go builds the desired hash.
	want := lockfile.ContentHash(map[string]any{
		"marketplace": "trein-vertraging", "version": "2.16.0",
	})
	if resources[0].ContentHash != want {
		t.Errorf("ContentHash = %q, want %q (must match the desired hash so an\n"+
			"in-sync plugin diffs to Noop and a changed pin diffs to Update)",
			resources[0].ContentHash, want)
	}
}

// TestPluginsDiff_UnpinnedAlwaysUpdates is the end-to-end contract behind the
// SHA-versioned flow: an unpinned plugin must diff to Update on every install so
// `claude plugin update` runs and Claude Code's own SHA check decides whether
// there is anything new. Diffing to Noop here is the bug that made `ainfra
// install` a no-op for plugins — the desired hash and the observed hash agreed
// because neither reflected reality.
func TestPluginsDiff_UnpinnedAlwaysUpdates(t *testing.T) {
	mem := provider.NewMemFilesystem()
	// Claude Code resolved the unpinned plugin to the marketplace commit SHA.
	if err := mem.WriteFile("/home/user/.claude/plugins/installed_plugins.json",
		[]byte(`{"version":2,"plugins":{"tvt-config@trein-vertraging":[{"scope":"user","version":"d0164a938e37"}]}}`),
		0o644); err != nil {
		t.Fatal(err)
	}
	observed, err := claudecode.Plugins{}.Observe(provider.Env{FS: mem, Home: "/home/user"})
	if err != nil {
		t.Fatal(err)
	}

	// Desired, exactly as resolve/pipeline.go builds it for a manifest entry
	// with no version: pin.
	desired := []provider.Resource{{
		ID:      "tvt-config",
		Channel: "plugins",
		ContentHash: lockfile.ContentHash(map[string]any{
			"marketplace": "trein-vertraging", "version": "",
		}),
	}}

	plan := provider.DiffResources("plugins", desired, observed, desired, provider.DiffOpts{})
	if len(plan.Changes) != 1 {
		t.Fatalf("got %d changes, want 1", len(plan.Changes))
	}
	if plan.Changes[0].Kind != provider.ChangeUpdate {
		t.Errorf("Kind = %v, want ChangeUpdate so `claude plugin update` runs and\n"+
			"new commits actually reach users", plan.Changes[0].Kind)
	}
}

// TestPluginsDiff_PinnedInSyncNoops is the other half: a pinned plugin already at
// its pinned version must NOT re-run an update on every install, or the plan is
// permanently noisy and `--strict` drift checks never pass.
func TestPluginsDiff_PinnedInSyncNoops(t *testing.T) {
	mem := provider.NewMemFilesystem()
	if err := mem.WriteFile("/home/user/.claude/plugins/installed_plugins.json",
		[]byte(`{"version":2,"plugins":{"tvt-config@trein-vertraging":[{"scope":"user","version":"2.16.0"}]}}`),
		0o644); err != nil {
		t.Fatal(err)
	}
	observed, err := claudecode.Plugins{}.Observe(provider.Env{FS: mem, Home: "/home/user"})
	if err != nil {
		t.Fatal(err)
	}
	desired := []provider.Resource{{
		ID:      "tvt-config",
		Channel: "plugins",
		ContentHash: lockfile.ContentHash(map[string]any{
			"marketplace": "trein-vertraging", "version": "2.16.0",
		}),
	}}

	plan := provider.DiffResources("plugins", desired, observed, desired, provider.DiffOpts{})
	if len(plan.Changes) != 1 || plan.Changes[0].Kind != provider.ChangeNoop {
		t.Errorf("want a single Noop for an in-sync pin, got %+v", plan.Changes)
	}
}

func TestPluginsApply_RefreshRunsUpdate(t *testing.T) {
	// A ChangeRefresh is the unpinned form of an update and must still invoke
	// `claude plugin update`. If Apply ignored this kind, unpinned plugins
	// would silently stop tracking upstream while the plan still claimed to
	// be refreshing them — a far worse failure than the misleading label the
	// refresh kind was introduced to fix.
	runner := provider.NewFakeRunner()
	runner.Script["claude plugin update tvt-config@trein-vertraging"] = provider.FakeResult{}
	env := provider.Env{FS: provider.NewMemFilesystem(), Home: "/home/user", Runner: runner}

	plan := provider.ChannelPlan{
		Channel: "plugins",
		Changes: []provider.Change{
			{
				Kind: provider.ChangeRefresh,
				ID:   "tvt-config",
				Resource: provider.Resource{
					ID:            "tvt-config",
					Channel:       "plugins",
					AlwaysRefresh: true,
					Payload: map[string]any{
						"marketplace": "trein-vertraging",
						"version":     "",
					},
				},
			},
		},
	}

	result, err := claudecode.Plugins{}.Apply(env, plan)
	if err != nil {
		t.Fatalf("Apply: unexpected error: %v", err)
	}
	if len(result.Applied) != 1 {
		t.Fatalf("Applied = %d, want 1", len(result.Applied))
	}
	if len(runner.Calls) != 1 || runner.Calls[0] != "claude plugin update tvt-config@trein-vertraging" {
		t.Errorf("refresh must run `claude plugin update`; runner.Calls = %v", runner.Calls)
	}
	// An unpinned refresh has no pin to compare against, so it must not warn.
	if len(result.Warnings) != 0 {
		t.Errorf("Warnings = %v, want none for an unpinned refresh", result.Warnings)
	}
}
