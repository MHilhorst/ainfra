package resolve

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/MHilhorst/ainfra/internal/provider"
)

// TestRenderResourcesForFiltersByIdentity confirms that an entry whose
// scope.identities does not include the caller identity is dropped from the
// rendered set. The lockfile still contains the entry — selectors filter at
// render time only — but this invocation neither plans nor applies it.
func TestRenderResourcesForFiltersByIdentity(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "team.md"), []byte("rule"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifestYAML := `version: 1
rules:
  human-rule:
    source: ./team.md
    scope:
      identities: [human]
  ci-rule:
    source: ./team.md
    scope:
      identities: [ci]
  everyone:
    source: ./team.md
`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(manifestYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	humanRes, err := RenderResourcesFor(dir, provider.ExecRunner{}, ResolutionContext{Identity: "human", InvocationPath: "."})
	if err != nil {
		t.Fatalf("RenderResourcesFor human: %v", err)
	}
	humanIDs := idsOf(humanRes["rules"])
	if !containsID(humanIDs, "human-rule") || !containsID(humanIDs, "everyone") {
		t.Errorf("human render missing expected entries: %v", humanIDs)
	}
	if containsID(humanIDs, "ci-rule") {
		t.Errorf("human render must not include ci-only entry: %v", humanIDs)
	}

	ciRes, err := RenderResourcesFor(dir, provider.ExecRunner{}, ResolutionContext{Identity: "ci", InvocationPath: "."})
	if err != nil {
		t.Fatalf("RenderResourcesFor ci: %v", err)
	}
	ciIDs := idsOf(ciRes["rules"])
	if !containsID(ciIDs, "ci-rule") || !containsID(ciIDs, "everyone") {
		t.Errorf("ci render missing expected entries: %v", ciIDs)
	}
	if containsID(ciIDs, "human-rule") {
		t.Errorf("ci render must not include human-only entry: %v", ciIDs)
	}
}

// TestRenderResourcesDefaultIdentityIsHuman confirms that the back-compat
// entry point keeps the historical behaviour: an entry without a scope
// matches, and an entry scoped to "human" matches because the default is
// "human".
func TestRenderResourcesDefaultIdentityIsHuman(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "team.md"), []byte("rule"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifestYAML := `version: 1
rules:
  unscoped:
    source: ./team.md
  human-only:
    source: ./team.md
    scope:
      identities: [human]
`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(manifestYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	resources, err := RenderResources(dir, provider.ExecRunner{})
	if err != nil {
		t.Fatalf("RenderResources: %v", err)
	}
	ids := idsOf(resources["rules"])
	if !containsID(ids, "unscoped") || !containsID(ids, "human-only") {
		t.Errorf("default render missing expected entries: %v", ids)
	}
}

// TestRenderResourcesForPathScope confirms a paths: selector includes the
// entry only when InvocationPath is within (or equal to) one of the paths.
// Path semantics are filter-only in v1 — render targets are unchanged.
func TestRenderResourcesForPathScope(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "team.md"), []byte("rule"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifestYAML := `version: 1
rules:
  api-only:
    source: ./team.md
    scope:
      paths: [services/api]
`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(manifestYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	insideRes, err := RenderResourcesFor(dir, provider.ExecRunner{}, ResolutionContext{Identity: "human", InvocationPath: "services/api"})
	if err != nil {
		t.Fatalf("RenderResourcesFor inside: %v", err)
	}
	if !containsID(idsOf(insideRes["rules"]), "api-only") {
		t.Errorf("inside services/api should include api-only: %v", idsOf(insideRes["rules"]))
	}

	outsideRes, err := RenderResourcesFor(dir, provider.ExecRunner{}, ResolutionContext{Identity: "human", InvocationPath: "."})
	if err != nil {
		t.Fatalf("RenderResourcesFor outside: %v", err)
	}
	if containsID(idsOf(outsideRes["rules"]), "api-only") {
		t.Errorf("at repo root, path-scoped entry must not render: %v", idsOf(outsideRes["rules"]))
	}
}

func idsOf(rs []provider.Resource) []string {
	out := make([]string, 0, len(rs))
	for _, r := range rs {
		out = append(out, r.ID)
	}
	return out
}

func containsID(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}
