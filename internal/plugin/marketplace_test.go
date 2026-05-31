package plugin

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/MHilhorst/ainfra/internal/manifest"
)

const sampleMarketplace = `{
  "name": "trein-vertraging",
  "owner": { "name": "Trein-Vertraging" },
  "plugins": [
    {
      "name": "tvt-config",
      "source": "./",
      "description": "OLD description",
      "license": "UNLICENSED"
    },
    {
      "name": "claude-ads",
      "source": { "source": "github", "repo": "AgriciDaniel/claude-ads" },
      "description": "third party"
    }
  ]
}`

func TestMergeMarketplaceEntry(t *testing.T) {
	p := manifest.PluginBuild{
		Name:        "tvt-config",
		Description: "NEW description",
		Marketplace: "trein-vertraging",
	}
	out, err := MergeMarketplaceEntry([]byte(sampleMarketplace), p)
	if err != nil {
		t.Fatal(err)
	}

	var doc struct {
		Plugins []map[string]any `json:"plugins"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if len(doc.Plugins) != 2 {
		t.Fatalf("expected 2 plugins, got %d", len(doc.Plugins))
	}

	var self, third map[string]any
	for _, e := range doc.Plugins {
		switch e["name"] {
		case "tvt-config":
			self = e
		case "claude-ads":
			third = e
		}
	}
	if self["description"] != "NEW description" {
		t.Errorf("self description not updated: %v", self["description"])
	}
	if self["license"] != "UNLICENSED" || self["source"] != "./" {
		t.Errorf("self other fields not preserved: %v", self)
	}
	if third["description"] != "third party" {
		t.Errorf("third-party entry was modified: %v", third)
	}
	if out[len(out)-1] != '\n' {
		t.Error("expected trailing newline")
	}
}

func TestMergeMarketplaceEntry_MissingSelf(t *testing.T) {
	p := manifest.PluginBuild{Name: "absent", Marketplace: "m"}
	if _, err := MergeMarketplaceEntry([]byte(sampleMarketplace), p); err == nil ||
		!strings.Contains(err.Error(), "no marketplace entry") {
		t.Errorf("expected missing-entry error, got %v", err)
	}
}
