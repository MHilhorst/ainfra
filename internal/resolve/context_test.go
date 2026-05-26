package resolve

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/MHilhorst/ainfra/internal/manifest"
)

func TestSelectorMatchesNilAndEmpty(t *testing.T) {
	ctx := DefaultContext()
	if !SelectorMatches(nil, ctx) {
		t.Error("nil selector must match every context")
	}
	if !SelectorMatches(&manifest.Selector{}, ctx) {
		t.Error("empty selector must match every context")
	}
}

func TestSelectorMatchesIdentity(t *testing.T) {
	s := &manifest.Selector{Identities: []string{"ci", "release-bot"}}
	if !SelectorMatches(s, ResolutionContext{Identity: "ci"}) {
		t.Error("ci identity should match")
	}
	if SelectorMatches(s, ResolutionContext{Identity: "human"}) {
		t.Error("human identity should not match a ci-only selector")
	}
}

func TestSelectorMatchesPaths(t *testing.T) {
	s := &manifest.Selector{Paths: []string{"services/api"}}
	cases := map[string]bool{
		"services/api":            true,
		"services/api/v2":         true,
		"services/api/internal/x": true,
		"services/web":            false,
		".":                       false,
	}
	for path, want := range cases {
		got := SelectorMatches(s, ResolutionContext{InvocationPath: path})
		if got != want {
			t.Errorf("path %q: got %v, want %v", path, got, want)
		}
	}
}

func TestSelectorMatchesGlob(t *testing.T) {
	s := &manifest.Selector{Paths: []string{"services/*"}}
	if !SelectorMatches(s, ResolutionContext{InvocationPath: "services/api"}) {
		t.Error("glob services/* should match services/api")
	}
	if SelectorMatches(s, ResolutionContext{InvocationPath: "services/api/v2"}) {
		// path.Match's lack of ** is documented; services/* matches one segment.
		// Confirm the limitation explicitly so a future relaxation is intentional.
		t.Error("glob services/* should not match services/api/v2 (no ** in v1)")
	}
}

func TestSelectorCombinedAxesAreAnd(t *testing.T) {
	s := &manifest.Selector{
		Identities: []string{"ci"},
		Paths:      []string{"services/api"},
	}
	if !SelectorMatches(s, ResolutionContext{Identity: "ci", InvocationPath: "services/api"}) {
		t.Error("both axes matching must match")
	}
	if SelectorMatches(s, ResolutionContext{Identity: "ci", InvocationPath: "services/web"}) {
		t.Error("identity match alone must not pass when paths fail")
	}
	if SelectorMatches(s, ResolutionContext{Identity: "human", InvocationPath: "services/api"}) {
		t.Error("path match alone must not pass when identity fails")
	}
}

func TestNewContextFromEnvFlagWinsOverEnv(t *testing.T) {
	t.Setenv("AINFRA_IDENTITY", "from-env")
	c := NewContextFromEnv("from-flag", "", "")
	if c.Identity != "from-flag" {
		t.Errorf("Identity = %q, want from-flag", c.Identity)
	}
}

func TestNewContextFromEnvEnvFallback(t *testing.T) {
	t.Setenv("AINFRA_IDENTITY", "ci")
	c := NewContextFromEnv("", "", "")
	if c.Identity != "ci" {
		t.Errorf("Identity = %q, want ci", c.Identity)
	}
}

func TestNewContextFromEnvDefault(t *testing.T) {
	t.Setenv("AINFRA_IDENTITY", "")
	c := NewContextFromEnv("", "", "")
	if c.Identity != DefaultIdentity {
		t.Errorf("Identity = %q, want %q", c.Identity, DefaultIdentity)
	}
}

func TestNewContextFromEnvInvocationPath(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "services", "api")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	c := NewContextFromEnv("", sub, root)
	if c.InvocationPath != "services/api" {
		t.Errorf("InvocationPath = %q, want services/api", c.InvocationPath)
	}
	rootCtx := NewContextFromEnv("", root, root)
	if rootCtx.InvocationPath != "." {
		t.Errorf("root InvocationPath = %q, want .", rootCtx.InvocationPath)
	}
}
