# Validation Hardening Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Catch two classes of "green-but-wrong" config at `lock`/`validate` time instead of at `apply` time — a CLI tool ainfra cannot install, and a malformed secret reference.

**Architecture:** Two independent additions. (1) A lock-time preflight: after `ainfra lock` succeeds, warn (do not fail) for every `cliTool` whose `install:` block declares no method any package adapter recognises — `apply` falls back to a bare PATH probe for such tools, so a green lock is misleading. (2) A `manifest.Validate` structural check of `secrets:` entries: a `ref` using a known scheme (`op://`, `env://`) must have the right shape, caught with no network call.

**Tech Stack:** Go 1.25, standard library. Tests use the `run()` CLI harness and `manifest.Validate`/`ValidateAll`.

**Context:** Items #3 and #6 of `docs/superpowers/specs/2026-05-22-apply-hardening-design.md`. Items #5, #1, #2 are merged. Re-grounded against current `main`:
- `pkg.Select` (`internal/provider/pkg/pkg.go`) recognises `brew`, `npm`, `npm-g`, `composer`. A `cliTool` with no recognised method is silently skipped by `CLITools.applyOne`, which then falls back to a `<id> --version` probe — failing at `apply` if the tool is not on `PATH`. Nothing at `lock`/`validate` time flags this.
- `manifest.Validate` (`internal/manifest/validate.go`) has per-channel loops returning `*diag.Diagnostic` but **no loop over `m.Secrets`** — secret refs are entirely unvalidated structurally. (Runtime reachability is separately checked by `ainfra check`; this plan adds the structural, offline check.)

Item #3's other sub-ideas from the spec are intentionally **not** in this plan: surfacing `<resolved:...>` placeholders needs investigation of when those resolve, and changing `lock`'s "next" message to point at `ainfra check` is unsound (`check` reports drift and exits non-zero before the first apply). Item #4 (personal-layer onboarding) is deferred — it is larger and entangled with the secret resolver.

---

### Task 1: CLI-tool install-method preflight at lock time

After `ainfra lock` succeeds, print a warning for each `cliTool` whose `install:` block has no method a package adapter can automate. This is a warning, not an error — the tool may still be present on `PATH` — but it makes a green lock honest about what `apply` will and will not install.

**Files:**
- Modify: `internal/provider/pkg/pkg.go` (add `Methods()`)
- Create: `cmd/ainfra/preflight.go`
- Modify: `cmd/ainfra/cmd_lock.go` (`runLock`)
- Test: `internal/provider/pkg/pkg_test.go`, `cmd/ainfra/preflight_test.go`

- [ ] **Step 1: Write the failing test for `pkg.Methods()`**

Append to `internal/provider/pkg/pkg_test.go`:

```go
func TestMethods(t *testing.T) {
	got := Methods()
	for _, want := range []string{"brew", "npm", "npm-g", "composer"} {
		found := false
		for _, m := range got {
			if m == want {
				found = true
			}
		}
		if !found {
			t.Errorf("Methods() = %v, missing %q", got, want)
		}
	}
	// Every reported method must actually resolve via Select.
	for _, m := range got {
		if _, ok := Select(m); !ok {
			t.Errorf("Methods() reported %q but Select(%q) does not recognise it", m, m)
		}
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/provider/pkg/ -run TestMethods -v`
Expected: FAIL — `Methods` is undefined.

- [ ] **Step 3: Implement `pkg.Methods()`**

In `internal/provider/pkg/pkg.go`, add (the file already imports nothing beyond `provider`; add `"sort"`):

```go
// Methods returns the sorted set of install-method names Select recognises.
// It is the single source of truth for "which methods ainfra can automate".
func Methods() []string {
	ms := []string{"brew", "npm", "npm-g", "composer"}
	sort.Strings(ms)
	return ms
}
```

Then make `Select` the consistency anchor — leave `Select` as-is; `TestMethods` already asserts every `Methods()` entry resolves via `Select`.

- [ ] **Step 4: Run it to verify it passes**

Run: `go test ./internal/provider/pkg/ -run TestMethods -v`
Expected: PASS.

- [ ] **Step 5: Write the failing test for the preflight**

Create `cmd/ainfra/preflight_test.go`:

```go
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLockWarnsOnUnautomatableCLITool(t *testing.T) {
	dir := t.TempDir()
	// `jq` installs via brew (automatable); `legacy-tool` declares only an
	// unrecognised method, so apply can only probe for it on PATH.
	yaml := "version: 1\n" +
		"cliTools:\n" +
		"  jq:\n" +
		"    install:\n" +
		"      brew:\n" +
		"        formula: jq\n" +
		"  legacy-tool:\n" +
		"    install:\n" +
		"      manual:\n" +
		"        url: https://example.com/legacy\n"
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	code := run([]string{"--chdir", dir, "lock"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("lock: code=%d out=%q err=%q", code, out.String(), errOut.String())
	}
	combined := out.String() + errOut.String()
	if !strings.Contains(combined, "legacy-tool") {
		t.Errorf("expected a warning naming 'legacy-tool', got: %q", combined)
	}
	if strings.Contains(combined, `"jq"`) {
		t.Errorf("jq installs via brew and must not be warned about, got: %q", combined)
	}
}
```

- [ ] **Step 6: Run it to verify it fails**

Run: `go test ./cmd/ainfra/ -run TestLockWarnsOnUnautomatableCLITool -v`
Expected: FAIL — `lock` prints no such warning.

- [ ] **Step 7: Implement the preflight**

Create `cmd/ainfra/preflight.go`:

```go
package main

import (
	"fmt"
	"maps"
	"slices"

	"github.com/MHilhorst/ainfra/internal/manifest"
	"github.com/MHilhorst/ainfra/internal/provider/pkg"
)

// cliToolInstallWarnings returns one warning per cliTool whose install: block
// declares no method ainfra can automate. apply falls back to a bare PATH
// probe for such tools, so a successful lock does not mean apply will install
// them. Entries are de-duplicated across layers and reported in id order.
func cliToolInstallWarnings(layers map[manifest.Layer]*manifest.Manifest) []string {
	var warnings []string
	seen := map[string]bool{}
	for _, ln := range []manifest.Layer{manifest.LayerTeam, manifest.LayerRepo, manifest.LayerPersonal} {
		m, ok := layers[ln]
		if !ok {
			continue
		}
		for _, id := range slices.Sorted(maps.Keys(m.CLITools)) {
			if seen[id] {
				continue
			}
			seen[id] = true
			t := m.CLITools[id]
			automatable := false
			for method := range t.Install {
				if _, ok := pkg.Select(method); ok {
					automatable = true
					break
				}
			}
			if !automatable {
				declared := slices.Sorted(maps.Keys(t.Install))
				warnings = append(warnings, fmt.Sprintf(
					"cliTool %q declares no install method ainfra can automate "+
						"(declared: %v; automatable: %v) — apply will probe for it "+
						"on PATH and fail if it is not already installed",
					id, declared, pkg.Methods()))
			}
		}
	}
	return warnings
}
```

- [ ] **Step 8: Wire the preflight into `runLock`**

In `cmd/ainfra/cmd_lock.go`, add `"github.com/MHilhorst/ainfra/internal/manifest"` to the import block. In `runLock`, after the existing `ui.Next(...)` line and before `return 0`, insert:

```go
	if layers, err := manifest.LoadLayers(ctx.Dir); err == nil {
		for _, w := range cliToolInstallWarnings(layers) {
			fmt.Fprintln(ctx.Stderr, c.Yellow("warning: "+w))
		}
	}
```

(`c` is the `*ui.Colorizer` already constructed in `runLock`. `LoadLayers` here cannot fail in practice — `resolve.RunLock` already loaded the same layers successfully — but the error is tolerated best-effort, matching `warnIfStale`.)

- [ ] **Step 9: Run the tests to verify they pass**

Run: `go test ./internal/provider/pkg/ ./cmd/ainfra/ -run 'TestMethods|TestLockWarns' -v`
Expected: both PASS.

- [ ] **Step 10: Run the whole suite**

Run: `go test ./... && go vet ./...`
Expected: all packages PASS, vet clean.

- [ ] **Step 11: Commit**

```bash
git add internal/provider/pkg/pkg.go internal/provider/pkg/pkg_test.go cmd/ainfra/preflight.go cmd/ainfra/preflight_test.go cmd/ainfra/cmd_lock.go
git commit -m "Warn at lock time about CLI tools ainfra cannot install"
```

---

### Task 2: Structural validation of secret references

`manifest.Validate` does not inspect `secrets:` entries. A typo in a `ref` — a missing field in an `op://` path, an empty `env://` variable — sails through `validate` and `lock` and only fails at resolve/exec time. Add a structural, offline check.

**Files:**
- Modify: `internal/manifest/validate.go` (`Validate`, plus a `validateSecrets` helper)
- Test: `internal/manifest/validate_test.go`

- [ ] **Step 1: Confirm the scheme names**

Before writing code, read `internal/secret/registry.go` and `internal/secret/stub.go` and confirm the resolver scheme strings are exactly `op` and `env` (the structural check below covers those two; brokered schemes like `doppler`/`vault`/`sops` are stubs and are intentionally not shape-checked). Also read the existing `Validate` function in `internal/manifest/validate.go` and one existing per-channel check block, so the new code matches the file's `*diag.Diagnostic` style.

- [ ] **Step 2: Write the failing tests**

Append to `internal/manifest/validate_test.go` (match the import style and helpers already in that file; the tests below assume `Validate(m)` returns an `error` that is a `*diag.Diagnostic`):

```go
func TestValidateRejectsMalformedOpRef(t *testing.T) {
	m := &Manifest{
		Version: 1,
		Secrets: map[string]Secret{
			"db-password": {Mode: "reference", Ref: "op://Vault/Item"}, // missing field segment
		},
	}
	err := Validate(m)
	if err == nil {
		t.Fatal("expected an error for an op:// ref missing its field segment")
	}
	if !strings.Contains(err.Error(), "op://") {
		t.Errorf("error should mention the op:// shape, got: %v", err)
	}
}

func TestValidateRejectsEmptyEnvRef(t *testing.T) {
	m := &Manifest{
		Version: 1,
		Secrets: map[string]Secret{
			"token": {Mode: "reference", Ref: "env://"}, // no variable name
		},
	}
	if err := Validate(m); err == nil {
		t.Fatal("expected an error for an env:// ref with no variable name")
	}
}

func TestValidateAcceptsWellFormedRefs(t *testing.T) {
	m := &Manifest{
		Version: 1,
		Secrets: map[string]Secret{
			"db-password": {Mode: "reference", Ref: "op://Vault/Item/field"},
			"token":       {Mode: "reference", Ref: "env://API_TOKEN"},
			"literal":     {Mode: "direct", Value: "inline-value"},
		},
	}
	if err := Validate(m); err != nil {
		t.Errorf("well-formed secrets should validate, got: %v", err)
	}
}
```

If `validate_test.go` does not already import `strings`, add it.

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/manifest/ -run TestValidate -v`
Expected: the three new tests FAIL (no secret validation exists; malformed refs are accepted).

- [ ] **Step 4: Implement `validateSecrets`**

In `internal/manifest/validate.go`, add a `validateSecrets` helper and call it from `Validate`. Add the call near the other channel checks in `Validate` (before `return nil`):

```go
	if d := validateSecrets(m); d != nil {
		return d
	}
```

Add the helper (the file already imports `fmt`, `maps`, `slices`, `strings`, and `internal/diag` — confirm and reuse):

```go
// validateSecrets checks every secrets: entry for a structurally malformed
// reference. It is offline-only — it does not resolve the secret, just checks
// the shape of a ref that uses a scheme ainfra knows. Entries are checked in
// sorted-key order so the first reported problem is deterministic.
func validateSecrets(m *Manifest) *diag.Diagnostic {
	for _, id := range slices.Sorted(maps.Keys(m.Secrets)) {
		s := m.Secrets[id]
		if s.Ref == "" {
			continue
		}
		scheme, rest, ok := strings.Cut(s.Ref, "://")
		if !ok {
			continue // not a scheme-style ref; nothing structural to check
		}
		switch scheme {
		case "op":
			// op://<vault>/<item>/<field> — at least three non-empty segments.
			segs := strings.Split(rest, "/")
			nonEmpty := 0
			for _, seg := range segs {
				if seg != "" {
					nonEmpty++
				}
			}
			if nonEmpty < 3 {
				return &diag.Diagnostic{
					Summary: "malformed op:// secret reference",
					Path:    "secrets." + id,
					Detail:  fmt.Sprintf("Secret %q has ref %q; a 1Password reference is op://<vault>/<item>/<field>.", id, s.Ref),
					Hint:    `Use three segments, e.g.  ref: "op://Engineering/Database/password"`,
				}
			}
		case "env":
			if strings.TrimSpace(rest) == "" {
				return &diag.Diagnostic{
					Summary: "malformed env:// secret reference",
					Path:    "secrets." + id,
					Detail:  fmt.Sprintf("Secret %q has ref %q but names no environment variable.", id, s.Ref),
					Hint:    `Name the variable, e.g.  ref: "env://API_TOKEN"`,
				}
			}
		}
	}
	return nil
}
```

If `validate.go` does not already import `strings`, add it.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/manifest/ -run TestValidate -v`
Expected: the three new tests PASS, and every pre-existing `TestValidate*` test still PASSES.

- [ ] **Step 6: Run the whole suite**

Run: `go test ./... && go vet ./...`
Expected: all packages PASS, vet clean.

- [ ] **Step 7: Commit**

```bash
git add internal/manifest/validate.go internal/manifest/validate_test.go
git commit -m "Structurally validate op:// and env:// secret references"
```

---

## Self-review notes

- **Spec coverage.** Item #3's CLI-tool preflight (Task 1) and item #6's structural
  secret-ref validation (Task 2) are delivered. Explicitly out of scope, with
  reasons in the Context section: surfacing `<resolved:...>` placeholders,
  re-pointing `lock`'s "next" message, `mode`-value validation (the valid set
  is owned by `internal/resolve`, not statically known here), and item #4.
- **Warning vs error.** Task 1 emits a *warning* and `lock` still exits 0 — an
  unautomatable cliTool is not necessarily broken (it may be on `PATH`). Task 2
  is a hard *validation error* — a malformed `op://`/`env://` ref is always
  wrong.
- **`pkg.Methods()` is the drift guard.** `TestMethods` asserts every method
  `Methods()` returns also resolves via `Select`, so the two cannot drift apart
  silently if a future adapter is added.
- **No layering inversion.** Task 1's preflight lives in `cmd/ainfra`, which may
  import both `manifest` and `provider/pkg`. Task 2 stays inside `manifest` and
  parses the ref scheme with `strings` — it does not import `internal/secret`,
  so `manifest` gains no new dependency.
- **Type consistency.** `validateSecrets` returns `*diag.Diagnostic` like the
  other `validate.go` helpers; `cliToolInstallWarnings` returns `[]string`
  rendered by `runLock` with the existing `ui.Colorizer`.
