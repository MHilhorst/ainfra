# Secret Resolver Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the pluggable secret resolver deferred from Phase 3 — resolve `op://` and `env://` references at session time and deliver values to Claude Code without writing any secret to disk.

**Architecture:** A new `internal/secret/` package holds a `Resolver` interface, a scheme registry, and the `op`/`env`/stub adapters. `ainfra lock` records each ref-mode secret as a placeholder (`${AINFRA_SECRET_*}`) rendered into `.mcp.json` plus a reference entry in the lockfile. A new `ainfra exec` command reads those entries, resolves each ref in-memory, and `syscall.Exec`s the target command with the values in its environment.

**Tech Stack:** Go (standard library + `gopkg.in/yaml.v3`), the project's hand-rolled `internal/cli` command framework.

**Spec:** `docs/superpowers/specs/2026-05-21-secret-resolver-design.md`

---

## File Structure

- Create: `internal/secret/registry.go` — `Resolver` interface, `Registry`, `SchemeOf`.
- Create: `internal/secret/env.go` — `EnvResolver`.
- Create: `internal/secret/op.go` — `OpResolver`, `Runner` seam, `ExecRunner`.
- Create: `internal/secret/stub.go` — `StubResolver`, `DefaultRegistry`.
- Create: `internal/secret/placeholder.go` — placeholder var/token derivation.
- Create: `internal/secret/*_test.go` — package tests.
- Modify: `internal/lockfile/types.go` — add `SecretRef` type and `Lock.Secrets`.
- Create: `internal/resolve/secrets.go` — `normalizeSecret`, `collectSecretRefs`, `substituteSecrets`.
- Modify: `internal/resolve/pipeline.go` — wire secrets into `RunLock` and `splitByLayer`.
- Modify: `internal/resolve/render.go` — wire secrets into `RenderResources`.
- Create: `cmd/ainfra/cmd_exec.go` — the `exec` command.
- Modify: `cmd/ainfra/main.go` — register `exec`.
- Modify: `cmd/ainfra/commands.go` — `check` calls the resolver registry.

---

## Task 1: Resolver interface and Registry

**Files:**
- Create: `internal/secret/registry.go`
- Test: `internal/secret/registry_test.go`

- [ ] **Step 1: Write the failing test**

```go
package secret

import (
	"strings"
	"testing"
)

// fakeResolver is a Resolver returning a fixed value, for tests.
type fakeResolver struct{ scheme, value string }

func (f fakeResolver) Scheme() string             { return f.scheme }
func (f fakeResolver) Resolve(string) (string, error) { return f.value, nil }
func (f fakeResolver) Check(string) error         { return nil }

func TestRegistryResolveDispatchesByScheme(t *testing.T) {
	reg := NewRegistry()
	reg.Add(fakeResolver{scheme: "op", value: "secret-value"})

	got, err := reg.Resolve("op://Vault/item/field")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "secret-value" {
		t.Errorf("Resolve = %q, want %q", got, "secret-value")
	}
}

func TestRegistryUnknownSchemeErrors(t *testing.T) {
	reg := NewRegistry()
	reg.Add(fakeResolver{scheme: "op"})

	_, err := reg.Resolve("vault://path/key")
	if err == nil {
		t.Fatal("Resolve with unknown scheme: want error, got nil")
	}
	if !strings.Contains(err.Error(), "vault") || !strings.Contains(err.Error(), "op") {
		t.Errorf("error %q should name the bad scheme and the registered schemes", err)
	}
}

func TestSchemeOf(t *testing.T) {
	got, err := SchemeOf("op://Vault/item")
	if err != nil || got != "op" {
		t.Fatalf("SchemeOf = %q, %v; want \"op\", nil", got, err)
	}
	if _, err := SchemeOf("no-scheme"); err == nil {
		t.Error("SchemeOf(\"no-scheme\"): want error, got nil")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/secret/ -run TestRegistry -v`
Expected: FAIL — `undefined: NewRegistry`.

- [ ] **Step 3: Write minimal implementation**

```go
// Package secret resolves manifest secret references (op://, env://, ...) to
// values at session time. It never stores, caches, or writes a resolved value.
package secret

import (
	"fmt"
	"sort"
	"strings"
)

// Resolver turns one ref scheme into a credential value.
type Resolver interface {
	// Scheme is the URI scheme this resolver handles, e.g. "op", "env".
	Scheme() string
	// Resolve returns the secret value for ref. The value is held in memory
	// and never logged. ref is the full URI including its scheme.
	Resolve(ref string) (string, error)
	// Check verifies ref is resolvable without returning or exposing the value.
	Check(ref string) error
}

// Registry dispatches a ref to the Resolver registered for its scheme.
type Registry struct {
	resolvers map[string]Resolver
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{resolvers: map[string]Resolver{}}
}

// Add registers r under its scheme, replacing any prior resolver.
func (reg *Registry) Add(r Resolver) { reg.resolvers[r.Scheme()] = r }

// SchemeOf returns the scheme of a "scheme://rest" reference.
func SchemeOf(ref string) (string, error) {
	i := strings.Index(ref, "://")
	if i <= 0 {
		return "", fmt.Errorf("secret ref %q has no scheme", ref)
	}
	return ref[:i], nil
}

func (reg *Registry) schemes() string {
	out := make([]string, 0, len(reg.resolvers))
	for s := range reg.resolvers {
		out = append(out, s)
	}
	sort.Strings(out)
	return strings.Join(out, ", ")
}

func (reg *Registry) resolverFor(ref string) (Resolver, error) {
	scheme, err := SchemeOf(ref)
	if err != nil {
		return nil, err
	}
	r, ok := reg.resolvers[scheme]
	if !ok {
		return nil, fmt.Errorf("secret ref %q: unknown scheme %q (registered: %s)", ref, scheme, reg.schemes())
	}
	return r, nil
}

// Resolve dispatches ref to its scheme's resolver.
func (reg *Registry) Resolve(ref string) (string, error) {
	r, err := reg.resolverFor(ref)
	if err != nil {
		return "", err
	}
	return r.Resolve(ref)
}

// Check dispatches ref to its scheme's resolver for verification.
func (reg *Registry) Check(ref string) error {
	r, err := reg.resolverFor(ref)
	if err != nil {
		return err
	}
	return r.Check(ref)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/secret/ -run 'TestRegistry|TestSchemeOf' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/secret/registry.go internal/secret/registry_test.go
git commit -m "Add secret Resolver interface and scheme registry"
```

---

## Task 2: env:// resolver

**Files:**
- Create: `internal/secret/env.go`
- Test: `internal/secret/env_test.go`

- [ ] **Step 1: Write the failing test**

```go
package secret

import "testing"

func TestEnvResolverReadsVariable(t *testing.T) {
	t.Setenv("AINFRA_TEST_TOKEN", "tok-123")

	got, err := EnvResolver{}.Resolve("env://AINFRA_TEST_TOKEN")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "tok-123" {
		t.Errorf("Resolve = %q, want %q", got, "tok-123")
	}
}

func TestEnvResolverUnsetVariableErrors(t *testing.T) {
	err := EnvResolver{}.Check("env://AINFRA_DEFINITELY_UNSET")
	if err == nil {
		t.Fatal("Check of unset variable: want error, got nil")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/secret/ -run TestEnvResolver -v`
Expected: FAIL — `undefined: EnvResolver`.

- [ ] **Step 3: Write minimal implementation**

```go
package secret

import (
	"fmt"
	"os"
	"strings"
)

// EnvResolver resolves env://VARNAME refs from the process environment. It is
// the always-works fallback for a developer who injects secrets themselves.
type EnvResolver struct{}

// Scheme returns "env".
func (EnvResolver) Scheme() string { return "env" }

// Resolve returns the value of the named environment variable.
func (EnvResolver) Resolve(ref string) (string, error) {
	name := strings.TrimPrefix(ref, "env://")
	if name == "" {
		return "", fmt.Errorf("env ref %q has no variable name", ref)
	}
	v, ok := os.LookupEnv(name)
	if !ok || v == "" {
		return "", fmt.Errorf("env ref %q: variable %s is not set", ref, name)
	}
	return v, nil
}

// Check verifies the variable is set without returning its value.
func (e EnvResolver) Check(ref string) error {
	_, err := e.Resolve(ref)
	return err
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/secret/ -run TestEnvResolver -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/secret/env.go internal/secret/env_test.go
git commit -m "Add env:// secret resolver"
```

---

## Task 3: op:// resolver with command-runner seam

**Files:**
- Create: `internal/secret/op.go`
- Test: `internal/secret/op_test.go`

- [ ] **Step 1: Write the failing test**

```go
package secret

import (
	"fmt"
	"strings"
	"testing"
)

// stubRunner is a Runner returning a canned result, for tests.
type stubRunner struct {
	stdout string
	err    error
	gotCmd []string
}

func (s *stubRunner) Run(name string, args ...string) (string, error) {
	s.gotCmd = append([]string{name}, args...)
	return s.stdout, s.err
}

func TestOpResolverResolvesViaCLI(t *testing.T) {
	runner := &stubRunner{stdout: "hunter2"}
	got, err := OpResolver{Runner: runner}.Resolve("op://Engineering/db/password")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "hunter2" {
		t.Errorf("Resolve = %q, want %q", got, "hunter2")
	}
	want := []string{"op", "read", "op://Engineering/db/password"}
	if strings.Join(runner.gotCmd, " ") != strings.Join(want, " ") {
		t.Errorf("ran %v, want %v", runner.gotCmd, want)
	}
}

func TestOpResolverNotSignedInGivesActionableError(t *testing.T) {
	runner := &stubRunner{err: fmt.Errorf("[ERROR] you are not currently signed in")}
	err := OpResolver{Runner: runner}.Check("op://Engineering/db/password")
	if err == nil || !strings.Contains(err.Error(), "op signin") {
		t.Errorf("error = %v, want it to mention `op signin`", err)
	}
}

func TestOpResolverNotInstalledGivesActionableError(t *testing.T) {
	runner := &stubRunner{err: fmt.Errorf(`exec: "op": executable file not found in $PATH`)}
	_, err := OpResolver{Runner: runner}.Resolve("op://Engineering/db/password")
	if err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Errorf("error = %v, want it to mention the CLI is not installed", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/secret/ -run TestOpResolver -v`
Expected: FAIL — `undefined: OpResolver`.

- [ ] **Step 3: Write minimal implementation**

```go
package secret

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Runner runs an external command and returns its trimmed stdout. It abstracts
// os/exec so tests can substitute a fake op binary.
type Runner interface {
	Run(name string, args ...string) (stdout string, err error)
}

// ExecRunner is the production Runner backed by os/exec.
type ExecRunner struct{}

// Run executes name with args and returns trimmed stdout. On a non-zero exit
// it returns the command's trimmed stderr as the error message.
func (ExecRunner) Run(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			return "", fmt.Errorf("%s", strings.TrimSpace(string(ee.Stderr)))
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// OpResolver resolves op://... references via the 1Password CLI (`op read`).
type OpResolver struct {
	Runner Runner
}

// Scheme returns "op".
func (OpResolver) Scheme() string { return "op" }

// Resolve returns the secret value for ref via `op read`.
func (o OpResolver) Resolve(ref string) (string, error) {
	val, err := o.Runner.Run("op", "read", ref)
	if err != nil {
		return "", opError(ref, err)
	}
	return val, nil
}

// Check verifies ref resolves without exposing the value.
func (o OpResolver) Check(ref string) error {
	if _, err := o.Runner.Run("op", "read", ref); err != nil {
		return opError(ref, err)
	}
	return nil
}

// opError maps a raw `op` CLI failure to an actionable message. It never
// includes a secret value.
func opError(ref string, err error) error {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "executable file not found"):
		return fmt.Errorf("secret %q: the 1Password CLI is not installed — see https://developer.1password.com/docs/cli/get-started/", ref)
	case strings.Contains(msg, "not currently signed in"), strings.Contains(msg, "no active session"):
		return fmt.Errorf("secret %q: not signed in to 1Password — run: op signin", ref)
	default:
		return fmt.Errorf("secret %q: %s", ref, msg)
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/secret/ -run TestOpResolver -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/secret/op.go internal/secret/op_test.go
git commit -m "Add op:// secret resolver with command-runner seam"
```

---

## Task 4: Stub resolver and DefaultRegistry

**Files:**
- Create: `internal/secret/stub.go`
- Test: `internal/secret/stub_test.go`

- [ ] **Step 1: Write the failing test**

```go
package secret

import (
	"strings"
	"testing"
)

func TestStubResolverFailsClearly(t *testing.T) {
	_, err := StubResolver{SchemeName: "vault"}.Resolve("vault://path/key")
	if err == nil || !strings.Contains(err.Error(), "not implemented") {
		t.Errorf("error = %v, want it to say vault:// is not implemented", err)
	}
}

func TestDefaultRegistryHasAllSchemes(t *testing.T) {
	reg := DefaultRegistry()
	for _, ref := range []string{
		"env://X", "op://V/i/f", "doppler://p/c", "vault://s/k", "sops://f#k",
	} {
		if _, err := reg.resolverFor(ref); err != nil {
			t.Errorf("DefaultRegistry has no resolver for %q: %v", ref, err)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/secret/ -run 'TestStub|TestDefaultRegistry' -v`
Expected: FAIL — `undefined: StubResolver`.

- [ ] **Step 3: Write minimal implementation**

```go
package secret

import "fmt"

// StubResolver registers a scheme that this increment does not implement, so
// a manifest using it stays well-formed but fails loudly at resolve time.
type StubResolver struct{ SchemeName string }

// Scheme returns the stubbed scheme name.
func (s StubResolver) Scheme() string { return s.SchemeName }

// Resolve always fails with a "not implemented" error.
func (s StubResolver) Resolve(ref string) (string, error) { return "", s.err(ref) }

// Check always fails with a "not implemented" error.
func (s StubResolver) Check(ref string) error { return s.err(ref) }

func (s StubResolver) err(ref string) error {
	return fmt.Errorf("secret %q: %s:// is not implemented in this increment (only op:// and env:// are supported)", ref, s.SchemeName)
}

// DefaultRegistry returns a Registry with every production resolver registered.
func DefaultRegistry() *Registry {
	reg := NewRegistry()
	reg.Add(EnvResolver{})
	reg.Add(OpResolver{Runner: ExecRunner{}})
	reg.Add(StubResolver{SchemeName: "doppler"})
	reg.Add(StubResolver{SchemeName: "vault"})
	reg.Add(StubResolver{SchemeName: "sops"})
	return reg
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/secret/ -run 'TestStub|TestDefaultRegistry' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/secret/stub.go internal/secret/stub_test.go
git commit -m "Add stub resolvers and DefaultRegistry"
```

---

## Task 5: Placeholder derivation

**Files:**
- Create: `internal/secret/placeholder.go`
- Test: `internal/secret/placeholder_test.go`

- [ ] **Step 1: Write the failing test**

```go
package secret

import "testing"

func TestPlaceholderVarIsDeterministicAndSanitized(t *testing.T) {
	got := PlaceholderVar("mcpServers", "linear-mcp", "token")
	want := "AINFRA_SECRET_MCPSERVERS_LINEAR_MCP_TOKEN"
	if got != want {
		t.Errorf("PlaceholderVar = %q, want %q", got, want)
	}
}

func TestPlaceholderWrapsVarInBraces(t *testing.T) {
	got := Placeholder("cliTools", "aws-cli", "ssoToken")
	want := "${AINFRA_SECRET_CLITOOLS_AWS_CLI_SSOTOKEN}"
	if got != want {
		t.Errorf("Placeholder = %q, want %q", got, want)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/secret/ -run TestPlaceholder -v`
Expected: FAIL — `undefined: PlaceholderVar`.

- [ ] **Step 3: Write minimal implementation**

```go
package secret

import "strings"

// PlaceholderVar returns the environment variable name ainfra renders into
// native config for a secret. channel is the owning channel ("mcpServers",
// "cliTools"), owner is the entry id, name is the secret's logical name. The
// derivation is deterministic so content hashes stay stable across runs.
func PlaceholderVar(channel, owner, name string) string {
	return "AINFRA_SECRET_" + sanitize(channel) + "_" + sanitize(owner) + "_" + sanitize(name)
}

// Placeholder returns the ${VAR} token rendered into config files.
func Placeholder(channel, owner, name string) string {
	return "${" + PlaceholderVar(channel, owner, name) + "}"
}

// sanitize uppercases s and replaces every non-alphanumeric rune with "_".
func sanitize(s string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(s) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	return b.String()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/secret/ -v`
Expected: PASS (all `internal/secret` tests).

- [ ] **Step 5: Commit**

```bash
git add internal/secret/placeholder.go internal/secret/placeholder_test.go
git commit -m "Add deterministic secret placeholder derivation"
```

---

## Task 6: Lockfile SecretRef type

**Files:**
- Modify: `internal/lockfile/types.go`
- Test: `internal/lockfile/secret_test.go`

- [ ] **Step 1: Write the failing test**

```go
package lockfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSecretRefRoundTrips(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ainfra.lock")
	in := &Lock{
		Version: 1,
		Secrets: map[string]SecretRef{
			"AINFRA_SECRET_MCPSERVERS_LINEAR_TOKEN": {
				Var:    "AINFRA_SECRET_MCPSERVERS_LINEAR_TOKEN",
				Ref:    "op://Engineering/linear/mcp",
				Scheme: "op",
				Scope:  "shared",
				Layer:  "repo",
			},
		},
	}
	if err := Write(path, in); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("lock not written: %v", err)
	}
	out, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	got, ok := out.Secrets["AINFRA_SECRET_MCPSERVERS_LINEAR_TOKEN"]
	if !ok {
		t.Fatal("Secrets entry missing after round-trip")
	}
	if got.Ref != "op://Engineering/linear/mcp" || got.Scheme != "op" || got.Layer != "repo" {
		t.Errorf("round-tripped SecretRef = %+v", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/lockfile/ -run TestSecretRef -v`
Expected: FAIL — `out.Secrets undefined` / `undefined: SecretRef`.

- [ ] **Step 3: Write minimal implementation**

In `internal/lockfile/types.go`, add the `Secrets` field to `Lock`:

```go
// Lock is one ainfra.lock file (spec Phase 2).
type Lock struct {
	Version      int                  `yaml:"version"`
	GeneratedAt  string               `yaml:"generatedAt"`
	ManifestHash string               `yaml:"manifestHash,omitempty"`
	Entries      Entries              `yaml:"entries"`
	Secrets      map[string]SecretRef `yaml:"secrets,omitempty"`
}
```

Then add the `SecretRef` type at the end of the file:

```go
// SecretRef is a resolved secret placeholder recorded in the lockfile. It
// holds a reference only — never a value. ainfra exec resolves Ref at session
// time and exports Var into the child environment.
type SecretRef struct {
	Var    string `yaml:"var"`    // the AINFRA_SECRET_* environment variable name
	Ref    string `yaml:"ref"`    // the secret reference, e.g. op://Vault/item/field
	Scheme string `yaml:"scheme"` // op, env, ...
	Scope  string `yaml:"scope"`  // shared | personal
	Layer  string `yaml:"layer"`  // team | repo | personal
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/lockfile/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/lockfile/types.go internal/lockfile/secret_test.go
git commit -m "Add SecretRef type to the lockfile"
```

---

## Task 7: Secret binding logic in the resolve package

**Files:**
- Create: `internal/resolve/secrets.go`
- Test: `internal/resolve/secrets_test.go`

This task adds three functions: `normalizeSecret` (turn one `secret:` map value into a `manifest.Secret`), `collectSecretRefs` (turn an entry's whole `secret:` map into lockfile refs plus interpolation values), and `substituteSecrets` (replace secret tokens in an MCP server's env/headers/url).

- [ ] **Step 1: Write the failing test**

```go
package resolve

import (
	"testing"

	"github.com/MHilhorst/ainfra/internal/manifest"
)

func TestCollectSecretRefsHandlesRefAndLiteral(t *testing.T) {
	raw := map[string]any{
		"token":  map[string]any{"mode": "direct", "ref": "op://Eng/linear/mcp"},
		"region": map[string]any{"mode": "direct", "value": "eu-west-1"},
	}
	refs, vals, err := collectSecretRefs("mcpServers", "linear", manifest.LayerRepo, raw, nil)
	if err != nil {
		t.Fatalf("collectSecretRefs: %v", err)
	}
	wantVar := "AINFRA_SECRET_MCPSERVERS_LINEAR_TOKEN"
	if vals["token"] != "${"+wantVar+"}" {
		t.Errorf("vals[token] = %q, want the placeholder", vals["token"])
	}
	if vals["region"] != "eu-west-1" {
		t.Errorf("vals[region] = %q, want the literal value", vals["region"])
	}
	sr, ok := refs[wantVar]
	if !ok {
		t.Fatalf("refs missing %q", wantVar)
	}
	if sr.Ref != "op://Eng/linear/mcp" || sr.Scheme != "op" || sr.Layer != "repo" || sr.Scope != "shared" {
		t.Errorf("SecretRef = %+v", sr)
	}
	if _, ok := refs["AINFRA_SECRET_MCPSERVERS_LINEAR_REGION"]; ok {
		t.Error("a literal-value secret must not produce a SecretRef")
	}
}

func TestCollectSecretRefsResolvesTopLevelByID(t *testing.T) {
	top := map[string]manifest.Secret{
		"bastion": {Mode: "direct", Ref: "op://Eng/bastion/key", Scope: "personal"},
	}
	raw := map[string]any{"key": "bastion"}
	refs, vals, err := collectSecretRefs("mcpServers", "db", manifest.LayerTeam, raw, top)
	if err != nil {
		t.Fatalf("collectSecretRefs: %v", err)
	}
	v := "AINFRA_SECRET_MCPSERVERS_DB_KEY"
	if vals["key"] != "${"+v+"}" || refs[v].Scope != "personal" {
		t.Errorf("refs=%+v vals=%+v", refs, vals)
	}
}

func TestSubstituteSecretsReplacesTokensInHeaders(t *testing.T) {
	srv := &manifest.MCPServer{
		Headers: map[string]string{"Authorization": "Bearer ${secret.token}"},
	}
	raw := map[string]any{
		"token": map[string]any{"mode": "direct", "ref": "op://Eng/linear/mcp"},
	}
	refs, err := substituteSecrets(srv, "mcpServers", "linear", manifest.LayerRepo, raw, nil)
	if err != nil {
		t.Fatalf("substituteSecrets: %v", err)
	}
	want := "Bearer ${AINFRA_SECRET_MCPSERVERS_LINEAR_TOKEN}"
	if srv.Headers["Authorization"] != want {
		t.Errorf("Authorization = %q, want %q", srv.Headers["Authorization"], want)
	}
	if len(refs) != 1 {
		t.Errorf("got %d refs, want 1", len(refs))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/resolve/ -run 'TestCollectSecretRefs|TestSubstituteSecrets' -v`
Expected: FAIL — `undefined: collectSecretRefs`.

- [ ] **Step 3: Write minimal implementation**

```go
package resolve

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/MHilhorst/ainfra/internal/lockfile"
	"github.com/MHilhorst/ainfra/internal/manifest"
	"github.com/MHilhorst/ainfra/internal/secret"
	"gopkg.in/yaml.v3"
)

// normalizeSecret turns one secret: map value into a manifest.Secret. A string
// value is a reference to a top-level secrets: entry by id; a map value is an
// inline secret definition.
func normalizeSecret(v any, topLevel map[string]manifest.Secret) (manifest.Secret, error) {
	switch t := v.(type) {
	case string:
		s, ok := topLevel[t]
		if !ok {
			return manifest.Secret{}, fmt.Errorf("references unknown top-level secret %q", t)
		}
		return s, nil
	case map[string]any:
		raw, err := yaml.Marshal(t)
		if err != nil {
			return manifest.Secret{}, err
		}
		var s manifest.Secret
		if err := yaml.Unmarshal(raw, &s); err != nil {
			return manifest.Secret{}, err
		}
		return s, nil
	default:
		return manifest.Secret{}, fmt.Errorf("must be a string id or an inline map, got %T", v)
	}
}

// collectSecretRefs normalizes an entry's secret: map and returns, keyed by
// placeholder var, the lockfile SecretRefs for every ref-mode secret, plus a
// map from each secret name to the string that should replace ${secret.<name>}
// during interpolation (a literal value, or an ${AINFRA_SECRET_*} placeholder).
func collectSecretRefs(channel, owner string, layer manifest.Layer, raw map[string]any, topLevel map[string]manifest.Secret) (map[string]lockfile.SecretRef, map[string]string, error) {
	refs := map[string]lockfile.SecretRef{}
	vals := map[string]string{}
	for _, name := range slices.Sorted(maps.Keys(raw)) {
		sec, err := normalizeSecret(raw[name], topLevel)
		if err != nil {
			return nil, nil, fmt.Errorf("%s %q: secret %q: %w", channel, owner, name, err)
		}
		mode := sec.Mode
		if mode == "" {
			mode = "direct"
		}
		switch {
		case mode == "direct" && sec.Ref == "":
			vals[name] = sec.Value
		case mode == "direct" && sec.Ref != "":
			scheme, err := secret.SchemeOf(sec.Ref)
			if err != nil {
				return nil, nil, fmt.Errorf("%s %q: secret %q: %w", channel, owner, name, err)
			}
			scope := sec.Scope
			if scope == "" {
				scope = "shared"
			}
			v := secret.PlaceholderVar(channel, owner, name)
			refs[v] = lockfile.SecretRef{Var: v, Ref: sec.Ref, Scheme: scheme, Scope: scope, Layer: string(layer)}
			vals[name] = "${" + v + "}"
		default: // brokered: no per-dev value exists in this increment
			vals[name] = ""
		}
	}
	return refs, vals, nil
}

// substituteSecrets replaces secret tokens in srv's env, headers, and url with
// their final value: a literal, or an ${AINFRA_SECRET_*} placeholder. It
// recognises both the raw ${secret.<name>} form used by inline servers and the
// <secret:<owner>.<name>> interim form emitted by Instantiate for templated
// servers. It returns the lockfile SecretRefs for every ref-mode secret.
func substituteSecrets(srv *manifest.MCPServer, channel, owner string, layer manifest.Layer, raw map[string]any, topLevel map[string]manifest.Secret) (map[string]lockfile.SecretRef, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	refs, vals, err := collectSecretRefs(channel, owner, layer, raw, topLevel)
	if err != nil {
		return nil, err
	}
	replace := func(s string) string {
		for name, val := range vals {
			s = strings.ReplaceAll(s, "${secret."+name+"}", val)
			s = strings.ReplaceAll(s, fmt.Sprintf("<secret:%s.%s>", owner, name), val)
		}
		return s
	}
	for k, v := range srv.Env {
		srv.Env[k] = replace(v)
	}
	for k, v := range srv.Headers {
		srv.Headers[k] = replace(v)
	}
	srv.URL = replace(srv.URL)
	return refs, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/resolve/ -run 'TestCollectSecretRefs|TestSubstituteSecrets' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/resolve/secrets.go internal/resolve/secrets_test.go
git commit -m "Add secret binding and substitution logic to resolve"
```

---

## Task 8: Wire secrets into RunLock, RenderResources, and splitByLayer

**Files:**
- Modify: `internal/resolve/pipeline.go`
- Modify: `internal/resolve/render.go`
- Test: `internal/resolve/secret_pipeline_test.go`

- [ ] **Step 1: Write the failing test**

```go
package resolve

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MHilhorst/ainfra/internal/lockfile"
)

const secretManifest = `version: 1
mcpServers:
  linear:
    transport: http
    url: https://mcp.linear.app/sse
    headers:
      Authorization: "Bearer ${secret.token}"
    secret:
      token:
        mode: direct
        ref: "op://Engineering/linear/mcp"
`

func writeSecretManifest(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(secretManifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	return dir
}

func TestRunLockRecordsSecretRefs(t *testing.T) {
	dir := writeSecretManifest(t)
	if err := RunLock(dir); err != nil {
		t.Fatalf("RunLock: %v", err)
	}
	lock, err := lockfile.Read(filepath.Join(dir, "ainfra.lock"))
	if err != nil {
		t.Fatalf("Read lock: %v", err)
	}
	sr, ok := lock.Secrets["AINFRA_SECRET_MCPSERVERS_LINEAR_TOKEN"]
	if !ok {
		t.Fatalf("lock.Secrets = %+v, want the linear token", lock.Secrets)
	}
	if sr.Ref != "op://Engineering/linear/mcp" {
		t.Errorf("SecretRef.Ref = %q", sr.Ref)
	}
}

func TestRenderResourcesRendersPlaceholderIntoHeaders(t *testing.T) {
	dir := writeSecretManifest(t)
	res, err := RenderResources(dir)
	if err != nil {
		t.Fatalf("RenderResources: %v", err)
	}
	var headers map[string]string
	for _, r := range res["mcpServers"] {
		if r.ID == "linear" {
			headers, _ = r.Payload["headers"].(map[string]string)
		}
	}
	got := headers["Authorization"]
	if !strings.Contains(got, "${AINFRA_SECRET_MCPSERVERS_LINEAR_TOKEN}") {
		t.Errorf("Authorization header = %q, want the placeholder", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/resolve/ -run 'TestRunLockRecordsSecretRefs|TestRenderResourcesRendersPlaceholder' -v`
Expected: FAIL — `lock.Secrets` is empty / header still contains `${secret.token}`.

- [ ] **Step 3: Write the implementation**

In `internal/resolve/pipeline.go`, function `RunLock`:

(a) After the `allTemplates` block, add a merged top-level secrets map:

```go
	// Merge top-level secrets: across layers, same precedence as templates.
	allSecrets := map[string]manifest.Secret{}
	for _, layerName := range []manifest.Layer{manifest.LayerTeam, manifest.LayerRepo, manifest.LayerPersonal} {
		if m, ok := layers[layerName]; ok {
			for name, s := range m.Secrets {
				if _, exists := allSecrets[name]; !exists {
					allSecrets[name] = s
				}
			}
		}
	}
```

(b) In the initial `lock := &lockfile.Lock{...}` literal, add a `Secrets` field:

```go
	lock := &lockfile.Lock{Version: 1, GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Secrets: map[string]lockfile.SecretRef{},
		Entries: lockfile.Entries{
```

(c) In the templated-server loop, immediately after `out, err := Instantiate(...)` succeeds and before the `entry := lockfile.Entry{...}` line, substitute secrets on the produced server:

```go
		if out.MCPServer != nil {
			refs, err := substituteSecrets(out.MCPServer, "mcpServers", ti.id, ti.layer, ti.inst.Secret, allSecrets)
			if err != nil {
				return err
			}
			for v, sr := range refs {
				lock.Secrets[v] = sr
			}
		}
```

(d) In the inline-server loop (`for _, id := range slices.Sorted(maps.Keys(m.MCPServers))` where `srv.Template == ""` is skipped), after `srv := m.MCPServers[id]` and the `if srv.Template != "" { continue }` guard, substitute secrets before the `ContentHash` is computed:

```go
			refs, err := substituteSecrets(&srv, "mcpServers", id, layerName, srv.Secret, allSecrets)
			if err != nil {
				return err
			}
			for v, sr := range refs {
				lock.Secrets[v] = sr
			}
```

(e) In the `cliTools` loop, record CLI-tool secret refs (no substitution — CLI secrets are not rendered into config, per spec §3.3):

```go
		for _, id := range slices.Sorted(maps.Keys(m.CLITools)) {
			t := m.CLITools[id]
			if len(t.Secret) > 0 {
				refs, _, err := collectSecretRefs("cliTools", id, layerName, t.Secret, allSecrets)
				if err != nil {
					return err
				}
				for v, sr := range refs {
					lock.Secrets[v] = sr
				}
			}
			g.AddNode("cli:" + id)
```

In `internal/resolve/pipeline.go`, function `splitByLayer`:

(f) Add `Secrets` to the `mk()` literal and route secret refs by layer. Change the `mk` closure to include `Secrets: map[string]lockfile.SecretRef{}`, then before `return committed, personal` add:

```go
	for v, sr := range l.Secrets {
		if sr.Layer == string(manifest.LayerPersonal) {
			personal.Secrets[v] = sr
		} else {
			committed.Secrets[v] = sr
		}
	}
```

In `internal/resolve/render.go`, function `RenderResources`:

(g) In the `mcpServers` loop, after the `if srv.Template != "" { ... } else { ... }` block that sets `cmd/args/envMap/headersMap/url` and before `payload := map[string]any{...}`, substitute secrets so the placeholder lands in the payload:

```go
			secSrv := &manifest.MCPServer{Env: envMap, Headers: headersMap, URL: url}
			if _, err := substituteSecrets(secSrv, "mcpServers", id, manifest.Layer(entry.Layer), srv.Secret, collectSecrets(layers)); err != nil {
				return nil, err
			}
			envMap, headersMap, url = secSrv.Env, secSrv.Headers, secSrv.URL
```

For templated servers `srv.Secret` is the instance's secret map (not nilled on the manifest struct — `Instantiate` only nils the produced copy), so this covers both templated and inline servers.

(h) Add a `collectSecrets` helper to `render.go`, mirroring `collectTemplates`:

```go
// collectSecrets merges top-level secrets: from all layers; higher layers take
// precedence (same logic as collectTemplates).
func collectSecrets(layers map[manifest.Layer]*manifest.Manifest) map[string]manifest.Secret {
	all := map[string]manifest.Secret{}
	for _, layerName := range []manifest.Layer{manifest.LayerTeam, manifest.LayerRepo, manifest.LayerPersonal} {
		m, ok := layers[layerName]
		if !ok {
			continue
		}
		for name, s := range m.Secrets {
			if _, exists := all[name]; !exists {
				all[name] = s
			}
		}
	}
	return all
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/resolve/ -v`
Expected: PASS — both new tests and every pre-existing resolve test.

- [ ] **Step 5: Commit**

```bash
git add internal/resolve/pipeline.go internal/resolve/render.go internal/resolve/secret_pipeline_test.go
git commit -m "Render secret placeholders and record refs in the lockfile"
```

---

## Task 9: The `ainfra exec` command

**Files:**
- Create: `cmd/ainfra/cmd_exec.go`
- Modify: `cmd/ainfra/main.go`
- Test: `cmd/ainfra/cmd_exec_test.go`

- [ ] **Step 1: Write the failing test**

```go
package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MHilhorst/ainfra/internal/cli"
	"github.com/MHilhorst/ainfra/internal/lockfile"
	"github.com/MHilhorst/ainfra/internal/secret"
)

func TestExecResolvesSecretsIntoChildEnv(t *testing.T) {
	dir := t.TempDir()
	lock := &lockfile.Lock{
		Version: 1,
		Secrets: map[string]lockfile.SecretRef{
			"AINFRA_SECRET_MCPSERVERS_LINEAR_TOKEN": {
				Var: "AINFRA_SECRET_MCPSERVERS_LINEAR_TOKEN",
				Ref: "op://Eng/linear/mcp", Scheme: "op", Scope: "shared", Layer: "repo",
			},
		},
	}
	if err := lockfile.Write(filepath.Join(dir, "ainfra.lock"), lock); err != nil {
		t.Fatalf("Write lock: %v", err)
	}

	reg := secret.NewRegistry()
	reg.Add(fakeExecResolver{scheme: "op", value: "resolved-token"})

	var gotEnv []string
	execFn := func(argv0 string, argv, envv []string) error {
		gotEnv = envv
		return nil
	}

	ctx := cli.Context{Dir: dir, Args: []string{"echo", "hi"}, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}
	if code := runExecWith(ctx, reg, execFn); code != 0 {
		t.Fatalf("runExecWith exit code = %d, want 0", code)
	}

	found := false
	for _, kv := range gotEnv {
		if kv == "AINFRA_SECRET_MCPSERVERS_LINEAR_TOKEN=resolved-token" {
			found = true
		}
	}
	if !found {
		t.Errorf("child env missing resolved secret; env = %v", gotEnv)
	}
}

func TestExecAbortsWhenSecretUnresolvable(t *testing.T) {
	dir := t.TempDir()
	lock := &lockfile.Lock{
		Version: 1,
		Secrets: map[string]lockfile.SecretRef{
			"AINFRA_SECRET_X": {Var: "AINFRA_SECRET_X", Ref: "op://Eng/x/y", Scheme: "op", Layer: "repo"},
		},
	}
	if err := lockfile.Write(filepath.Join(dir, "ainfra.lock"), lock); err != nil {
		t.Fatalf("Write lock: %v", err)
	}

	reg := secret.NewRegistry()
	reg.Add(fakeExecResolver{scheme: "op", err: true})

	called := false
	execFn := func(string, []string, []string) error { called = true; return nil }

	var stderr bytes.Buffer
	ctx := cli.Context{Dir: dir, Args: []string{"echo"}, Stdout: &bytes.Buffer{}, Stderr: &stderr}
	if code := runExecWith(ctx, reg, execFn); code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if called {
		t.Error("child must not be launched when a secret fails to resolve")
	}
	if !strings.Contains(stderr.String(), "resolve") {
		t.Errorf("stderr = %q, want a resolution-failure message", stderr.String())
	}
}

// fakeExecResolver is a Resolver for the exec tests.
type fakeExecResolver struct {
	scheme, value string
	err           bool
}

func (f fakeExecResolver) Scheme() string { return f.scheme }
func (f fakeExecResolver) Resolve(string) (string, error) {
	if f.err {
		return "", errExecResolve
	}
	return f.value, nil
}
func (f fakeExecResolver) Check(string) error {
	if f.err {
		return errExecResolve
	}
	return nil
}

var errExecResolve = stringError("could not resolve")

type stringError string

func (e stringError) Error() string { return string(e) }
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/ainfra/ -run TestExec -v`
Expected: FAIL — `undefined: runExecWith`.

- [ ] **Step 3: Write the implementation**

Create `cmd/ainfra/cmd_exec.go`:

```go
package main

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/MHilhorst/ainfra/internal/cli"
	"github.com/MHilhorst/ainfra/internal/lockfile"
	"github.com/MHilhorst/ainfra/internal/secret"
	"github.com/MHilhorst/ainfra/internal/ui"
)

// execFunc replaces the current process with another. syscall.Exec in
// production; a recording stub in tests.
type execFunc func(argv0 string, argv []string, envv []string) error

// newExecCommand returns the `ainfra exec` command: it resolves every secret
// reference in the lockfile and runs a command with the values in its
// environment. No secret value is written to disk.
func newExecCommand() *cli.Command {
	return &cli.Command{
		Name:      "exec",
		Summary:   "Resolve secrets and run a command with them in its environment",
		UsageLine: "ainfra exec [-- <command> [args...]]",
		Example:   "ainfra exec -- claude",
		Run: func(ctx cli.Context) int {
			return runExecWith(ctx, secret.DefaultRegistry(), syscall.Exec)
		},
	}
}

// runExecWith is the testable core of `ainfra exec`.
func runExecWith(ctx cli.Context, reg *secret.Registry, execFn execFunc) int {
	errColor := ui.NewColorizer(ctx.Stderr, ctx.NoColor)

	argv := ctx.Args
	if len(argv) == 0 {
		argv = []string{"claude"}
	}

	committed, err := lockfile.Read(filepath.Join(ctx.Dir, "ainfra.lock"))
	if err != nil {
		ui.RenderError(ctx.Stderr, errColor, fmt.Errorf("ainfra.lock not found — run `ainfra lock` first"))
		return 1
	}
	personal, err := lockfile.Read(filepath.Join(ctx.Dir, "ainfra.personal.lock"))
	if err != nil {
		personal = &lockfile.Lock{}
	}

	// The secret set is the union of both lockfiles.
	refs := map[string]lockfile.SecretRef{}
	maps.Copy(refs, committed.Secrets)
	maps.Copy(refs, personal.Secrets)

	// Resolve every ref, collecting all failures before aborting.
	resolved := map[string]string{}
	var failures []string
	for _, v := range slices.Sorted(maps.Keys(refs)) {
		sr := refs[v]
		val, err := reg.Resolve(expandUser(sr.Ref))
		if err != nil {
			failures = append(failures, "  "+err.Error())
			continue
		}
		resolved[sr.Var] = val
	}
	if len(failures) > 0 {
		fmt.Fprintln(ctx.Stderr, "Could not resolve secrets:")
		for _, f := range failures {
			fmt.Fprintln(ctx.Stderr, f)
		}
		return 1
	}

	// Build the child environment: the current env plus one var per secret.
	envv := os.Environ()
	for _, v := range slices.Sorted(maps.Keys(resolved)) {
		envv = append(envv, v+"="+resolved[v])
	}

	bin, err := exec.LookPath(argv[0])
	if err != nil {
		ui.RenderError(ctx.Stderr, errColor, fmt.Errorf("command not found: %s", argv[0]))
		return 1
	}
	if err := execFn(bin, argv, envv); err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}
	return 0 // unreachable on success: syscall.Exec replaces this process
}

// expandUser substitutes ${user} in a personal-scope ref with the OS username.
func expandUser(ref string) string {
	if !strings.Contains(ref, "${user}") {
		return ref
	}
	name := os.Getenv("USER")
	if u, err := user.Current(); err == nil && u.Username != "" {
		name = u.Username
	}
	return strings.ReplaceAll(ref, "${user}", name)
}
```

- [ ] **Step 4: Register the command**

In `cmd/ainfra/main.go`, add `reg.Add(newExecCommand())` after `reg.Add(newCheckCommand())`:

```go
	reg.Add(newCheckCommand())
	reg.Add(newExecCommand())
	reg.Add(newVersionCommand())
```

- [ ] **Step 5: Run the tests**

Run: `go test ./cmd/ainfra/ -run TestExec -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add cmd/ainfra/cmd_exec.go cmd/ainfra/main.go cmd/ainfra/cmd_exec_test.go
git commit -m "Add ainfra exec command to resolve secrets and run a command"
```

---

## Task 10: `check` verifies secret references

**Files:**
- Modify: `cmd/ainfra/commands.go`
- Test: `cmd/ainfra/cmd_check_secret_test.go`

- [ ] **Step 1: Write the failing test**

```go
package main

import (
	"strings"
	"testing"

	"github.com/MHilhorst/ainfra/internal/lockfile"
	"github.com/MHilhorst/ainfra/internal/secret"
)

func TestCheckSecretsReportsUnresolvableRefs(t *testing.T) {
	committed := &lockfile.Lock{
		Secrets: map[string]lockfile.SecretRef{
			"AINFRA_SECRET_OK":  {Var: "AINFRA_SECRET_OK", Ref: "op://Eng/ok/f", Scheme: "op"},
			"AINFRA_SECRET_BAD": {Var: "AINFRA_SECRET_BAD", Ref: "op://Eng/bad/f", Scheme: "op"},
		},
	}
	reg := secret.NewRegistry()
	reg.Add(checkSecretResolver{})

	failures := checkSecrets(committed, &lockfile.Lock{}, reg)
	if len(failures) != 1 {
		t.Fatalf("got %d failures, want 1: %v", len(failures), failures)
	}
	if failures[0] == "" {
		t.Error("failure message is empty")
	}
}

// checkSecretResolver fails Check for any ref containing "bad".
type checkSecretResolver struct{}

func (checkSecretResolver) Scheme() string                { return "op" }
func (checkSecretResolver) Resolve(string) (string, error) { return "v", nil }
func (checkSecretResolver) Check(ref string) error {
	if strings.Contains(ref, "bad") {
		return stringError("item not found")
	}
	return nil
}
```

Note: `stringError` is already defined in `cmd_exec_test.go` (Task 9) — reuse it, do not redefine it.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/ainfra/ -run TestCheckSecrets -v`
Expected: FAIL — `undefined: checkSecrets`.

- [ ] **Step 3: Write the implementation**

In `cmd/ainfra/commands.go`, add the `checkSecrets` helper (place it after `runCheck`):

```go
// checkSecrets verifies every secret reference in both lockfiles is resolvable.
// It returns one message per unresolvable ref; the messages never contain a
// secret value.
func checkSecrets(committed, personal *lockfile.Lock, reg *secret.Registry) []string {
	refs := map[string]lockfile.SecretRef{}
	maps.Copy(refs, committed.Secrets)
	maps.Copy(refs, personal.Secrets)

	var failures []string
	for _, v := range slices.Sorted(maps.Keys(refs)) {
		if err := reg.Check(refs[v].Ref); err != nil {
			failures = append(failures, err.Error())
		}
	}
	return failures
}
```

Add the imports `"maps"`, `"slices"`, and `"github.com/MHilhorst/ainfra/internal/secret"` to the import block of `commands.go` (keep the existing imports).

In `runCheck`, after the existing drift block and before `return 0`, wire the helper in. Replace the tail of `runCheck` — the part from `if allEmpty {` onward — with:

```go
	secretFailures := checkSecrets(committed, personal, secret.DefaultRegistry())

	if allEmpty && len(secretFailures) == 0 {
		fmt.Fprintln(ctx.Stdout, "No drift.")
		return 0
	}

	c := ui.NewColorizer(ctx.Stdout, ctx.NoColor)
	if !allEmpty {
		ui.RenderPlan(ctx.Stdout, c, plans)
	}
	if len(secretFailures) > 0 {
		fmt.Fprintln(ctx.Stderr, "Unresolvable secrets:")
		for _, f := range secretFailures {
			fmt.Fprintf(ctx.Stderr, "  %s\n", f)
		}
	}
	return 1
```

- [ ] **Step 4: Run the tests**

Run: `go test ./cmd/ainfra/ -v`
Expected: PASS — the new test and every pre-existing `cmd/ainfra` test (including `cmd_check_test.go`).

- [ ] **Step 5: Commit**

```bash
git add cmd/ainfra/commands.go cmd/ainfra/cmd_check_secret_test.go
git commit -m "Verify secret references during ainfra check"
```

---

## Task 11: Full build and test sweep

**Files:** none — verification only.

- [ ] **Step 1: Build the whole module**

Run: `go build ./...`
Expected: no output, exit 0.

- [ ] **Step 2: Run the whole test suite**

Run: `go test ./...`
Expected: every package `ok`, no `FAIL`.

- [ ] **Step 3: Vet**

Run: `go vet ./...`
Expected: no output, exit 0.

- [ ] **Step 4: Manual smoke test**

```bash
mkdir -p /tmp/ainfra-secret-smoke && cd /tmp/ainfra-secret-smoke
cat > ainfra.yaml <<'YAML'
version: 1
mcpServers:
  linear:
    transport: http
    url: https://mcp.linear.app/sse
    headers:
      Authorization: "Bearer ${secret.token}"
    secret:
      token:
        mode: direct
        ref: "env://SMOKE_TOKEN"
YAML
go run github.com/MHilhorst/ainfra/cmd/ainfra lock
grep -q 'AINFRA_SECRET_MCPSERVERS_LINEAR_TOKEN' ainfra.lock && echo "lock OK"
SMOKE_TOKEN=smoke-value go run github.com/MHilhorst/ainfra/cmd/ainfra exec -- printenv AINFRA_SECRET_MCPSERVERS_LINEAR_TOKEN
```

Expected: `lock OK`, and the final command prints `smoke-value`.

- [ ] **Step 5: Commit (if any doc or cleanup changes were needed)**

```bash
git add -A
git commit -m "Verify secret resolver build and tests" --allow-empty
```

---

## Self-Review Notes

- **Spec coverage:** §2 resolver package → Tasks 1-5; §3 render side → Tasks 6-8; §4 `ainfra exec` → Task 9; §5 `check` → Task 10; §6 error handling → Tasks 3, 9, 10; §7 testing → tests in every task plus Task 11. §8 non-goals: stub adapter (Task 4) covers doppler/vault/sops; brokered mode handled as the `default` branch in `collectSecretRefs` (Task 7) with no value and no ref recorded.
- **`brokered` mode:** `collectSecretRefs` sets an empty interpolation value and records no `SecretRef`, leaving brokered entries untouched by the resolver — consistent with spec §1 decision 4.
- **Type consistency:** `lockfile.SecretRef` fields (`Var`, `Ref`, `Scheme`, `Scope`, `Layer`) are used identically in Tasks 6-10. `execFunc` matches the `syscall.Exec` signature `(argv0 string, argv []string, envv []string) error`. `secret.Registry` is passed by pointer everywhere (`*secret.Registry`).
- **`settings.json`:** intentionally untouched — CLI-tool secrets are recorded in the lockfile (Task 8e) and delivered via the session environment by `ainfra exec`, never written to `settings.json` (spec §3.3).
