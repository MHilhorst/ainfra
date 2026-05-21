package provider

import (
	"testing"

	"github.com/MHilhorst/ainfra/internal/lockfile"
)

func TestAppliedLedgerRoundTrip(t *testing.T) {
	root := t.TempDir()
	got, err := ReadApplied(root)
	if err != nil {
		t.Fatalf("ReadApplied on missing: %v", err)
	}
	if got.Entries.Skills == nil {
		t.Error("missing-file ledger must have non-nil entry maps")
	}
	l := &lockfile.Lock{Version: 1, Entries: lockfile.Entries{
		Skills: map[string]lockfile.Entry{"s": {Layer: "repo", ContentHash: "sha256:x"}},
	}}
	if err := WriteApplied(root, l); err != nil {
		t.Fatalf("WriteApplied: %v", err)
	}
	back, err := ReadApplied(root)
	if err != nil {
		t.Fatal(err)
	}
	if back.Entries.Skills["s"].ContentHash != "sha256:x" {
		t.Errorf("round-trip lost the entry: %+v", back.Entries.Skills)
	}
}
