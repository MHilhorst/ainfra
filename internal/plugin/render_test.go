package plugin

import (
	"encoding/json"
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
	out, err := RenderPluginJSON(p, "2.11.0")
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
	if _, ok := doc["agents"].([]any); !ok {
		t.Errorf("agents must render as a (possibly empty) array, got %v", doc["agents"])
	}
}
