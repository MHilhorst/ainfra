package provider

import (
	"errors"
	"testing"
)

// A channel that names a file the user already wrote must not overwrite it
// without keeping a copy — and unlike a prune, this happens on a plain
// `ainfra install` with no --prune anywhere.
func TestAdoptionIsBackedUpWithoutPrune(t *testing.T) {
	mem := NewMemFilesystem()
	p := &prunableStub{
		channel:  "commands",
		observed: []Resource{{ID: "stop", Channel: "commands", ContentHash: "theirs"}},
	}
	o := NewOrchestratorScoped(t.TempDir(), ScopeRepo, Env{FS: mem}, []Provider{p})
	// Deliberately no EnablePrune: adoption backup is not a prune feature.

	desired := map[string][]Resource{
		"commands": {{ID: "stop", Channel: "commands", ContentHash: "ours"}},
	}
	if _, err := o.ApplyAllRendered(desired, emptyLock()); err != nil {
		t.Fatalf("ApplyAllRendered: %v", err)
	}

	if len(p.backedUp) != 1 || p.backedUp[0] != "stop" {
		t.Errorf("backedUp = %v, want the overwritten file to be preserved", p.backedUp)
	}
}

// The same rule prune deletes follow: never destroy what has no copy. An
// overwrite is the worse case, because it leaves nothing behind to restore.
func TestAdoptionBackupFailureCancelsTheOverwrite(t *testing.T) {
	mem := NewMemFilesystem()
	p := &prunableStub{
		channel:   "commands",
		observed:  []Resource{{ID: "stop", Channel: "commands", ContentHash: "theirs"}},
		backupErr: errors.New("disk full"),
	}
	o := NewOrchestratorScoped(t.TempDir(), ScopeRepo, Env{FS: mem}, []Provider{p})

	desired := map[string][]Resource{
		"commands": {{ID: "stop", Channel: "commands", ContentHash: "ours"}},
	}
	results, err := o.ApplyAllRendered(desired, emptyLock())
	if err == nil {
		t.Error("err = nil, want the cancelled overwrite surfaced")
	}

	for _, plan := range p.applied {
		for _, c := range plan.Changes {
			if c.ID == "stop" && c.Kind == ChangeUpdate {
				t.Error("the overwrite was applied despite its backup failing")
			}
		}
	}
	found := false
	for _, res := range results {
		for _, f := range res.Failed {
			if f.Change.ID == "stop" {
				found = true
			}
		}
	}
	if !found {
		t.Error("the cancelled overwrite was not reported as a failure")
	}
}
