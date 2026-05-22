package claudedesktop_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/MHilhorst/ainfra/internal/provider"
	"github.com/MHilhorst/ainfra/internal/provider/claudedesktop"
)

// TestApplyCreatesServerAndPreservesForeign verifies that applying a create
// plan writes the new ainfra-owned server while leaving pre-existing foreign
// servers in the file untouched.
func TestApplyCreatesServerAndPreservesForeign(t *testing.T) {
	mem := provider.NewMemFilesystem()

	// Pre-populate the config file with a foreign server.
	initial := map[string]any{
		"mcpServers": map[string]any{
			"foreign": map[string]any{
				"command": "foreign-cmd",
				"args":    []any{"--foo"},
			},
		},
	}
	raw, err := json.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	configFile := "/home/Library/Application Support/Claude/claude_desktop_config.json"
	if err := mem.WriteFile(configFile, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	env := provider.Env{FS: mem, Home: "/home"}
	plan := provider.ChannelPlan{
		Channel: "mcpServers",
		Changes: []provider.Change{{
			Kind: provider.ChangeCreate,
			ID:   "github",
			Resource: provider.Resource{
				ID:      "github",
				Channel: "mcpServers",
				Payload: map[string]any{
					"command":   "npx",
					"args":      []any{"-y", "server-github"},
					"env":       map[string]any{"TOKEN": "x"},
					"transport": "stdio",
				},
			},
		}},
	}

	result, err := (claudedesktop.MCP{}).Apply(env, plan)
	if err != nil {
		t.Fatalf("Apply: unexpected error: %v", err)
	}
	if len(result.Applied) != 1 {
		t.Fatalf("Applied = %d, want 1", len(result.Applied))
	}

	out := string(mem.Files[configFile])

	// New server must be present.
	if !strings.Contains(out, `"github"`) {
		t.Errorf("new server 'github' missing from output:\n%s", out)
	}
	if !strings.Contains(out, `"npx"`) {
		t.Errorf("command 'npx' missing from output:\n%s", out)
	}

	// Foreign server must be preserved.
	if !strings.Contains(out, `"foreign"`) {
		t.Errorf("foreign server must be preserved:\n%s", out)
	}
	if !strings.Contains(out, `"foreign-cmd"`) {
		t.Errorf("foreign server command must be preserved:\n%s", out)
	}
}

// TestObserveMissingFileNoResources verifies that Observe returns (nil, nil)
// when the config file does not exist.
func TestObserveMissingFileNoResources(t *testing.T) {
	mem := provider.NewMemFilesystem()
	env := provider.Env{FS: mem, Home: "/home"}

	got, err := (claudedesktop.MCP{}).Observe(env)
	if err != nil {
		t.Fatalf("Observe: unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("Observe: got %d resources, want 0", len(got))
	}
}

// TestConfigPathPerOS verifies that configPathFor resolves to the correct
// OS-specific path for both darwin and windows.
func TestConfigPathPerOS(t *testing.T) {
	darwinPath := claudedesktop.ConfigPathFor("/home/u", "darwin")
	if !strings.Contains(darwinPath, "Application Support") {
		t.Errorf("darwin path %q should contain 'Application Support'", darwinPath)
	}
	if !strings.Contains(darwinPath, "claude_desktop_config.json") {
		t.Errorf("darwin path %q should end with claude_desktop_config.json", darwinPath)
	}

	windowsPath := claudedesktop.ConfigPathFor("/home/u", "windows")
	if !strings.Contains(windowsPath, "AppData") || !strings.Contains(windowsPath, "Roaming") {
		t.Errorf("windows path %q should contain 'AppData/Roaming'", windowsPath)
	}
	if !strings.Contains(windowsPath, "claude_desktop_config.json") {
		t.Errorf("windows path %q should end with claude_desktop_config.json", windowsPath)
	}
}
