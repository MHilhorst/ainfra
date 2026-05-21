package resolve

import (
	"reflect"
	"testing"

	"github.com/MHilhorst/ainfra/internal/manifest"
)

func TestMergeToolsUnionsAcrossLayers(t *testing.T) {
	got := MergeTools(
		&manifest.Tools{Permissions: &manifest.Permissions{Allow: []string{"A"}}},
		&manifest.Tools{Permissions: &manifest.Permissions{Allow: []string{"B"}}},
	)
	if !reflect.DeepEqual(got.Permissions.Allow, []string{"A", "B"}) {
		t.Errorf("allow = %v, want [A B] (a lower layer extends, never shrinks)", got.Permissions.Allow)
	}
}

func TestMergeToolsDenyBeatsAllow(t *testing.T) {
	// A developer's personal layer cannot lift a team deny by also allowing it.
	got := MergeTools(
		&manifest.Tools{Permissions: &manifest.Permissions{Deny: []string{"Bash(rm:*)"}}},
		&manifest.Tools{Permissions: &manifest.Permissions{Allow: []string{"Bash(rm:*)"}}},
	)
	if len(got.Permissions.Allow) != 0 {
		t.Errorf("allow = %v, want empty — deny wins", got.Permissions.Allow)
	}
	if !reflect.DeepEqual(got.Permissions.Deny, []string{"Bash(rm:*)"}) {
		t.Errorf("deny = %v, want [Bash(rm:*)]", got.Permissions.Deny)
	}
}

func TestMergeToolsAskBeatsAllow(t *testing.T) {
	got := MergeTools(
		&manifest.Tools{Permissions: &manifest.Permissions{Ask: []string{"X"}}},
		&manifest.Tools{Permissions: &manifest.Permissions{Allow: []string{"X"}}},
	)
	if len(got.Permissions.Allow) != 0 {
		t.Errorf("allow = %v, want empty — ask wins over allow", got.Permissions.Allow)
	}
	if !reflect.DeepEqual(got.Permissions.Ask, []string{"X"}) {
		t.Errorf("ask = %v, want [X]", got.Permissions.Ask)
	}
}

func TestMergeToolsDisabledUnionDedups(t *testing.T) {
	got := MergeTools(
		&manifest.Tools{Builtins: &manifest.Builtins{Disabled: []string{"WebFetch"}}},
		&manifest.Tools{Builtins: &manifest.Builtins{Disabled: []string{"WebSearch", "WebFetch"}}},
	)
	if !reflect.DeepEqual(got.Builtins.Disabled, []string{"WebFetch", "WebSearch"}) {
		t.Errorf("disabled = %v, want [WebFetch WebSearch]", got.Builtins.Disabled)
	}
}

// The union is order-independent: layer order must not change the result.
func TestMergeToolsOrderIndependent(t *testing.T) {
	a := &manifest.Tools{Permissions: &manifest.Permissions{Allow: []string{"A"}, Deny: []string{"D"}}}
	b := &manifest.Tools{Permissions: &manifest.Permissions{Allow: []string{"B"}}}
	if !reflect.DeepEqual(MergeTools(a, b), MergeTools(b, a)) {
		t.Error("MergeTools is not order-independent")
	}
}

func TestMergeToolsAllNilIsNil(t *testing.T) {
	if got := MergeTools(nil, nil); got != nil {
		t.Errorf("got %v, want nil when no layer defines tools", got)
	}
}
