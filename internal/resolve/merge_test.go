package resolve

import "testing"

func TestMergeHigherLayerWins(t *testing.T) {
	team := map[string]Entry{"srv": {Value: "team", Overridable: false}}
	personal := map[string]Entry{"srv": {Value: "personal"}}
	merged, err := Merge([]LayerEntries{
		{Layer: "team", Entries: team},
		{Layer: "personal", Entries: personal},
	})
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if merged["srv"].Value != "team" {
		t.Errorf("got %q, want team (non-overridable wins)", merged["srv"].Value)
	}
}

func TestMergeOverridableLetsLowerLayerWin(t *testing.T) {
	team := map[string]Entry{"srv": {Value: "team", Overridable: true}}
	personal := map[string]Entry{"srv": {Value: "personal"}}
	merged, _ := Merge([]LayerEntries{
		{Layer: "team", Entries: team},
		{Layer: "personal", Entries: personal},
	})
	if merged["srv"].Value != "personal" {
		t.Errorf("got %q, want personal (overridable)", merged["srv"].Value)
	}
}

func TestMergeAddsUniqueEntries(t *testing.T) {
	merged, _ := Merge([]LayerEntries{
		{Layer: "repo", Entries: map[string]Entry{"a": {Value: "1"}}},
		{Layer: "personal", Entries: map[string]Entry{"b": {Value: "2"}}},
	})
	if len(merged) != 2 {
		t.Errorf("want 2 entries, got %d", len(merged))
	}
}
