package plugin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/MHilhorst/ainfra/internal/manifest"
)

func TestRenderPluginJSON(t *testing.T) {
	p := manifest.PluginBuild{
		Name:        "tvt-config",
		Description: "Team config",
		Marketplace: "trein-vertraging",
		Author:      manifest.PluginAuthor{Name: "Trein-Vertraging", URL: "https://github.com/trein-vertraging"},
		Repository:  "https://github.com/trein-vertraging/claude-config",
		License:     "UNLICENSED",
	}
	out, err := RenderPluginJSON(p, "2.11.0", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) == 0 || out[len(out)-1] != '\n' {
		t.Error("expected trailing newline")
	}

	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if doc["name"] != "tvt-config" || doc["version"] != "2.11.0" {
		t.Errorf("got name=%v version=%v", doc["name"], doc["version"])
	}
	skills, ok := doc["skills"].([]any)
	if !ok || len(skills) != 1 || skills[0] != "./skills/" {
		t.Errorf("expected skills [./skills/], got %v", doc["skills"])
	}
	agents, ok := doc["agents"].([]any)
	if !ok || len(agents) != 0 {
		t.Errorf("agents must render as an empty array when none given, got %v", doc["agents"])
	}
}

func TestRenderPluginJSONWithAgents(t *testing.T) {
	p := manifest.PluginBuild{Name: "tvt-config", Marketplace: "m"}
	out, err := RenderPluginJSON(p, "2.13.0", []string{"./agents/code-searcher.md"})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	agents, ok := doc["agents"].([]any)
	if !ok || len(agents) != 1 || agents[0] != "./agents/code-searcher.md" {
		t.Errorf("expected agents [./agents/code-searcher.md], got %v", doc["agents"])
	}
}

func TestAgentsRefs(t *testing.T) {
	root := t.TempDir()

	// Declared in content but directory missing -> empty.
	declared := manifest.PluginBuild{Name: "p", Content: []string{"skills/", "agents/"}}
	if got := AgentsRefs(root, declared); len(got) != 0 {
		t.Errorf("missing agents dir: expected empty, got %v", got)
	}

	if err := os.MkdirAll(filepath.Join(root, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"searcher.md", "designer.md", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(root, "agents", f), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Declared and files exist -> sorted .md file refs, non-md ignored.
	want := []string{"./agents/designer.md", "./agents/searcher.md"}
	got := AgentsRefs(root, declared)
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("declared+exists: expected %v, got %v", want, got)
	}

	// Default content paths include agents/ -> picked up without declaration.
	defaulted := manifest.PluginBuild{Name: "p"}
	if got := AgentsRefs(root, defaulted); len(got) != 2 {
		t.Errorf("default+exists: expected 2 refs, got %v", got)
	}

	// Content explicitly without agents/ -> empty even when files exist.
	without := manifest.PluginBuild{Name: "p", Content: []string{"skills/"}}
	if got := AgentsRefs(root, without); len(got) != 0 {
		t.Errorf("undeclared: expected empty, got %v", got)
	}
}
