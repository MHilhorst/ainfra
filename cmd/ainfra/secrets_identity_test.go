package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MHilhorst/ainfra/internal/lockfile"
	"github.com/MHilhorst/ainfra/internal/resolve"
	"github.com/MHilhorst/ainfra/internal/secret"
)

// A secret can declare scope.identities exactly like any other manifest entry,
// and a caller whose identity is excluded must not even attempt to resolve it.
//
// The motivating case is a headless agent box. Its 1Password service account
// can see the shared vault and, by construction, nobody's Private vault. Before
// this gate, every `ainfra exec` on that box tried the personal blob, failed,
// and printed a resolution warning -- on every cron run, forever. That is worse
// than cosmetic: a warning that fires on every healthy run is indistinguishable
// from one that fires on a broken one, so the real failures stop being read.
func writeIdentityFixture(t *testing.T, dir string) {
	t.Helper()
	yaml := `version: 1
secrets:
  team-env:
    mode: direct
    scope: shared
    ref: "env://TEAM_ENV_BLOB"
    envFile: true
  personal-env:
    mode: direct
    scope: personal
    ref: "env://PERSONAL_ENV_BLOB"
    envFile: true
    identities: [human]
`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestResolveSecretEnvSkipsSecretsScopedToAnotherIdentity(t *testing.T) {
	dir := t.TempDir()
	writeIdentityFixture(t, dir)
	t.Setenv("TEAM_ENV_BLOB", "TEAM_KEY=team-value")
	t.Setenv("PERSONAL_ENV_BLOB", "PERSONAL_KEY=personal-value")

	empty := &lockfile.Lock{}
	ctx := resolve.ResolutionContext{Identity: "agent", InvocationPath: "."}
	resolved, failures := resolveSecretEnvFor(dir, secret.DefaultRegistry(), empty, empty, ctx)

	if resolved["TEAM_KEY"] != "team-value" {
		t.Errorf("shared secret must still resolve for a non-human identity: %v", resolved)
	}
	if _, ok := resolved["PERSONAL_KEY"]; ok {
		t.Errorf("secret scoped to [human] must not resolve for identity agent: %v", resolved)
	}
	if len(failures) != 0 {
		t.Errorf("a skipped secret is not a failure; got %v", failures)
	}
}

func TestResolveSecretEnvResolvesScopedSecretForMatchingIdentity(t *testing.T) {
	dir := t.TempDir()
	writeIdentityFixture(t, dir)
	t.Setenv("TEAM_ENV_BLOB", "TEAM_KEY=team-value")
	t.Setenv("PERSONAL_ENV_BLOB", "PERSONAL_KEY=personal-value")

	empty := &lockfile.Lock{}
	ctx := resolve.ResolutionContext{Identity: "human", InvocationPath: "."}
	resolved, failures := resolveSecretEnvFor(dir, secret.DefaultRegistry(), empty, empty, ctx)

	if resolved["PERSONAL_KEY"] != "personal-value" {
		t.Errorf("secret scoped to [human] must resolve for identity human: %v", resolved)
	}
	if resolved["TEAM_KEY"] != "team-value" {
		t.Errorf("shared secret must resolve too: %v", resolved)
	}
	if len(failures) != 0 {
		t.Errorf("unexpected failures: %v", failures)
	}
}

// An unreachable secret that IS in scope must still be reported. The gate is
// about relevance, not about hiding errors: silencing a secret the caller
// genuinely needs would turn this fix into the bug it replaces.
func TestResolveSecretEnvStillReportsInScopeFailures(t *testing.T) {
	dir := t.TempDir()
	yaml := `version: 1
secrets:
  team-env:
    mode: direct
    scope: shared
    ref: "env://DEFINITELY_NOT_SET_ANYWHERE"
    envFile: true
`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	empty := &lockfile.Lock{}
	ctx := resolve.ResolutionContext{Identity: "agent", InvocationPath: "."}
	_, failures := resolveSecretEnvFor(dir, secret.DefaultRegistry(), empty, empty, ctx)
	if len(failures) == 0 {
		t.Fatal("an in-scope secret that cannot resolve must still be reported")
	}
	if !strings.Contains(strings.Join(failures, " "), "team-env") {
		t.Errorf("failure should name the secret: %v", failures)
	}
}

// Back-compat: a secret with no identities matches every caller, and the
// existing entry point keeps resolving everything for the default identity.
func TestResolveSecretEnvUnscopedSecretMatchesEveryIdentity(t *testing.T) {
	dir := t.TempDir()
	yaml := `version: 1
secrets:
  team-env:
    mode: direct
    scope: shared
    ref: "env://TEAM_ENV_BLOB"
    envFile: true
`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEAM_ENV_BLOB", "TEAM_KEY=team-value")
	empty := &lockfile.Lock{}

	for _, id := range []string{"human", "agent", "ci", ""} {
		ctx := resolve.ResolutionContext{Identity: id, InvocationPath: "."}
		resolved, _ := resolveSecretEnvFor(dir, secret.DefaultRegistry(), empty, empty, ctx)
		if resolved["TEAM_KEY"] != "team-value" {
			t.Errorf("unscoped secret must resolve for identity %q: %v", id, resolved)
		}
	}

	// The original signature must behave exactly as before.
	resolved, _ := resolveSecretEnv(dir, secret.DefaultRegistry(), empty, empty)
	if resolved["TEAM_KEY"] != "team-value" {
		t.Errorf("back-compat entry point regressed: %v", resolved)
	}
}

// The implicit gate: `scope: personal` alone is enough to keep a non-human
// identity from attempting a per-human vault. This is what lets the fix ship
// without a coordinated manifest change -- an older ainfra rejects unknown
// keys, so an explicit-key-only design could not land the manifest edit until
// every machine had upgraded.
func TestPersonalScopeIsSkippedForNonHumanIdentitiesWithoutAnyManifestChange(t *testing.T) {
	dir := t.TempDir()
	// Note: no `identities:` key anywhere. This is today's manifest shape.
	yaml := `version: 1
secrets:
  team-env:
    mode: direct
    scope: shared
    ref: "env://TEAM_ENV_BLOB"
    envFile: true
  personal-env:
    mode: direct
    scope: personal
    ref: "env://DEFINITELY_NOT_SET_ANYWHERE"
    envFile: true
`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEAM_ENV_BLOB", "TEAM_KEY=team-value")
	empty := &lockfile.Lock{}

	// The agent box: shared resolves, personal is not even attempted.
	agent := resolve.ResolutionContext{Identity: "agent", InvocationPath: "."}
	resolved, failures := resolveSecretEnvFor(dir, secret.DefaultRegistry(), empty, empty, agent)
	if resolved["TEAM_KEY"] != "team-value" {
		t.Errorf("shared secret must resolve for the agent: %v", resolved)
	}
	if len(failures) != 0 {
		t.Errorf("agent must not be warned about a per-human vault: %v", failures)
	}

	// A human on a laptop still attempts it, and still hears about it failing.
	human := resolve.ResolutionContext{Identity: "human", InvocationPath: "."}
	_, humanFailures := resolveSecretEnvFor(dir, secret.DefaultRegistry(), empty, empty, human)
	if len(humanFailures) == 0 {
		t.Fatal("a human must still be told their personal secret did not resolve")
	}
	if !strings.Contains(strings.Join(humanFailures, " "), "personal-env") {
		t.Errorf("failure should name the secret: %v", humanFailures)
	}
}

// A shared secret is never gated by scope, whoever asks. Otherwise the fix
// would silently starve the box of the credentials it actually needs.
func TestSharedScopeResolvesForEveryIdentity(t *testing.T) {
	dir := t.TempDir()
	yaml := `version: 1
secrets:
  team-env:
    mode: direct
    scope: shared
    ref: "env://TEAM_ENV_BLOB"
    envFile: true
`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEAM_ENV_BLOB", "TEAM_KEY=team-value")
	empty := &lockfile.Lock{}
	for _, id := range []string{"human", "agent", "ci", "codex", ""} {
		ctx := resolve.ResolutionContext{Identity: id, InvocationPath: "."}
		resolved, _ := resolveSecretEnvFor(dir, secret.DefaultRegistry(), empty, empty, ctx)
		if resolved["TEAM_KEY"] != "team-value" {
			t.Errorf("shared secret must resolve for identity %q: %v", id, resolved)
		}
	}
}

// A secret with no scope at all defaults to shared behaviour, not personal.
func TestUnscopedSecretIsNotTreatedAsPersonal(t *testing.T) {
	dir := t.TempDir()
	yaml := `version: 1
secrets:
  legacy:
    mode: direct
    ref: "env://TEAM_ENV_BLOB"
    envFile: true
`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEAM_ENV_BLOB", "TEAM_KEY=team-value")
	empty := &lockfile.Lock{}
	ctx := resolve.ResolutionContext{Identity: "agent", InvocationPath: "."}
	resolved, _ := resolveSecretEnvFor(dir, secret.DefaultRegistry(), empty, empty, ctx)
	if resolved["TEAM_KEY"] != "team-value" {
		t.Errorf("a secret with no scope must not be gated: %v", resolved)
	}
}
