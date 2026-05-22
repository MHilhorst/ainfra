# cliTool Resolution Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:test-driven-development for each task. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Make `ainfra apply` resolve and install cliTools correctly, so it gets past the first channel and completes.

**Architecture:** Change the `pkg.Adapter` interface so adapters receive the install *spec* (formula/cask/package) instead of a bare id; fix the type-assertion bug in the cliTools channel; make the declare-and-check fallback use the manifest `check.command`.

**Tech Stack:** Go. Tests use the existing `provider` fakes (`FakeRunner`).

See `docs/superpowers/specs/2026-05-22-clitool-resolution-design.md` for full rationale.

---

### Task 1: Adapters take the install spec

**Files:**
- Modify: `internal/provider/pkg/pkg.go`
- Modify: `internal/provider/pkg/pkg_test.go`

- [ ] **Step 1: Write failing tests** in `pkg_test.go` for the new behaviour:
  - `BrewAdapter.Install` with spec `{"formula": "mysql-client"}` runs `brew install mysql-client`.
  - `BrewAdapter.Install` with spec `{"cask": "1password-cli"}` runs `brew install --cask 1password-cli`.
  - `BrewAdapter.IsInstalled` with a `formula` spec runs `brew list --versions mysql-client`; with a `cask` spec runs `brew list --cask --versions 1password-cli`.
  - `BrewAdapter` with a spec missing both `formula` and `cask` returns an error.
  - `NpmAdapter.Install` with `{"package": "x", "version": "1.1.1"}` runs `npm install -g x@1.1.1`; with `{"package": "x"}` runs `npm install -g x`.
  - `NpmAdapter.IsInstalled` with `{"package": "x"}` runs `npm ls -g --depth 0 x`.
  - `NpmAdapter` with a spec missing `package` returns an error.
  Update any existing `pkg_test.go` cases to the new `map[string]any`-spec signature.

- [ ] **Step 2: Run the tests, confirm they fail to compile / fail.**
  Run: `go test ./internal/provider/pkg/...`

- [ ] **Step 3: Change the `Adapter` interface and both adapters** in `pkg.go`:
  - `IsInstalled(env provider.Env, spec map[string]any) (bool, error)` and `Install(env provider.Env, spec map[string]any) error`.
  - `BrewAdapter`: a helper derives `(name string, cask bool, err error)` from the spec — `cask` key wins, else `formula`, else error. `IsInstalled` runs `brew list --versions` plus `--cask` when cask. `Install` runs `brew install` plus `--cask` when cask.
  - `NpmAdapter`: derive `package` (error if absent). `Install` appends `@<version>` when `version` is a non-empty string. `IsInstalled` uses the bare package name.
  - `Select` unchanged.

- [ ] **Step 4: Run the tests, confirm they pass.** `go test ./internal/provider/pkg/...`

- [ ] **Step 5: Commit.** `git add internal/provider/pkg && git commit -m "Adapters take the install spec, not a bare tool id"`

---

### Task 2: Fix the cliTools channel

**Files:**
- Modify: `internal/provider/channels/clitools.go`
- Modify: `internal/provider/channels/clitools_test.go`

- [ ] **Step 1: Write failing tests** in `clitools_test.go`:
  - A cliTool whose payload `install` is `map[string]map[string]any{"brew": {"formula": "X"}}`, in an uninstalled state, triggers `brew install X` (assert the runner saw that, NOT `brew install <id>`).
  - A cliTool with `install` `{"brew": {"cask": "Y"}}` triggers `brew install --cask Y`.
  - A cliTool with an empty/unrecognised `install` map but a `check` payload `{"command": "mysql --version"}` runs `mysql --version` as the declare-and-check probe (assert the runner saw `mysql`, not `<id>`).
  - An already-installed tool is a no-op (no install call).
  Update existing `clitools_test.go` cases to the new payload/interface shapes.

- [ ] **Step 2: Run the tests, confirm they fail.** `go test ./internal/provider/channels/...`

- [ ] **Step 3: Fix `clitools.go`:**
  - Read `install` from the payload as `map[string]map[string]any`. Add a small helper that also accepts `map[string]any` whose values are `map[string]any` and coerces, so either decode path works.
  - For the first method `pkg.Select` recognises, pass that method's spec (`map[string]any`) to `adapter.IsInstalled` / `adapter.Install`.
  - Declare-and-check fallback: read `check` from the payload (`map[string]any`); if `check["command"]` is a non-empty string, split it on whitespace into binary + args and run that via `env.Runner`; otherwise run `<id> --version` as before.

- [ ] **Step 4: Run the tests, confirm they pass.** `go test ./internal/provider/channels/...`

- [ ] **Step 5: Commit.** `git add internal/provider/channels && git commit -m "Fix cliTools channel: correct install payload type and check-command probe"`

---

### Task 3: Full suite + apply verification

**Files:** none (verification only).

- [ ] **Step 1: Run the full Go suite.** `go test ./...` — all packages pass.

- [ ] **Step 2: Rebuild the binary.** From the worktree root: `go build -o ainfra ./cmd/ainfra`

- [ ] **Step 3: Contained apply against the tvt-config manifest.**
  ```
  mkdir -p /tmp/ainfra-apply-test
  HOME=/tmp/ainfra-apply-test ./ainfra --chdir /Users/michael.hilhorst/projects/tvt-nl/claude-config/.claude/worktrees/ainfra-manifest apply --yes 2>&1 | tail -40
  ```
  Expected: apply gets **past** the cliTools channel. It may still error later, or on a genuinely-missing tool — but NOT with "no supported install method is declared" for `mysql-client` (which declares `brew`). Record exactly how far it gets and any new error.

- [ ] **Step 4: Restore any mutated tracked files** in the claude-config worktree:
  `git -C /Users/michael.hilhorst/projects/tvt-nl/claude-config/.claude/worktrees/ainfra-manifest checkout ainfra.lock` (and any other tracked file apply touched). Confirm with `git -C ... status`. Then `rm -rf /tmp/ainfra-apply-test`.

- [ ] **Step 5: Report** how far `apply` reached and the next blocker, if any. No commit in this task.
