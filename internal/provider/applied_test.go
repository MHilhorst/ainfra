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

// TestUserAppliedLedgerRoundTrip covers the user-scope ledger that lives at
// $XDG_CONFIG_HOME/ainfra/applied.lock — same on-disk format as the repo-scope
// ledger, just at a per-user path.
func TestUserAppliedLedgerRoundTrip(t *testing.T) {
	// Redirect the XDG path so the test does not touch the real $HOME.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	got, err := ReadAppliedUser()
	if err != nil {
		t.Fatalf("ReadAppliedUser on missing: %v", err)
	}
	if got.Entries.Skills == nil {
		t.Error("missing-file user ledger must have non-nil entry maps")
	}

	l := &lockfile.Lock{Version: 1, Entries: lockfile.Entries{
		Skills: map[string]lockfile.Entry{"u": {Layer: "personal", ContentHash: "sha256:user"}},
	}}
	if err := WriteAppliedUser(l); err != nil {
		t.Fatalf("WriteAppliedUser: %v", err)
	}
	back, err := ReadAppliedUser()
	if err != nil {
		t.Fatal(err)
	}
	if back.Entries.Skills["u"].ContentHash != "sha256:user" {
		t.Errorf("user-scope round-trip lost the entry: %+v", back.Entries.Skills)
	}
}

// TestUserAppliedLedgerIndependentOfRepoLedger ensures the two ledgers never
// confuse each other — a write to the user scope must not touch any repo
// ledger and vice versa.
func TestUserAppliedLedgerIndependentOfRepoLedger(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root := t.TempDir()

	repoLock := &lockfile.Lock{Version: 1, Entries: lockfile.Entries{
		Skills: map[string]lockfile.Entry{"repo-skill": {Layer: "repo", ContentHash: "sha256:r"}},
	}}
	userLock := &lockfile.Lock{Version: 1, Entries: lockfile.Entries{
		Skills: map[string]lockfile.Entry{"user-skill": {Layer: "personal", ContentHash: "sha256:u"}},
	}}

	if err := WriteApplied(root, repoLock); err != nil {
		t.Fatalf("WriteApplied: %v", err)
	}
	if err := WriteAppliedUser(userLock); err != nil {
		t.Fatalf("WriteAppliedUser: %v", err)
	}

	repoBack, _ := ReadApplied(root)
	userBack, _ := ReadAppliedUser()

	if _, ok := repoBack.Entries.Skills["user-skill"]; ok {
		t.Errorf("repo ledger leaked user entry: %+v", repoBack.Entries.Skills)
	}
	if _, ok := userBack.Entries.Skills["repo-skill"]; ok {
		t.Errorf("user ledger leaked repo entry: %+v", userBack.Entries.Skills)
	}
}
