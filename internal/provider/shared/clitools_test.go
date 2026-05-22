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

func TestCLIToolsObserve_NoLedgerEmpty(t *testing.T) {
	// Observe sources managed tools from the applied ledger; with no ledger
	// present it reports nothing.
	env := provider.Env{Root: t.TempDir()}

	resources, err := shared.CLITools{}.Observe(env)
	if err != nil {
		t.Fatalf("Observe: unexpected error: %v", err)
	}
	if len(resources) != 0 {
		t.Fatalf("Observe: got %d resources with no ledger, want 0", len(resources))
	}
}

func TestCLIToolsApply_CreateWithBrewFormula(t *testing.T) {
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
						"install": map[string]map[string]any{
							"brew": {"formula": "jq"},
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

	// verify brew install was called with the formula name, not the id
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

func TestCLIToolsApply_CreateWithBrewCask(t *testing.T) {
	runner := provider.NewFakeRunner()
	runner.Script["brew list --cask --versions 1password-cli"] = provider.FakeResult{Err: errors.New("not found")}
	runner.Script["brew install --cask 1password-cli"] = provider.FakeResult{Output: []byte("installed")}

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
				ID:   "op",
				Resource: provider.Resource{
					ID:      "op",
					Channel: "cliTools",
					Payload: map[string]any{
						"install": map[string]map[string]any{
							"brew": {"cask": "1password-cli"},
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

	found := false
	for _, call := range runner.Calls {
		if call == "brew install --cask 1password-cli" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'brew install --cask 1password-cli' call; calls = %v", runner.Calls)
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
						"install": map[string]map[string]any{
							"brew": {"formula": "jq"},
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

// TestCLIToolsApply_CheckCommand verifies that when no adapter matches, the
// check.command from the payload is used as the probe instead of "<id> --version".
func TestCLIToolsApply_CheckCommand(t *testing.T) {
	runner := provider.NewFakeRunner()
	// The check command is "mysql --version", not "mysql-client --version"
	runner.Script["mysql --version"] = provider.FakeResult{Output: []byte("mysql 8.0")}

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
				ID:   "mysql-client",
				Resource: provider.Resource{
					ID:      "mysql-client",
					Channel: "cliTools",
					Payload: map[string]any{
						"install": map[string]map[string]any{
							"manual": {"url": "https://example.com"},
						},
						"check": map[string]any{
							"command": "mysql --version",
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

	// The runner must have seen "mysql --version", not "mysql-client --version"
	found := false
	for _, call := range runner.Calls {
		if call == "mysql --version" {
			found = true
		}
		if call == "mysql-client --version" {
			t.Errorf("must not probe with id --version when check.command is set; calls = %v", runner.Calls)
		}
	}
	if !found {
		t.Errorf("expected 'mysql --version' probe call; calls = %v", runner.Calls)
	}
}

func TestCLIToolsApply_UnknownMethodToolAbsent_ReturnsError(t *testing.T) {
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
						"install": map[string]map[string]any{
							"manual": {"url": "download from example.com"},
						},
					},
				},
			},
		},
	}

	p := shared.CLITools{}
	_, err := p.Apply(env, plan)
	if err == nil {
		t.Fatal("Apply: expected error for absent tool with no supported install method, got nil")
	}
	if !strings.Contains(err.Error(), "install it manually") {
		t.Errorf("error message should mention manual install, got: %v", err)
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
						"install": map[string]map[string]any{
							"manual": {"url": "download from example.com"},
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
						"install": map[string]map[string]any{
							"brew": {"formula": "jq"},
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
							"install": map[string]map[string]any{
								"brew": {"formula": "mything"},
								"npm":  {"package": "mything"},
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
