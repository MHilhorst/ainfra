package plugin

import (
	"encoding/json"
	"fmt"

	"github.com/MHilhorst/ainfra/internal/manifest"
)

// marketplaceDoc preserves top-level key order and passes every plugin entry
// through as raw JSON, so non-self entries are emitted unchanged.
type marketplaceDoc struct {
	Name     string            `json:"name"`
	Owner    json.RawMessage   `json:"owner,omitempty"`
	Metadata json.RawMessage   `json:"metadata,omitempty"`
	Plugins  []json.RawMessage `json:"plugins"`
}

// MergeMarketplaceEntry updates only the plugins[] entry whose name matches
// p.Name (its `name` and `description`), preserving that entry's other fields
// and every other entry verbatim. Returns an error if no self-entry exists.
func MergeMarketplaceEntry(existing []byte, p manifest.PluginBuild) ([]byte, error) {
	var doc marketplaceDoc
	if err := json.Unmarshal(existing, &doc); err != nil {
		return nil, fmt.Errorf("plugin: parse marketplace.json: %w", err)
	}

	found := false
	for i, raw := range doc.Plugins {
		var probe struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(raw, &probe); err != nil {
			return nil, fmt.Errorf("plugin: parse marketplace entry: %w", err)
		}
		if probe.Name != p.Name {
			continue
		}
		found = true

		var entry map[string]json.RawMessage
		if err := json.Unmarshal(raw, &entry); err != nil {
			return nil, err
		}
		entry["name"] = mustRaw(p.Name)
		entry["description"] = mustRaw(p.Description)
		merged, err := json.Marshal(entry)
		if err != nil {
			return nil, err
		}
		doc.Plugins[i] = merged
	}
	if !found {
		return nil, fmt.Errorf("plugin: no marketplace entry named %q in marketplace.json", p.Name)
	}

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

func mustRaw(s string) json.RawMessage {
	b, _ := json.Marshal(s)
	return b
}
