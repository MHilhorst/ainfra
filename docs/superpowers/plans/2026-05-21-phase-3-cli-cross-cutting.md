# Phase 3 — Plan 5: CLI Tools, Background Services, Preconditions

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development. Steps use `- [ ]`.

**Goal:** Implement the CLI-tools provider (with package-manager adapters), the background-services provider (generates start/stop scripts, never supervises), and the preconditions checker (verify-only). Also resolve the `cliTools` channel into the lockfile — the gap Plan 1 left open.

**Architecture:** `cliTools` resolution mirrors the other channels in `RunLock`. The CLI-tools provider delegates install/probe to a `PackageAdapter` (`brew`, `npm -g`, plus a declare-and-check fallback). Background services render start/stop scripts as owned files. Preconditions are verify-only — the checker runs declared checks and fails loudly; there is no Apply.

**Tech Stack:** Go, standard `testing`.

---

### Task 1: Resolve the cliTools channel into the lockfile

**Files:** Modify `internal/resolve/pipeline.go`; test `internal/resolve/pipeline_test.go`.

`RunLock` never records `cliTools` entries. Add resolution mirroring the hooks/commands loops.

- [ ] **Step 1 — failing test:** a manifest with a `cliTools` entry produces an `ainfra.lock` containing a `cliTools:` section with that tool id and a `contentHash:`.
- [ ] **Step 2 — run, see fail.**
- [ ] **Step 3 — implement.** In `RunLock`'s second layer loop, after the tools block, add a loop over `slices.Sorted(maps.Keys(m.CLITools))`: for each, `g.AddNode("cli:"+id)`, and `lock.Entries.CLITools[id] = lockfile.Entry{ Layer: string(layerName), Constraint: t.VersionConstraint, ContentHash: lockfile.ContentHash(map[string]any{"versionConstraint": t.VersionConstraint, "install": t.Install, "check": t.Check}) }`. Confirm `splitByLayer` already routes `CLITools` (it was added in Plan 1's final fix) — if not, add the route.
- [ ] **Step 4 — run, see pass.**
- [ ] **Step 5 — commit** `Resolve the cliTools channel into the lockfile`.

---

### Task 2: Package-manager adapters

**Files:** Create `internal/provider/pkg/pkg.go`, `pkg_test.go`.

- [ ] **Step 1 — failing tests** (package `pkg`), using `provider.NewFakeRunner()`:
  - `BrewAdapter.IsInstalled` runs `brew list --versions <formula>` and reports true when the runner returns output, false when it errors.
  - `BrewAdapter.Install` runs `brew install <formula>`.
  - `NpmAdapter.IsInstalled` runs `npm ls -g --depth 0 <pkg>` similarly; `Install` runs `npm install -g <pkg>`.
  - `Select(method string)` returns the matching adapter, or the `nil` adapter / a `false` ok for an unknown method.

- [ ] **Step 2 — run, see fail.**

- [ ] **Step 3 — implement `pkg.go`:**

```go
// Package pkg provides package-manager adapters that install CLI tools.
package pkg

import "github.com/MHilhorst/ainfra/internal/provider"

// Adapter installs and probes a CLI tool through one package manager.
type Adapter interface {
	Name() string
	IsInstalled(env provider.Env, tool string) (bool, error)
	Install(env provider.Env, tool string) error
}
```

  - `BrewAdapter` — `Name()` = `"brew"`; `IsInstalled` runs `env.Runner.Run("brew", "list", "--versions", tool)` and returns `err == nil`; `Install` runs `env.Runner.Run("brew", "install", tool)` and returns its error.
  - `NpmAdapter` — `Name()` = `"npm"`; `IsInstalled` runs `env.Runner.Run("npm", "ls", "-g", "--depth", "0", tool)`; `Install` runs `env.Runner.Run("npm", "install", "-g", tool)`.
  - `Select(method string) (Adapter, bool)` — returns `BrewAdapter{}` for `"brew"`, `NpmAdapter{}` for `"npm"`/`"npm-g"`, else `nil, false`.

- [ ] **Step 4 — run, see pass.**
- [ ] **Step 5 — commit** `Add brew and npm package-manager adapters`.

---

### Task 3: CLI-tools provider

**Files:** Create `internal/provider/channels/clitools.go`, `clitools_test.go`.

`Resource.Payload` keys: `install` (map[string]any of method -> spec; the first key whose method `pkg.Select` recognises is used) and `check` (optional). When no adapter matches, the provider does declare-and-check: report whether the tool is on `PATH` and, if not, surface an actionable error.

- [ ] **Step 1 — failing tests:** `Channel()` returns `"cliTools"`; `Observe` returns a resource for each desired tool that is installed — since `Observe` has no desired list, instead: `Observe` returns an empty slice always, and the provider relies on the orchestrator treating a never-applied tool as a create (acceptable — installs are idempotent). Test `Apply` Create with a `brew` install method calls the brew adapter's install (assert via `FakeRunner.Calls`); `Apply` with an unknown install method and the tool absent from PATH returns an actionable error; `env.DryRun` runs no install commands.

- [ ] **Step 2 — run, see fail.**

- [ ] **Step 3 — implement `clitools.go`.** Package `channels`, type `CLITools struct{}`. `Channel()` = `"cliTools"`. `Observe` returns `nil, nil` (installs are idempotent; the orchestrator will plan a create, and Apply re-checks). `Apply`: for each Create/Update change, read `install` from `Change.Resource.Payload`; iterate its method keys, and for the first one `pkg.Select` recognises, call `adapter.IsInstalled` — if already installed, record a noop-equivalent applied entry; else if `env.DryRun` skip, else `adapter.Install`. If no method matches any adapter, run `env.Runner.Run(tool, "--version")` as a declare-and-check probe; if that errors, return `fmt.Errorf("cliTools: %q is not installed and no supported install method is declared; install it manually", id)`. Delete changes: a no-op with a note (ainfra does not uninstall CLI tools — comment this). Honor `env.DryRun`.

- [ ] **Step 4 — run, see pass.**
- [ ] **Step 5 — commit** `Add the CLI-tools channel provider`.

---

### Task 4: Background-services provider

**Files:** Create `internal/provider/channels/services.go`, `services_test.go`.

A background service is reconciled by *generating* its start/stop scripts as owned files under `<root>/.ainfra/services/<id>/` (`start.sh`, `stop.sh`); ainfra never runs or supervises the process (design §7).

`Resource.Payload` keys: `kind` (string) and `spec` (map[string]any). For this increment the scripts are simple: `start.sh` contains `#!/bin/sh\n# ainfra-generated start script for service <id> (kind <kind>)\n` followed by a `spec`-derived command if `spec["command"]` is a string, else a `# TODO` placeholder line; `stop.sh` similarly.

- [ ] **Step 1 — failing tests:** `Channel()` returns `"backgroundServices"`; `Apply` Create writes `.ainfra/services/<id>/start.sh` and `stop.sh`; `Observe` returns an id for each service directory present under `.ainfra/services/`; Delete removes the service directory (use `env.FS.RemoveAll`); `env.DryRun` writes nothing.

- [ ] **Step 2 — run, see fail.**

- [ ] **Step 3 — implement `services.go`.** Package `channels`, type `Services struct{}`. `Channel()` = `"backgroundServices"`. Services dir = `filepath.Join(env.Root, ".ainfra", "services")`. `Observe` lists subdirectories there via `env.FS.ReadDir` (missing → none). `Apply` Create/Update writes the two scripts via `fsmerge.WriteOwnedFile`; Delete `env.FS.RemoveAll`s the service dir. Honor `env.DryRun`. Add a comment: ainfra generates the service definition only; starting and supervising the process is out of scope (design §7).

- [ ] **Step 4 — run, see pass.**
- [ ] **Step 5 — commit** `Add the background-services channel provider`.

---

### Task 5: Preconditions checker

**Files:** Create `internal/provider/precond/precond.go`, `precond_test.go`.

Preconditions are verify-only. A `Checker` runs each declared precondition and returns the failures; the orchestrator calls it before apply and aborts loudly on any failure.

- [ ] **Step 1 — failing tests** (package `precond`): a precondition whose `check` is `{command: "true"}` passes; one whose `check` is `{command: "false"}` fails and the failure carries the precondition id and its remediation text; `CheckAll` aggregates failures.

- [ ] **Step 2 — run, see fail.**

- [ ] **Step 3 — implement `precond.go`:**

```go
// Package precond runs verify-only manifest preconditions.
package precond

import "github.com/MHilhorst/ainfra/internal/provider"

// Precondition is one verify-only check.
type Precondition struct {
	ID          string
	Command     string // a shell command; exit 0 means the precondition holds
	Remediation string
}

// Failure is a precondition that did not hold.
type Failure struct {
	ID          string
	Remediation string
}

// CheckAll runs every precondition and returns the failures.
func CheckAll(env provider.Env, ps []Precondition) []Failure {
	var out []Failure
	for _, p := range ps {
		if p.Command == "" {
			continue
		}
		if _, err := env.Runner.Run("sh", "-c", p.Command); err != nil {
			out = append(out, Failure{ID: p.ID, Remediation: p.Remediation})
		}
	}
	return out
}
```

  Tests use `provider.NewFakeRunner()` with `sh -c true` scripted to succeed and `sh -c false` scripted to error.

- [ ] **Step 4 — run, see pass.**
- [ ] **Step 5 — commit** `Add the preconditions checker`.

---

### Task 6: Verification

- [ ] **Step 1:** `go build ./... && go test ./...` — green.
- [ ] **Step 2:** `go vet ./internal/... ` — clean.
- [ ] **Step 3:** commit any vet fix.

---

## Self-Review

**Spec coverage:** cliTools provider + adapters (spec §3.5, §6) — Tasks 2-3; cliTools lockfile resolution (Plan 1 gap) — Task 1; background-services generation, no supervision (spec §3.4 cross-cutting, design §7) — Task 4; preconditions verify-only (spec §3.4) — Task 5.

**Type consistency:** `Adapter`/`BrewAdapter`/`NpmAdapter`/`Select` in package `pkg`; `CLITools`/`Services` in package `channels`; `Precondition`/`Failure`/`CheckAll` in package `precond`. All providers honor `env.DryRun`.

**Out of scope:** `apt`/`uv`/`cargo`/direct-download adapters (follow-up); the `plan`/`apply`/`check` command wiring, manifest-to-Resource rendering, precondition gating in the orchestrator, and end-to-end tests (Plan 6).
