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

	p := DiffResources("skills", desired, observed, prior)

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

	p := DiffResources("mcpServers", desired, observed, prior)

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

	p := DiffResources("mcpServers", desired, observed, prior)

	if c, ok := find(p, "linear-server"); ok {
		t.Errorf("tombstone for an absent server must emit no change, got %v", c.Kind)
	}
}

func TestDiffResourcesTombstoneNeverCreates(t *testing.T) {
	// A tombstone must never be installed, even if absent from the machine.
	desired := []Resource{{ID: "linear-server", Channel: "mcpServers", Tombstone: true}}
	p := DiffResources("mcpServers", desired, []Resource{}, []Resource{})
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

	p := DiffResources("skills", desired, observed, prior)

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
