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

// --- regressions from adversarial review -----------------------------------

// An explicit identities list must WIN over the implicit personal-scope rule.
// Otherwise `identities: [agent]` on a personal secret matches the selector and
// is then dropped anyway: the intent is unexpressible and the caller is silently
// denied a credential it was named for. That is a worse failure than the noise
// this gate removes, so it is pinned.
func TestExplicitIdentityBeatsTheImplicitPersonalRule(t *testing.T) {
	dir := t.TempDir()
	yaml := `version: 1
secrets:
  agent-personal:
    mode: direct
    scope: personal
    ref: "env://AGENT_BLOB"
    envFile: true
    identities: [agent]
`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENT_BLOB", "AGENT_KEY=agent-value")
	empty := &lockfile.Lock{}

	ctx := resolve.ResolutionContext{Identity: "agent", InvocationPath: "."}
	resolved, failures := resolveSecretEnvFor(dir, secret.DefaultRegistry(), empty, empty, ctx)
	if resolved["AGENT_KEY"] != "agent-value" {
		t.Errorf("a personal secret named for [agent] must resolve for the agent: %v", resolved)
	}
	if len(failures) != 0 {
		t.Errorf("unexpected failures: %v", failures)
	}

	// And a caller NOT in the list still does not get it.
	other := resolve.ResolutionContext{Identity: "ci", InvocationPath: "."}
	otherResolved, _ := resolveSecretEnvFor(dir, secret.DefaultRegistry(), empty, empty, other)
	if _, ok := otherResolved["AGENT_KEY"]; ok {
		t.Errorf("identity ci must not receive a secret scoped to [agent]: %v", otherResolved)
	}
}

// `install --agent codex` treats the agent as the identity when rendering
// resources. Secret gating must read identity the same way, or an install
// writes an agent's config while skipping the credentials that config needs --
// launching it broken with no backend failure to explain why.
func TestAgentOverrideActsAsIdentityForSecrets(t *testing.T) {
	dir := t.TempDir()
	yaml := `version: 1
secrets:
  codex-secret:
    mode: direct
    scope: shared
    ref: "env://CODEX_BLOB"
    envFile: true
    identities: [codex]
  human-personal:
    mode: direct
    scope: personal
    ref: "env://DEFINITELY_NOT_SET_ANYWHERE"
    envFile: true
`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_BLOB", "CODEX_KEY=codex-value")
	empty := &lockfile.Lock{}

	// No explicit identity, agent override only: exactly what `--agent codex` builds.
	ctx := resolve.ResolutionContext{Agent: "codex", InvocationPath: "."}
	if got := effectiveIdentity(ctx); got != "codex" {
		t.Fatalf("an --agent override must stand in for identity, got %q", got)
	}
	// Assert on `resolved`, not only on `failures`. A silently-skipped secret
	// produces zero failures, so a failures-only assertion passes while the
	// credential the install needs is missing -- which is exactly how the gap
	// this test was written to catch shipped anyway.
	resolved, _ := resolveSecretEnvFor(dir, secret.DefaultRegistry(), empty, empty, ctx)
	if resolved["CODEX_KEY"] != "codex-value" {
		t.Errorf("a secret scoped identities:[codex] must resolve under --agent codex: %v", resolved)
	}
	// The human-personal secret in this fixture IS still attempted here, and
	// that is correct: this is a human on their own laptop writing config for a
	// different tool, not a different principal. See
	// TestAgentOverrideDoesNotStripAHumansOwnPersonalSecrets.
}

// An explicit identity still beats an --agent override, matching the renderer.
func TestExplicitIdentityWinsOverAgentOverride(t *testing.T) {
	ctx := resolve.ResolutionContext{Identity: "ci", Agent: "codex", InvocationPath: "."}
	if got := effectiveIdentity(ctx); got != "ci" {
		t.Errorf("explicit identity must win over --agent, got %q", got)
	}
	if got := effectiveIdentity(resolve.ResolutionContext{}); got != resolve.DefaultIdentity {
		t.Errorf("empty context must default to human, got %q", got)
	}
	if got := effectiveIdentity(resolve.ResolutionContext{Identity: "human", Agent: "codex"}); got != "codex" {
		t.Errorf("an explicit default identity should not mask --agent, got %q", got)
	}
}

// Lockfile-backed secrets carry scope too, and MCP env/header bindings resolve
// through them. Leaving that path ungated let the exact warning this change
// removes survive by another route.
func TestLockfileBackedPersonalSecretsAreGatedToo(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte("version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	locks := &lockfile.Lock{Secrets: map[string]lockfile.SecretRef{
		"AINFRA_SECRET_SHARED": {
			Var: "AINFRA_SECRET_SHARED", Ref: "env://SHARED_ONE", Scheme: "env", Scope: "shared",
		},
		"AINFRA_SECRET_PERSONAL": {
			Var: "AINFRA_SECRET_PERSONAL", Ref: "env://DEFINITELY_NOT_SET_ANYWHERE", Scheme: "env", Scope: "personal",
		},
	}}
	t.Setenv("SHARED_ONE", "shared-value")
	empty := &lockfile.Lock{}

	agent := resolve.ResolutionContext{Identity: "agent", InvocationPath: "."}
	resolved, failures := resolveSecretEnvFor(dir, secret.DefaultRegistry(), locks, empty, agent)
	if resolved["AINFRA_SECRET_SHARED"] != "shared-value" {
		t.Errorf("a shared lockfile secret must still resolve: %v", resolved)
	}
	if len(failures) != 0 {
		t.Errorf("agent must not be warned about a personal lockfile secret: %v", failures)
	}

	// The human still attempts it and still hears about the failure.
	human := resolve.ResolutionContext{Identity: "human", InvocationPath: "."}
	_, humanFailures := resolveSecretEnvFor(dir, secret.DefaultRegistry(), locks, empty, human)
	if len(humanFailures) == 0 {
		t.Error("a human must still be told their personal lockfile secret failed")
	}

	// And the backend preflight must not demand a backend only the skipped
	// secret needs.
	if got := secretSchemesUsedFor(dir, locks, empty, agent); len(got) != 1 || got[0] != "env" {
		t.Errorf("preflight schemes for the agent: %v", got)
	}
}

// --- second review round ----------------------------------------------------

// `--agent codex` on a laptop is the same human, the same machine and the same
// 1Password session -- just writing config for a different tool. It must not be
// read as a different principal, or that human's own personal secrets vanish
// from their Codex install: the exact failure this whole change exists to stop,
// reintroduced one flag away.
func TestAgentOverrideDoesNotStripAHumansOwnPersonalSecrets(t *testing.T) {
	dir := t.TempDir()
	yaml := `version: 1
secrets:
  my-personal:
    mode: direct
    scope: personal
    ref: "env://MY_BLOB"
    envFile: true
`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MY_BLOB", "MY_KEY=my-value")
	empty := &lockfile.Lock{}

	// Exactly what `ainfra install --agent codex` builds on a laptop.
	ctx := resolve.NewContextFromEnv("", dir, dir)
	ctx.Agent = "codex"
	resolved, failures := resolveSecretEnvFor(dir, secret.DefaultRegistry(), empty, empty, ctx)
	if resolved["MY_KEY"] != "my-value" {
		t.Errorf("a human's own personal secret must survive --agent: %v (failures %v)", resolved, failures)
	}

	// A genuine agent caller is still gated, --agent or not.
	agentCtx := resolve.ResolutionContext{Identity: "agent", Agent: "codex", InvocationPath: "."}
	agentResolved, _ := resolveSecretEnvFor(dir, secret.DefaultRegistry(), empty, empty, agentCtx)
	if _, ok := agentResolved["MY_KEY"]; ok {
		t.Errorf("a service account must not reach a per-human vault: %v", agentResolved)
	}
}

// The two gates answer different questions and must read identity differently.
// Pinned explicitly so a future "simplification" that unifies them fails here
// with a reason rather than silently losing credentials.
func TestSelectorAndPersonalRuleReadIdentityDifferently(t *testing.T) {
	laptopCodex := resolve.ResolutionContext{Identity: resolve.DefaultIdentity, Agent: "codex"}

	// Selector: --agent stands in, so `identities: [codex]` is reachable.
	if got := effectiveIdentity(laptopCodex); got != "codex" {
		t.Errorf("selector identity under --agent codex: want codex, got %q", got)
	}
	// Personal-vault rule: --agent does NOT stand in, so the human keeps theirs.
	if personalAndNotHuman("personal", laptopCodex) {
		t.Error("--agent must not make a human unable to reach their own vault")
	}
	// A real agent identity is gated regardless.
	if !personalAndNotHuman("personal", resolve.ResolutionContext{Identity: "agent"}) {
		t.Error("a service account must be gated from a per-human vault")
	}
	// Shared is never gated.
	if personalAndNotHuman("shared", resolve.ResolutionContext{Identity: "agent"}) {
		t.Error("a shared secret must never be gated by scope")
	}
}
