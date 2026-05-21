# Phase 3 — Plan 6: Command Wiring (plan / apply / check)

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development. Steps use `- [ ]`.

**Goal:** Make `ainfra plan`, `ainfra apply`, and `ainfra check` real — replace the pending stubs with the orchestrator-driven reconciliation, render the `+`/`~`/`-` plan diff, gate `apply` on preconditions and confirmation, and add end-to-end tests.

**Architecture:** A renderer turns the resolved manifest into desired `provider.Resource`s with full payloads. The command layer builds an `Orchestrator` over all nine providers with a real `Env`, loads the lockfile, runs `PlanAll`/`ApplyAll`, and renders results. `plan` changes nothing; `apply` checks preconditions, confirms, applies; `check` exits non-zero on drift.

**Tech Stack:** Go, standard `testing`.

---

### Task 1: Render the manifest into desired resources

**Files:** Create `internal/resolve/render.go`, `render_test.go`.

`RenderResources` resolves the manifest and produces, per channel, the desired `provider.Resource`s WITH `Payload` populated so providers can render artifacts. It reuses the existing resolve pipeline. The `resolve` package may import `internal/provider` (no cycle: `provider` does not import `resolve`).

- [ ] **Step 1 — failing test:** a temp manifest with an inline mcpServer, a hook, a command (local `source` file), and a rule (local `source` file) — `RenderResources(dir)` returns a `map[string][]provider.Resource` whose `mcpServers` resource carries `Payload["command"]`, whose `commands` resource carries `Payload["content"]` equal to the source file's bytes, and whose `rules` resource carries `Payload["target"]` and `Payload["content"]`.

- [ ] **Step 2 — run, see fail.**

- [ ] **Step 3 — implement `render.go`.** `func RenderResources(dir string) (map[string][]provider.Resource, error)`:
  - Load layers (`manifest.LoadLayers`), validate (`manifest.ValidateAll`).
  - Run the lock pipeline in-memory enough to get content hashes — simplest: call `RunLock(dir)` is a side-effecting write; instead, factor the per-channel resolution so render can compute the same `ContentHash` values. Acceptable shortcut for this task: call `RunLock(dir)` then `lockfile.Read` the resulting `ainfra.lock`/`ainfra.personal.lock` to obtain each entry's `ContentHash`, `Layer`, `Requires`; then build the `Payload` separately from the manifest (below). Document that render relies on a current lockfile.
  - For each channel, build `provider.Resource{ID, Channel, Layer, ContentHash, Requires, Payload}`:
    - `mcpServers`: templated entries via `Instantiate` (reuse existing code), inline entries direct — `Payload`: `command`, `args`, `env`, `transport`.
    - `hooks`: `Payload`: `event`, `matcher`, `command`, `timeout`.
    - `commands`: `Payload`: `content` = bytes of `<dir>/<source>` (read the local file; a non-local source → `Payload["content"]` empty with a recorded note).
    - `rules`: `Payload`: `target`, `content` = bytes of `<dir>/<source>`.
    - `skills`, `plugins`: `Payload`: `source`, `version`.
    - `tools`: `Payload`: `disabled`, `allow`, `deny`.
    - `cliTools`: `Payload`: `install`, `check`.
    - `backgroundServices`: `Payload`: `kind`, `spec`.
  - Return the map.

- [ ] **Step 4 — run, see pass.**
- [ ] **Step 5 — commit** `Render the manifest into desired provider resources`.

---

### Task 2: Provider registry and Env construction

**Files:** Create `cmd/ainfra/reconcile.go`, `reconcile_test.go`.

- [ ] **Step 1 — failing test:** `allProviders()` returns nine providers whose `Channel()` values are exactly the nine channel names; `buildEnv(dir)` returns a `provider.Env` with a non-nil `FS`, `Runner`, `Fetch`, and `Root == dir`.

- [ ] **Step 2 — run, see fail.**

- [ ] **Step 3 — implement `reconcile.go`** in package `main`:
  - `allProviders() []provider.Provider` — returns `channels.MCP{}`, `channels.Hooks{}`, `channels.Commands{}`, `channels.Rules{}`, `channels.Skills{}`, `channels.Plugins{}`, `channels.CLITools{}`, `channels.Services{}`, `channels.Tools{}` — note: a `Tools` provider does not exist yet. ADD a minimal `internal/provider/channels/tools.go`: `Tools` provider, `Channel()` = `"tools"`, `Observe` reads `.claude/settings.json` and returns one resource with ID = the layer keys present under a `permissions`/`disabledTools` block (or simply: ID `"tools"` when the file has the managed keys), `Apply` merges `permissions` (`allow`/`deny`) and `disabledTools` into `.claude/settings.json` via `fsmerge.MergeJSONKeys` for each managed key. Keep it simple and consistent with the hooks provider; honor `DryRun`. Write `tools_test.go`.
  - `buildEnv(dir string) provider.Env` — `provider.Env{FS: provider.OSFilesystem{}, Runner: provider.ExecRunner{}, Fetch: fetch.LocalFetcher{Root: dir}, Root: dir, Home: <user home>}`. Get the home dir via `os.UserHomeDir()` (ignore the error, fall back to `""`).

- [ ] **Step 4 — run, see pass.**
- [ ] **Step 5 — commit** `Add provider registry, Env construction, and the tools provider`.

---

### Task 3: Plan diff rendering

**Files:** Create `internal/ui/plan.go`, `plan_test.go` (or extend existing `ui` files).

- [ ] **Step 1 — failing test:** `RenderPlan(w, colorizer, plans)` given a `map[string]provider.ChannelPlan` writes a line per non-noop change prefixed with the change symbol (`+`/`~`/`-`) and the channel/id, and a summary line `N to add, M to change, K to destroy`; an all-empty plan writes `No changes. Environment matches the lockfile.`

- [ ] **Step 2 — run, see fail.**

- [ ] **Step 3 — implement.** `RenderPlan(w io.Writer, c *ui.Colorizer, plans map[string]provider.ChannelPlan)` — iterate channels in sorted order, then changes; print `  <symbol> <channel>.<id>  <detail>` for each non-noop change, color `+` green, `~` yellow, `-` red (reuse the existing `Colorizer`). End with the summary counts. `internal/ui` may import `internal/provider` (no cycle).

- [ ] **Step 4 — run, see pass.**
- [ ] **Step 5 — commit** `Add plan diff rendering`.

---

### Task 4: The plan command

**Files:** Modify `cmd/ainfra/commands.go`; create `cmd/ainfra/cmd_plan_test.go`.

- [ ] **Step 1 — failing test:** running `plan` in a temp repo with an `ainfra.yaml` + `ainfra.lock` writes a plan to stdout and exits 0; running it with NO `ainfra.lock` exits non-zero with an actionable "run ainfra lock first" message.

- [ ] **Step 2 — run, see fail.**

- [ ] **Step 3 — implement.** Replace `newPlanCommand`'s body. It must: resolve the working dir from the command context (read `cmd/ainfra/main.go` and `internal/cli` to find how `--chdir` is exposed); error if `ainfra.lock` is absent ("run `ainfra lock` first"); load `ainfra.lock` + `ainfra.personal.lock` (`lockfile.Read`); recompute the manifest hash and print a staleness warning if it differs from the lock's `ManifestHash`; build the orchestrator (`provider.NewOrchestrator(dir, buildEnv(dir), allProviders())`); call `PlanAll(mergedLock)`; render via `ui.RenderPlan`; exit 0. Provide a helper `mergeLocks(committed, personal *lockfile.Lock) *lockfile.Lock` that unions the entry maps.

- [ ] **Step 4 — run, see pass.**
- [ ] **Step 5 — commit** `Wire the plan command to the orchestrator`.

---

### Task 5: The apply command

**Files:** Modify `cmd/ainfra/commands.go`; create `cmd/ainfra/cmd_apply_test.go`.

- [ ] **Step 1 — failing test:** `apply` with `--yes` in a temp repo reconciles the environment (assert an expected file is written) and exits 0; a second `apply --yes` reports no changes; `apply` writes `.ainfra/applied.lock`.

- [ ] **Step 2 — run, see fail.**

- [ ] **Step 3 — implement.** Replace `newApplyCommand`'s body. Add a `--yes` bool flag. It must: load locks as `plan` does; build the orchestrator; run `PlanAll` and render it; if the plan is empty, print "Nothing to do." and exit 0; check preconditions (load them from the manifest layers, build `[]precond.Precondition`, run `precond.CheckAll` — on any failure print each failure's id + remediation and exit non-zero BEFORE applying); if not `--yes`, prompt for confirmation via the existing `internal/ui` confirm helper and abort on "no"; call `ApplyAll(mergedLock)`; on error print it and exit non-zero; on success print a summary and exit 0.

- [ ] **Step 4 — run, see pass.**
- [ ] **Step 5 — commit** `Wire the apply command with preconditions and confirmation`.

---

### Task 6: The check command

**Files:** Modify `cmd/ainfra/commands.go`; create `cmd/ainfra/cmd_check_test.go`.

- [ ] **Step 1 — failing test:** `check` in a repo whose environment matches the lockfile exits 0; after deleting an applied artifact, `check` exits non-zero and reports the drift.

- [ ] **Step 2 — run, see fail.**

- [ ] **Step 3 — implement.** Replace `newCheckCommand`'s body. Load locks; build the orchestrator; run `PlanAll`; render the plan; if every channel plan is `Empty()` print "No drift." and exit 0; otherwise render the drift and exit 1. `check` never mutates. Then DELETE the now-unused `newPendingCommand` helper.

- [ ] **Step 4 — run, see pass.**
- [ ] **Step 5 — commit** `Wire the check command and remove the pending-command stub`.

---

### Task 7: End-to-end test and documentation

**Files:** Create `cmd/ainfra/e2e_test.go`; modify `README.md`, `docs/quickstart.md`.

- [ ] **Step 1:** write `e2e_test.go` — in a temp dir, write an `ainfra.yaml` exercising a hook and a command (local source), run `init`-free: `lock`, then `plan` (asserts changes shown), then `apply --yes` (asserts artifacts written under `.claude/`), then `check` (asserts exit 0, no drift), then a second `plan` (asserts empty). Drive the commands through the same entry point `main_test.go` uses.

- [ ] **Step 2:** run `go test ./...` — all green.

- [ ] **Step 3:** update `README.md` and `docs/quickstart.md` — change the status table so Phase 3 is `done`, and remove the "plan/apply/check are specified but not yet built" notices, replacing them with the real behaviour.

- [ ] **Step 4:** `go build ./... && go test ./...` — green.
- [ ] **Step 5 — commit** `Add end-to-end reconciliation test and update docs for Phase 3`.

---

## Self-Review

**Spec coverage:** the `plan`/`apply`/`check` orchestration (spec §6) — Tasks 4-6; manifest rendering — Task 1; plan diff rendering (`ui`) — Task 3; precondition gating + confirmation (spec §6, §7) — Task 5; the ninth (`tools`) provider — Task 2; end-to-end verification + doc truth — Task 7.

**Type consistency:** `RenderResources` (resolve), `allProviders`/`buildEnv`/`mergeLocks` (cmd/ainfra), `RenderPlan` (ui), `Tools` provider (channels). The orchestrator, providers, `precond`, and `lockfile` from Plans 1-5 are reused unchanged.

**Out of scope (documented follow-ups):** remote (git/npm) fetching; the pluggable secret resolver; gateway adapters; `apt`/`uv`/`cargo` package adapters; the Govern workflow.
