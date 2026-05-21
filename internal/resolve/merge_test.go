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
	merged, err := Merge([]LayerEntries{
		{Layer: "team", Entries: team},
		{Layer: "personal", Entries: personal},
	})
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if merged["srv"].Value != "personal" {
		t.Errorf("got %q, want personal (overridable)", merged["srv"].Value)
	}
}

func TestMergeAddsUniqueEntries(t *testing.T) {
	merged, err := Merge([]LayerEntries{
		{Layer: "repo", Entries: map[string]Entry{"a": {Value: "1"}}},
		{Layer: "personal", Entries: map[string]Entry{"b": {Value: "2"}}},
	})
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if len(merged) != 2 {
		t.Errorf("want 2 entries, got %d", len(merged))
	}
}

// team(overridable) -> repo(non-overridable) wins -> personal must NOT win,
// because repo's entry, once it replaces team's, is itself not overridable.
func TestMergeThreeLayerChain(t *testing.T) {
	merged, err := Merge([]LayerEntries{
		{Layer: "team", Entries: map[string]Entry{
			"srv": {Value: "team", Overridable: true},
		}},
		{Layer: "repo", Entries: map[string]Entry{
			"srv": {Value: "repo", Overridable: false},
		}},
		{Layer: "personal", Entries: map[string]Entry{
			"srv": {Value: "personal"},
		}},
	})
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if merged["srv"].Value != "repo" {
		t.Errorf("value = %q, want repo", merged["srv"].Value)
	}
	if merged["srv"].Layer != "repo" {
		t.Errorf("layer = %q, want repo", merged["srv"].Layer)
	}
}
