package schema

import (
	"encoding/json"
	"testing"
)

func TestGenerateCoversEveryChannel(t *testing.T) {
	props, ok := Generate()["properties"].(map[string]any)
	if !ok {
		t.Fatal("generated schema has no properties object")
	}
	// Every top-level manifest key the loader decodes must appear in the
	// schema. The schema is reflected from manifest.Manifest, so a new channel
	// is covered automatically — this test guards against an accidental gap.
	for _, channel := range []string{
		"version", "extends", "preconditions", "cliTools", "backgroundServices",
		"secrets", "templates", "mcpServers", "hooks", "commands",
		"skills", "plugins", "rules", "tools",
	} {
		if _, present := props[channel]; !present {
			t.Errorf("schema is missing the %q key", channel)
		}
	}
}

func TestGenerateMarshalsToJSON(t *testing.T) {
	if _, err := json.Marshal(Generate()); err != nil {
		t.Fatalf("schema does not marshal to JSON: %v", err)
	}
}

// additionalProperties:false at the root mirrors the loader's strict decoding,
// so the editor schema and the parser reject the same unknown keys.
func TestGenerateRejectsUnknownKeys(t *testing.T) {
	if got := Generate()["additionalProperties"]; got != false {
		t.Errorf("root additionalProperties = %v, want false", got)
	}
}
