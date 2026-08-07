package provider

import (
	"reflect"
	"testing"
)

func res(id, hash string) Resource { return Resource{ID: id, ContentHash: hash} }

func find(p ChannelPlan, id string) (Change, bool) {
	for _, c := range p.Changes {
		if c.ID == id {
			return c, true
		}
	}
	return Change{}, false
}

func TestDiffResources(t *testing.T) {
	desired := []Resource{res("keep", "h1"), res("changed", "h2new"), res("new", "h3")}
	observed := []Resource{res("keep", "h1"), res("changed", "h2old"), res("foreign", "hX")}
	prior := []Resource{res("keep", "h1"), res("changed", "h2old"), res("gone", "h4")}

	p := DiffResources("skills", desired, observed, prior, DiffOpts{})

	want := map[string]ChangeKind{
		"keep": ChangeNoop, "changed": ChangeUpdate, "new": ChangeCreate, "gone": ChangeDelete,
	}
	for id, kind := range want {
		c, ok := find(p, id)
		if !ok {
			t.Errorf("%s: no change emitted", id)
			continue
		}
		if c.Kind != kind {
			t.Errorf("%s: kind = %v, want %v", id, c.Kind, kind)
		}
	}
	if _, ok := find(p, "foreign"); ok {
		t.Error("a resource owned by neither prior nor desired must be left alone")
	}
}

func TestDiffResourcesTombstoneDeletesForeignServer(t *testing.T) {
	// A tombstone (enabled: false) must remove a matching resource from the
	// machine even when ainfra never installed it — i.e. it is in observed but
	// not in prior. This is the "retire a server everywhere" case.
	desired := []Resource{{ID: "linear-server", Channel: "mcpServers", Tombstone: true}}
	observed := []Resource{res("linear-server", "hX")}
	prior := []Resource{}

	p := DiffResources("mcpServers", desired, observed, prior, DiffOpts{})

	c, ok := find(p, "linear-server")
	if !ok {
		t.Fatal("tombstone for a present server must emit a delete")
	}
	if c.Kind != ChangeDelete {
		t.Errorf("kind = %v, want ChangeDelete", c.Kind)
	}
}

func TestDiffResourcesTombstoneAbsentIsNoop(t *testing.T) {
	// A tombstone for a server that is not on the machine must do nothing —
	// not a delete, not a create.
	desired := []Resource{{ID: "linear-server", Channel: "mcpServers", Tombstone: true}}
	observed := []Resource{res("keep", "h1")}
	prior := []Resource{}

	p := DiffResources("mcpServers", desired, observed, prior, DiffOpts{})

	if c, ok := find(p, "linear-server"); ok {
		t.Errorf("tombstone for an absent server must emit no change, got %v", c.Kind)
	}
}

func TestDiffResourcesTombstoneNeverCreates(t *testing.T) {
	// A tombstone must never be installed, even if absent from the machine.
	desired := []Resource{{ID: "linear-server", Channel: "mcpServers", Tombstone: true}}
	p := DiffResources("mcpServers", desired, []Resource{}, []Resource{}, DiffOpts{})
	for _, c := range p.Changes {
		if c.ID == "linear-server" && c.Kind == ChangeCreate {
			t.Fatal("tombstone must never produce a create")
		}
	}
}

func TestDiffResourcesCarriesResource(t *testing.T) {
	desiredNew := Resource{ID: "new", Channel: "skills", ContentHash: "h3", Payload: map[string]any{"k": "v"}}
	priorGone := Resource{ID: "gone", Channel: "skills", ContentHash: "h4"}

	desired := []Resource{desiredNew}
	observed := []Resource{}
	prior := []Resource{priorGone}

	p := DiffResources("skills", desired, observed, prior, DiffOpts{})

	create, ok := find(p, "new")
	if !ok {
		t.Fatal("expected a create change for 'new'")
	}
	if !reflect.DeepEqual(create.Resource, desiredNew) {
		t.Errorf("create change Resource = %+v, want %+v", create.Resource, desiredNew)
	}

	del, ok := find(p, "gone")
	if !ok {
		t.Fatal("expected a delete change for 'gone'")
	}
	if !reflect.DeepEqual(del.Resource, priorGone) {
		t.Errorf("delete change Resource = %+v, want %+v", del.Resource, priorGone)
	}
}

func TestDiffResourcesAlwaysRefreshReportsRefreshNotDrift(t *testing.T) {
	// An unpinned resource hashes differently from the machine on every run
	// by design (the upstream version is authoritative). That must plan as a
	// refresh, not as drift — "out of sync" would describe a divergence no
	// run can resolve, which is what made every ainfra install look dirty.
	desired := []Resource{{ID: "unpinned", ContentHash: "want", AlwaysRefresh: true}}
	observed := []Resource{res("unpinned", "got")}
	prior := []Resource{res("unpinned", "got")}

	c, ok := find(DiffResources("plugins", desired, observed, prior, DiffOpts{}), "unpinned")
	if !ok {
		t.Fatal("unpinned: no change emitted")
	}
	if c.Kind != ChangeRefresh {
		t.Errorf("kind = %v, want ChangeRefresh", c.Kind)
	}
	if c.Detail != "unpinned — will check for updates" {
		t.Errorf("detail = %q, must not claim the resource drifted", c.Detail)
	}
}

func TestDiffResourcesAlwaysRefreshStillNoopsWhenHashesMatch(t *testing.T) {
	// AlwaysRefresh is not "always mutate": it only reclassifies a genuine
	// hash difference. A pinned-and-matching resource stays a noop, so the
	// flag can never manufacture work out of nothing.
	desired := []Resource{{ID: "same", ContentHash: "h", AlwaysRefresh: true}}
	observed := []Resource{res("same", "h")}

	c, ok := find(DiffResources("plugins", desired, observed, nil, DiffOpts{}), "same")
	if !ok {
		t.Fatal("same: no change emitted")
	}
	if c.Kind != ChangeNoop {
		t.Errorf("kind = %v, want ChangeNoop", c.Kind)
	}
}

func TestDiffResourcesRefreshIsNotSilentlyAppliedAsUpdate(t *testing.T) {
	// Regression guard: a resource without the flag must keep reporting real
	// drift as ChangeUpdate. If AlwaysRefresh ever leaked into the default
	// path, genuine divergence would be downgraded to routine noise.
	desired := []Resource{res("drifted", "want")}
	observed := []Resource{res("drifted", "got")}
	// Prior matters: drift is what ainfra installed and something changed.
	// With no prior this is an adoption, covered separately below.
	prior := []Resource{res("drifted", "installed")}

	c, _ := find(DiffResources("plugins", desired, observed, prior, DiffOpts{}), "drifted")
	if c.Kind != ChangeUpdate {
		t.Errorf("kind = %v, want ChangeUpdate", c.Kind)
	}
	if c.Detail != "out of sync — will be updated" {
		t.Errorf("detail = %q, want the drift phrasing", c.Detail)
	}
	if c.Adopts {
		t.Error("a resource ainfra installed is not an adoption")
	}
}

func TestDiffResourcesFlagsOverwriteOfAFileAinfraNeverInstalled(t *testing.T) {
	// The user already had a file at this id and ainfra never recorded it, so
	// the write destroys their only copy. It must not be reported as routine
	// drift, and the orchestrator needs the flag to back the file up first.
	desired := []Resource{res("stop", "ours")}
	observed := []Resource{res("stop", "theirs")}

	c, ok := find(DiffResources("commands", desired, observed, nil, DiffOpts{}), "stop")
	if !ok {
		t.Fatal("stop: no change emitted")
	}
	if c.Kind != ChangeUpdate {
		t.Errorf("kind = %v, want ChangeUpdate", c.Kind)
	}
	if !c.Adopts {
		t.Error("overwriting a file absent from prior must set Adopts")
	}
	if c.Detail == "out of sync — will be updated" {
		t.Error("adoption must not borrow the drift phrasing — that is what hid it")
	}
}

func TestDiffResourcesMatchingUntrackedFileIsStillANoop(t *testing.T) {
	// Adoption is about writes, not about being untracked. A file whose
	// content already matches is not overwritten, so nothing is at risk and
	// nothing should be backed up.
	desired := []Resource{res("stop", "same")}
	observed := []Resource{res("stop", "same")}

	c, _ := find(DiffResources("commands", desired, observed, nil, DiffOpts{}), "stop")
	if c.Kind != ChangeNoop {
		t.Errorf("kind = %v, want ChangeNoop", c.Kind)
	}
	if c.Adopts {
		t.Error("a noop writes nothing and must not be flagged as an adoption")
	}
}

func TestDiffPruneRemovesUntracked(t *testing.T) {
	desired := []Resource{{ID: "kept", Channel: "skills"}}
	observed := []Resource{{ID: "kept", Channel: "skills"}, {ID: "stray", Channel: "skills"}}
	p := DiffResources("skills", desired, observed, nil, DiffOpts{Prune: true})

	c, ok := find(p, "stray")
	if !ok {
		t.Fatal("stray not in plan")
	}
	if c.Kind != ChangeDelete {
		t.Errorf("Kind = %v, want ChangeDelete", c.Kind)
	}
	if !c.Prune {
		t.Error("Prune = false, want true")
	}
}

func TestDiffWithoutPruneLeavesUntracked(t *testing.T) {
	desired := []Resource{{ID: "kept", Channel: "skills"}}
	observed := []Resource{{ID: "kept", Channel: "skills"}, {ID: "stray", Channel: "skills"}}
	p := DiffResources("skills", desired, observed, nil, DiffOpts{})

	if _, ok := find(p, "stray"); ok {
		t.Error("stray in plan without Prune; untracked resources must be left alone")
	}
}

func TestDiffPruneSkipsTombstonedID(t *testing.T) {
	desired := []Resource{{ID: "gone", Channel: "skills", Tombstone: true}}
	observed := []Resource{{ID: "gone", Channel: "skills"}}
	p := DiffResources("skills", desired, observed, nil, DiffOpts{Prune: true})

	n := 0
	for _, c := range p.Changes {
		if c.ID == "gone" && c.Kind == ChangeDelete {
			n++
		}
	}
	if n != 1 {
		t.Errorf("delete count = %d, want 1 (tombstone must not double up with prune)", n)
	}
}

func TestDiffPruneSkipsPriorID(t *testing.T) {
	observed := []Resource{{ID: "retired", Channel: "skills"}}
	prior := []Resource{{ID: "retired", Channel: "skills"}}
	p := DiffResources("skills", nil, observed, prior, DiffOpts{Prune: true})

	n := 0
	for _, c := range p.Changes {
		if c.ID == "retired" && c.Kind == ChangeDelete {
			n++
		}
	}
	if n != 1 {
		t.Errorf("delete count = %d, want 1 (prior delete must not double up with prune)", n)
	}
	c, _ := find(p, "retired")
	if c.Prune {
		t.Error("Prune = true for a prior-tracked delete; only untracked deletes are prune deletes")
	}
}
