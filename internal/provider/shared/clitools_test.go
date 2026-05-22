package shared_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/MHilhorst/ainfra/internal/provider"
	"github.com/MHilhorst/ainfra/internal/provider/shared"
)

func TestCLIToolsChannel(t *testing.T) {
	p := shared.CLITools{}
	if got := p.Channel(); got != "cliTools" {
		t.Fatalf("Channel() = %q, want %q", got, "cliTools")
	}
}

func TestCLIToolsObserve_ReturnsNil(t *testing.T) {
	mem := provider.NewMemFilesystem()
	env := provider.Env{FS: mem, Root: "/repo"}

	p := shared.CLITools{}
	resources, err := p.Observe(env)
	if err != nil {
		t.Fatalf("Observe: unexpected error: %v", err)
	}
	if resources != nil {
		t.Fatalf("Observe: got %v, want nil", resources)
	}
}

func TestCLIToolsApply_CreateWithBrew(t *testing.T) {
	runner := provider.NewFakeRunner()
	// brew list returns an error meaning "not installed"
	runner.Script["brew list --versions jq"] = provider.FakeResult{Err: errors.New("not found")}
	runner.Script["brew install jq"] = provider.FakeResult{Output: []byte("installed")}

	env := provider.Env{
		FS:     provider.NewMemFilesystem(),
		Runner: runner,
		Root:   "/repo",
	}

	plan := provider.ChannelPlan{
		Channel: "cliTools",
		Changes: []provider.Change{
			{
				Kind: provider.ChangeCreate,
				ID:   "jq",
				Resource: provider.Resource{
					ID:      "jq",
					Channel: "cliTools",
					Payload: map[string]any{
						"install": map[string]any{
							"brew": "jq",
						},
					},
				},
			},
		},
	}

	p := shared.CLITools{}
	result, err := p.Apply(env, plan)
	if err != nil {
		t.Fatalf("Apply: unexpected error: %v", err)
	}
	if result.Channel != "cliTools" {
		t.Errorf("result.Channel = %q, want %q", result.Channel, "cliTools")
	}
	if len(result.Applied) != 1 {
		t.Fatalf("result.Applied: got %d, want 1", len(result.Applied))
	}

	// verify brew install was called
	found := false
	for _, call := range runner.Calls {
		if call == "brew install jq" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'brew install jq' call; calls = %v", runner.Calls)
	}
}

func TestCLIToolsApply_CreateWithBrew_AlreadyInstalled(t *testing.T) {
	runner := provider.NewFakeRunner()
	// brew list succeeds meaning already installed
	runner.Script["brew list --versions jq"] = provider.FakeResult{Output: []byte("jq 1.7")}

	env := provider.Env{
		FS:     provider.NewMemFilesystem(),
		Runner: runner,
		Root:   "/repo",
	}

	plan := provider.ChannelPlan{
		Channel: "cliTools",
		Changes: []provider.Change{
			{
				Kind: provider.ChangeCreate,
				ID:   "jq",
				Resource: provider.Resource{
					ID:      "jq",
					Channel: "cliTools",
					Payload: map[string]any{
						"install": map[string]any{
							"brew": "jq",
						},
					},
				},
			},
		},
	}

	p := shared.CLITools{}
	result, err := p.Apply(env, plan)
	if err != nil {
		t.Fatalf("Apply: unexpected error: %v", err)
	}
	if len(result.Applied) != 1 {
		t.Fatalf("result.Applied: got %d, want 1", len(result.Applied))
	}

	// brew install should NOT have been called
	for _, call := range runner.Calls {
		if call == "brew install jq" {
			t.Error("brew install called but tool was already installed")
		}
	}
}

func TestCLIToolsApply_UnknownMethodToolAbsent_Fails(t *testing.T) {
	runner := provider.NewFakeRunner()
	// --version probe fails meaning not on PATH
	runner.Script["mytool --version"] = provider.FakeResult{Err: errors.New("exec: not found")}

	env := provider.Env{
		FS:     provider.NewMemFilesystem(),
		Runner: runner,
		Root:   "/repo",
	}

	plan := provider.ChannelPlan{
		Channel: "cliTools",
		Changes: []provider.Change{
			{
				Kind: provider.ChangeCreate,
				ID:   "mytool",
				Resource: provider.Resource{
					ID:      "mytool",
					Channel: "cliTools",
					Payload: map[string]any{
						"install": map[string]any{
							"manual": "download from example.com",
						},
					},
				},
			},
		},
	}

	p := shared.CLITools{}
	res, err := p.Apply(env, plan)
	if err != nil {
		t.Fatalf("Apply: per-entry failures go in ApplyResult.Failed, not the error return: %v", err)
	}
	if len(res.Failed) != 1 || res.Failed[0].Change.ID != "mytool" {
		t.Fatalf("Failed = %+v, want one failure for 'mytool'", res.Failed)
	}
	if !strings.Contains(res.Failed[0].Err.Error(), "install it manually") {
		t.Errorf("failure message should mention manual install, got: %v", res.Failed[0].Err)
	}
}

func TestCLIToolsApply_UnknownMethodToolPresent(t *testing.T) {
	runner := provider.NewFakeRunner()
	// --version probe succeeds meaning tool is on PATH
	runner.Script["mytool --version"] = provider.FakeResult{Output: []byte("mytool 1.0.0")}

	env := provider.Env{
		FS:     provider.NewMemFilesystem(),
		Runner: runner,
		Root:   "/repo",
	}

	plan := provider.ChannelPlan{
		Channel: "cliTools",
		Changes: []provider.Change{
			{
				Kind: provider.ChangeCreate,
				ID:   "mytool",
				Resource: provider.Resource{
					ID:      "mytool",
					Channel: "cliTools",
					Payload: map[string]any{
						"install": map[string]any{
							"manual": "download from example.com",
						},
					},
				},
			},
		},
	}

	p := shared.CLITools{}
	result, err := p.Apply(env, plan)
	if err != nil {
		t.Fatalf("Apply: unexpected error: %v", err)
	}
	if len(result.Applied) != 1 {
		t.Fatalf("result.Applied: got %d, want 1", len(result.Applied))
	}
}

func TestCLIToolsApply_DryRun_NoInstall(t *testing.T) {
	runner := provider.NewFakeRunner()

	env := provider.Env{
		FS:     provider.NewMemFilesystem(),
		Runner: runner,
		Root:   "/repo",
		DryRun: true,
	}

	plan := provider.ChannelPlan{
		Channel: "cliTools",
		Changes: []provider.Change{
			{
				Kind: provider.ChangeCreate,
				ID:   "jq",
				Resource: provider.Resource{
					ID:      "jq",
					Channel: "cliTools",
					Payload: map[string]any{
						"install": map[string]any{
							"brew": "jq",
						},
					},
				},
			},
		},
	}

	p := shared.CLITools{}
	result, err := p.Apply(env, plan)
	if err != nil {
		t.Fatalf("Apply DryRun: unexpected error: %v", err)
	}
	if len(result.Applied) != 1 {
		t.Fatalf("DryRun: result.Applied: got %d, want 1", len(result.Applied))
	}
	if len(runner.Calls) != 0 {
		t.Errorf("DryRun: expected no runner calls, got %v", runner.Calls)
	}
}

func TestCLIToolsApply_MultiMethodDeterministic(t *testing.T) {
	// Both brew and npm are recognised; sorted order means brew comes first.
	// Running Apply multiple times must always invoke the same adapter.
	for i := range 5 {
		runner := provider.NewFakeRunner()
		// brew: not installed
		runner.Script["brew list --versions mything"] = provider.FakeResult{Err: errors.New("not found")}
		runner.Script["brew install mything"] = provider.FakeResult{Output: []byte("installed")}
		// npm would also be recognised but must never be reached
		runner.Script["npm ls -g --depth 0 mything"] = provider.FakeResult{Err: errors.New("not found")}
		runner.Script["npm install -g mything"] = provider.FakeResult{Output: []byte("installed")}

		env := provider.Env{
			FS:     provider.NewMemFilesystem(),
			Runner: runner,
			Root:   "/repo",
		}

		plan := provider.ChannelPlan{
			Channel: "cliTools",
			Changes: []provider.Change{
				{
					Kind: provider.ChangeCreate,
					ID:   "mything",
					Resource: provider.Resource{
						ID:      "mything",
						Channel: "cliTools",
						Payload: map[string]any{
							"install": map[string]any{
								"brew": "mything",
								"npm":  "mything",
							},
						},
					},
				},
			},
		}

		p := shared.CLITools{}
		_, err := p.Apply(env, plan)
		if err != nil {
			t.Fatalf("iteration %d: Apply: unexpected error: %v", i, err)
		}

		// brew must have been used (sorted order: brew < npm)
		brewInstallSeen := false
		npmInstallSeen := false
		for _, call := range runner.Calls {
			if call == "brew install mything" {
				brewInstallSeen = true
			}
			if call == "npm install -g mything" {
				npmInstallSeen = true
			}
		}
		if !brewInstallSeen {
			t.Errorf("iteration %d: expected brew to be selected (sorted first), calls = %v", i, runner.Calls)
		}
		if npmInstallSeen {
			t.Errorf("iteration %d: npm must not be used when brew is selected first, calls = %v", i, runner.Calls)
		}
	}
}

func TestCLIToolsApply_Delete_Noop(t *testing.T) {
	runner := provider.NewFakeRunner()

	env := provider.Env{
		FS:     provider.NewMemFilesystem(),
		Runner: runner,
		Root:   "/repo",
	}

	plan := provider.ChannelPlan{
		Channel: "cliTools",
		Changes: []provider.Change{
			{
				Kind: provider.ChangeDelete,
				ID:   "jq",
				Resource: provider.Resource{
					ID:      "jq",
					Channel: "cliTools",
				},
			},
		},
	}

	p := shared.CLITools{}
	result, err := p.Apply(env, plan)
	if err != nil {
		t.Fatalf("Apply Delete: unexpected error: %v", err)
	}
	if len(result.Applied) != 1 {
		t.Fatalf("Delete: result.Applied: got %d, want 1", len(result.Applied))
	}
	if len(runner.Calls) != 0 {
		t.Errorf("Delete: expected no runner calls (no-op), got %v", runner.Calls)
	}
}

func TestCLIToolsApply_NoInstallSkipsAdapter(t *testing.T) {
	runner := provider.NewFakeRunner() // every call is unscripted -> errors if reached
	env := provider.Env{
		FS:        provider.NewMemFilesystem(),
		Runner:    runner,
		Root:      "/repo",
		NoInstall: true,
	}

	plan := provider.ChannelPlan{
		Channel: "cliTools",
		Changes: []provider.Change{{
			Kind: provider.ChangeCreate,
			ID:   "jq",
			Resource: provider.Resource{
				ID:      "jq",
				Channel: "cliTools",
				Payload: map[string]any{
					"install": map[string]any{"brew": map[string]any{}},
				},
			},
		}},
	}

	res, err := shared.CLITools{}.Apply(env, plan)
	if err != nil {
		t.Fatalf("Apply with NoInstall: unexpected error: %v", err)
	}
	if len(runner.Calls) != 0 {
		t.Errorf("NoInstall: expected no commands run, got %v", runner.Calls)
	}
	if len(res.Applied) != 1 {
		t.Errorf("NoInstall: expected the change still recorded as applied, got %+v", res.Applied)
	}
}

func TestCLIToolsApply_OneFailureDoesNotAbortSiblings(t *testing.T) {
	runner := provider.NewFakeRunner()
	// "good" installs cleanly.
	runner.Script["brew list --versions good"] = provider.FakeResult{Err: errors.New("absent")}
	runner.Script["brew install good"] = provider.FakeResult{Output: []byte("ok")}
	// "bad" is absent and its install fails.
	runner.Script["brew list --versions bad"] = provider.FakeResult{Err: errors.New("absent")}
	runner.Script["brew install bad"] = provider.FakeResult{Err: errors.New("brew install bad: network error")}

	env := provider.Env{FS: provider.NewMemFilesystem(), Runner: runner, Root: "/repo"}

	mkChange := func(id string) provider.Change {
		return provider.Change{
			Kind: provider.ChangeCreate,
			ID:   id,
			Resource: provider.Resource{
				ID:      id,
				Channel: "cliTools",
				Payload: map[string]any{"install": map[string]any{"brew": map[string]any{}}},
			},
		}
	}
	plan := provider.ChannelPlan{
		Channel: "cliTools",
		Changes: []provider.Change{mkChange("good"), mkChange("bad")},
	}

	res, err := shared.CLITools{}.Apply(env, plan)
	if err != nil {
		t.Fatalf("Apply should not return a top-level error for a per-entry failure: %v", err)
	}
	if len(res.Applied) != 1 || res.Applied[0].ID != "good" {
		t.Errorf("Applied = %+v, want one entry 'good'", res.Applied)
	}
	if len(res.Failed) != 1 || res.Failed[0].Change.ID != "bad" {
		t.Fatalf("Failed = %+v, want one failure 'bad'", res.Failed)
	}
	if res.Failed[0].Err == nil {
		t.Error("Failed[0].Err is nil; want the underlying install error")
	}
}

func TestCLIToolsApply_NoInstallSkipsDeclareCheckProbe(t *testing.T) {
	runner := provider.NewFakeRunner()
	env := provider.Env{
		FS:        provider.NewMemFilesystem(),
		Runner:    runner,
		Root:      "/repo",
		NoInstall: true,
	}

	// No recognised adapter — normally falls through to a `tool --version` probe.
	plan := provider.ChannelPlan{
		Channel: "cliTools",
		Changes: []provider.Change{{
			Kind: provider.ChangeCreate,
			ID:   "some-tool",
			Resource: provider.Resource{
				ID:      "some-tool",
				Channel: "cliTools",
				Payload: map[string]any{
					"install": map[string]any{"manual": map[string]any{}},
				},
			},
		}},
	}

	res, err := shared.CLITools{}.Apply(env, plan)
	if err != nil {
		t.Fatalf("Apply with NoInstall: unexpected error: %v", err)
	}
	if len(runner.Calls) != 0 {
		t.Errorf("NoInstall: expected no probe, got %v", runner.Calls)
	}
	if len(res.Applied) != 1 {
		t.Errorf("NoInstall: expected the change still recorded as applied, got %+v", res.Applied)
	}
}
