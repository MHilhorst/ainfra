package plugin

import "testing"

func TestDecide(t *testing.T) {
	// Unchanged content, no bump -> no-op.
	d, err := Decide("h", "h", "2.11.0", "")
	if err != nil || d.Action != ActionNoop {
		t.Fatalf("noop case: action=%q err=%v", d.Action, err)
	}

	// Changed content, no bump -> drift error.
	if _, err := Decide("new", "old", "2.11.0", ""); err == nil {
		t.Error("expected drift error when content changed without bump")
	}

	// Changed content, patch bump -> release.
	d, err = Decide("new", "old", "2.11.0", "patch")
	if err != nil {
		t.Fatal(err)
	}
	if d.Action != ActionRelease || d.NewVersion != "2.11.1" || d.OldVersion != "2.11.0" {
		t.Errorf("got %+v", d)
	}

	// Unchanged content but explicit bump -> still releases (metadata-only).
	d, err = Decide("h", "h", "2.11.0", "minor")
	if err != nil || d.Action != ActionRelease || d.NewVersion != "2.12.0" {
		t.Errorf("explicit bump on unchanged content: %+v err=%v", d, err)
	}
}
