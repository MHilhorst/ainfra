# Phase 3 — Plan 3: Filesystem-Channel Providers Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development. Steps use `- [ ]`.

**Goal:** Implement the four filesystem-channel providers — MCP servers, hooks, commands, rules — so the orchestrator can reconcile `.mcp.json`, `.claude/settings.json`, `.claude/commands/`, and `CLAUDE.md` plus rule fragments.

**Architecture:** Each provider implements `Observe` (report which managed resources are physically present) and `Apply` (render a `Change`'s payload onto disk via `fsmerge`). `Observe` leaves `ContentHash` empty; the orchestrator backfills it from the applied-state ledger so content currency is judged by lockfile-hash vs ledger-hash. A `Change` now carries the target `Resource` so `Apply` has the payload to render.

**Tech Stack:** Go, `gopkg.in/yaml.v3`, `encoding/json`, standard `testing`.

---

### Task 1: Carry the target Resource on a Change; orchestrator hash backfill

**Files:** Modify `internal/provider/provider.go`, `internal/provider/diff.go`, `internal/provider/orchestrator.go`; update affected tests.

- [ ] **Step 1 — failing test.** In `diff_test.go`, extend `TestDiffResources` (or add a test) to assert that a Create change carries `Change.Resource` equal to the desired resource, and a Delete change carries the prior resource.

- [ ] **Step 2 — run, see fail.**

- [ ] **Step 3 — implement.**
  - In `provider.go`, add a field to `Change`: `Resource Resource` — for `ChangeCreate`/`ChangeUpdate`/`ChangeNoop` it is the desired resource; for `ChangeDelete` it is the prior resource.
  - In `diff.go` `DiffResources`, populate `Change.Resource` at every emit site (desired resource for create/update/noop; prior resource for delete).
  - In `orchestrator.go` `PlanAll`, after calling `p.Observe`, backfill: for each observed resource whose `ContentHash == ""`, set it to the prior ledger entry's hash for that ID if one exists. This makes a presence-only `Observe` fall back to manifest-hash comparison; a provider that computes a real artifact hash keeps content-drift detection.

- [ ] **Step 4 — run, see pass.** `go test ./internal/provider/...` green.
- [ ] **Step 5 — commit** `Carry target resource on changes and backfill observed hashes`.

---

### Task 2: MCP server provider

**Files:** Create `internal/provider/channels/mcp.go`, `internal/provider/channels/mcp_test.go`.

The MCP provider manages server entries inside `<root>/.mcp.json` under the top-level `mcpServers` key. It owns only the keys it created.

`Resource.Payload` keys for an MCP server: `command` (string), `args` ([]any), `env` (map[string]any), `transport` (string). These render to the `.mcp.json` server object.

- [ ] **Step 1 — failing tests** (`mcp_test.go`, package `channels`), using `provider.NewMemFilesystem()`:
  - `Channel()` returns `"mcpServers"`.
  - `Observe`: given a `.mcp.json` containing servers `a` and `foreign`, returns a resource for every server key present (IDs `a`, `foreign`).
  - `Apply` of a Create change writes the server object under `mcpServers` in `.mcp.json`, preserving any pre-existing foreign key.
  - `Apply` of a Delete change removes only that server key.
  - `Apply` respects `env.DryRun` — with `DryRun: true`, the file is not modified.

- [ ] **Step 2 — run, see fail.**

- [ ] **Step 3 — implement `mcp.go`.** Package `channels`. Type `MCP struct{}`. `Channel() string` returns `"mcpServers"`. `mcpPath(env) string` = `filepath.Join(env.Root, ".mcp.json")`.
  - `Observe(env provider.Env) ([]provider.Resource, error)`: read `.mcp.json` via `env.FS.ReadFile`; missing file → no resources, no error; otherwise `json.Unmarshal`, and for each key under `mcpServers` return a `provider.Resource{ID: key, Channel: "mcpServers"}` (leave `ContentHash` empty).
  - `Apply(env provider.Env, plan provider.ChannelPlan) (provider.ApplyResult, error)`: if `env.DryRun`, return the result describing the changes without writing. Otherwise: collect the owned keys (every change's ID) and the desired map (for create/update, build the server object from `Change.Resource.Payload`), then call `fsmerge.MergeJSONKeys(env.FS, mcpPath, "mcpServers", desired, ownedKeys)`. Delete changes contribute their ID to `ownedKeys` but not to `desired`, so the merge removes them. Return `provider.ApplyResult{Channel: "mcpServers", Applied: <non-noop changes>}`.
  - The server object: `map[string]any{"command": payload["command"], "args": payload["args"], "env": payload["env"]}` plus `"type": payload["transport"]` when transport is non-empty. Omit nil/empty fields.

- [ ] **Step 4 — run, see pass.**
- [ ] **Step 5 — commit** `Add the MCP server channel provider`.

---

### Task 3: Hooks provider

**Files:** Create `internal/provider/channels/hooks.go`, `hooks_test.go`.

Hooks live in `<root>/.claude/settings.json` under the top-level `hooks` key. `ainfra` owns one nested object per managed hook id.

`Resource.Payload` keys for a hook: `event` (string), `matcher` (string), `command` (string), `timeout` (number, optional).

- [ ] **Step 1 — failing tests:** `Channel()` is `"hooks"`; `Observe` returns ids present under `hooks` in `settings.json`; `Apply` Create merges a hook object under `hooks` preserving foreign hook ids; Delete removes only the owned id; `DryRun` writes nothing.

- [ ] **Step 2 — run, see fail.**

- [ ] **Step 3 — implement `hooks.go`.** Package `channels`. Type `Hooks struct{}`. `Channel()` = `"hooks"`. Path = `filepath.Join(env.Root, ".claude", "settings.json")`. `Observe` reads `settings.json` (missing → none), returns a resource per key under `hooks`. `Apply` uses `fsmerge.MergeJSONKeys(env.FS, path, "hooks", desired, ownedKeys)`. The hook object built from payload: `map[string]any{"event": ..., "matcher": ..., "command": ...}` plus `"timeout"` when present and non-zero; omit empty `matcher`. Respect `env.DryRun`.

- [ ] **Step 4 — run, see pass.**
- [ ] **Step 5 — commit** `Add the hooks channel provider`.

---

### Task 4: Commands provider

**Files:** Create `internal/provider/channels/commands.go`, `commands_test.go`.

Each command is a standalone markdown file `<root>/.claude/commands/<id>.md` that `ainfra` fully owns.

`Resource.Payload` keys for a command: `content` (string — the markdown body to write). (Where `content` comes from — reading the source file — is the command-layer's job in Plan 6; the provider just writes what it is given.)

- [ ] **Step 1 — failing tests:** `Channel()` is `"commands"`; `Observe` returns an id for every `<root>/.claude/commands/*.md` file present (use the filename without `.md` as the id); `Apply` Create writes `.claude/commands/<id>.md` with the payload content; Delete removes that file; `Apply` of a Create when the file already exists overwrites it; `DryRun` writes nothing; observing a directory with no commands dir present returns no resources and no error.

- [ ] **Step 2 — run, see fail.**

- [ ] **Step 3 — implement `commands.go`.** Package `channels`. Type `Commands struct{}`. `Channel()` = `"commands"`. Commands dir = `filepath.Join(env.Root, ".claude", "commands")`. `Observe`: list the dir — since `provider.Filesystem` has no `ReadDir`, instead derive presence by attempting `env.FS.Stat` on `<dir>/<id>.md` per **desired** id is not possible here (Observe has no desired). Therefore: add a `ReadDir(path string) ([]string, error)` method to the `provider.Filesystem` interface and implement it on `OSFilesystem` (via `os.ReadDir`) and `MemFilesystem` (track files, return names with the dir prefix stripped). `Observe` then lists `.md` files in the commands dir. `Apply`: Create/Update → `fsmerge.WriteOwnedFile(env.FS, <dir>/<id>.md, []byte(content))`; Delete → `env.FS.Remove`. Respect `env.DryRun`.

  NOTE: extending `provider.Filesystem` with `ReadDir` also requires updating `OSFilesystem`, `MemFilesystem`, and the `fsmerge.FS` interface is unaffected (it does not need `ReadDir`). Update `fakes.go` so `MemFilesystem.ReadDir` returns the base names of files whose path has the given dir as parent.

- [ ] **Step 4 — run, see pass** (`go test ./internal/provider/...`).
- [ ] **Step 5 — commit** `Add the commands channel provider`.

---

### Task 5: Rules provider

**Files:** Create `internal/provider/channels/rules.go`, `rules_test.go`.

A rule's content lands in an `ainfra`-owned fragment file `<root>/.claude/ainfra/<id>.md`, and the rule's `target` file (e.g. `<root>/CLAUDE.md`) gets a single `@`-import line pointing at the fragment (relative path `.claude/ainfra/<id>.md`).

`Resource.Payload` keys for a rule: `target` (string — e.g. `CLAUDE.md`), `content` (string — the fragment body).

- [ ] **Step 1 — failing tests:** `Channel()` is `"rules"`; `Apply` Create writes the fragment file `.claude/ainfra/<id>.md` AND ensures an `@.claude/ainfra/<id>.md` import line in the target file; a second Apply is idempotent (import line not duplicated); Delete removes the fragment file (leaving the import line removal as a documented limitation — see below); `Observe` returns an id for each fragment file present under `.claude/ainfra/`; `DryRun` writes nothing.

- [ ] **Step 2 — run, see fail.**

- [ ] **Step 3 — implement `rules.go`.** Package `channels`. Type `Rules struct{}`. `Channel()` = `"rules"`. Fragment dir = `filepath.Join(env.Root, ".claude", "ainfra")`. `Observe` lists `.md` files there (uses the `ReadDir` added in Task 4). `Apply`: for Create/Update — `fsmerge.WriteOwnedFile` the fragment, then `fsmerge.EnsureImportLine(env.FS, filepath.Join(env.Root, target), ".claude/ainfra/<id>.md")`. For Delete — `env.FS.Remove` the fragment file. Respect `env.DryRun`.

  DOCUMENTED LIMITATION: Delete removes the fragment but does not strip the `@import` line from the target file (that needs a line-removal helper). Add a one-line comment in `rules.go` noting this; a follow-up plan can add `fsmerge.RemoveImportLine`.

- [ ] **Step 4 — run, see pass.**
- [ ] **Step 5 — commit** `Add the rules channel provider`.

---

### Task 6: Verification

- [ ] **Step 1:** `go build ./... && go test ./...` — all green.
- [ ] **Step 2:** `go vet ./internal/provider/...` — clean.
- [ ] **Step 3:** Confirm `internal/provider/channels` imports `internal/provider` and `internal/provider/fsmerge` only — no cycle.
- [ ] **Step 4:** commit any vet fix; otherwise nothing to commit.

---

## Self-Review

**Spec coverage:** the four filesystem-channel providers of spec §3.4 (mcpServers, hooks, commands, rules) — Tasks 2-5. `Change.Resource` threading + orchestrator hash backfill — Task 1. `fsmerge` (Plan 2) and the `Provider` interface (Plan 2) are reused; `ReadDir` is added to `Filesystem` in Task 4.

**Type consistency:** `MCP`, `Hooks`, `Commands`, `Rules` all in package `channels`, each a zero-field struct implementing `provider.Provider`. `Change.Resource` added in Task 1, consumed by every provider's `Apply`.

**Out of scope (later plans):** skills + plugins providers (Plan 4); cliTools, preconditions, background services (Plan 5); the `plan`/`apply`/`check` commands, manifest-to-Resource rendering, and end-to-end tests (Plan 6).
