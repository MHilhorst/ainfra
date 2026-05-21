package provider

import "testing"

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
