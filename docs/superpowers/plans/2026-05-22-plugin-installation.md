---
title: "feat: Plugin installation via the claude CLI (sub-project #3)"
type: feat
status: active
created: 2026-05-22
depth: standard
---

# feat: Plugin installation via the `claude` CLI (sub-project #3)

> **For agentic workers:** execute unit-by-unit in U-ID order; each unit is an atomic-ish change with its own tests. Build and test from the worktree root, not the main checkout.

## Problem frame

`ainfra`'s `plugins` channel only *records* the desired plugin set into
`.claude/ainfra/plugins.json` — an ainfra-private file Claude Code never reads.
Plugins are therefore declared but never installed. The complete-test against
the tvt-config manifest confirmed this: `apply` writes `plugins.json`, but no
plugin is actually installed.

Claude Code installs plugins from **marketplaces** via its own CLI:
`claude plugin marketplace add <source>` registers a marketplace, then
`claude plugin install <name>@<marketplace>` installs a plugin from it. This
plan makes `ainfra` delegate to that CLI — owning *declaration and
reconciliation*, while the `claude` CLI owns the runtime mechanics — exactly as
`cliTools` delegates installs to `brew`/`npm`.

## Scope

In scope: a new `marketplaces` channel, a reworked `plugins` channel, the
manifest/lockfile/render changes a new channel requires, orchestrator wiring,
and updating the tvt-config `ainfra.yaml` to the corrected model.

### Deferred to Follow-Up Work

- **Externally-sourced standalone skills** (the `skills:` channel). Repos like
  [`vercel-labs/skills`](https://github.com/vercel-labs/skills) are *not* Claude
  Code marketplaces — they are agent-agnostic skill collections installed via a
  separate `npx skills` CLI. They belong to ainfra's `skills:` channel, not this
  one. A future sub-project should handle them by delegating to the `skills`
  CLI, mirroring the delegate-to-native-CLI pattern this plan establishes for
  `claude plugin`. This plan deliberately does not touch the `skills` channel —
  the tvt-config skills ride bundled inside the `tvt-config` plugin and arrive
  when that plugin installs.
- **Plugin `enable`/`disable`** (`claude plugin disable`) — install/uninstall is
  the reconciliation surface here.

## Key technical decisions

1. **Delegate to the `claude` CLI; never reimplement Claude Code's plugin
   manager.** Writing `installed_plugins.json` / cache directories directly
   would couple ainfra to Claude Code internals. The `claude plugin` subcommand
   surface (`marketplace add/remove`, `install`, `uninstall`, `update`, `list`)
   is the contract.
2. **Marketplaces are a first-class channel**, not a property folded into
   plugin entries. Registration and install are distinct reconcilable units;
   `channelOrder` places `marketplaces` before `plugins` so registration always
   precedes install.
3. **Full lifecycle, ledger-scoped.** A plugin removed from the manifest is
   uninstalled — but only if ainfra installed it (the diff emits a Delete only
   for entries in ainfra's applied ledger). A hand-installed plugin is never
   touched.
4. **Best-effort `update`.** `claude plugin` versioning is fuzzy (many plugins
   report version `unknown`). When a manifest `version` pin differs from the
   installed version, the channel runs `claude plugin update`; it does not fail
   `apply` over a version it cannot reconcile.
5. **The `claude` CLI is a `precondition`**, not a `cliTool` ainfra installs —
   ainfra runs alongside Claude Code; it verifies `claude` is on `PATH` and
   errors clearly if not.

## Manifest schema

New top-level block:
```yaml
marketplaces:
  <name>:
    source: <github:owner/repo | https URL | local path>
```
`<name>` must match the `name` field in that marketplace's `marketplace.json`
(Claude derives the registered name from the source).

`plugins` entries gain a `marketplace` field and drop the `git+` `source`
model:
```yaml
plugins:
  <plugin-name>:
    marketplace: <name>      # references a declared marketplace
    version: "<pin>"         # optional
```

## High-level technical design

Adding `marketplaces` follows the established "new channel" surface. *Directional —
not implementation specification.*

```
manifest (marketplaces:, plugins[].marketplace)
   -> RunLock        -> lockfile.Entries.Marketplaces  (+ splitByLayer routing)
   -> RenderResources -> result["marketplaces"], reworked result["plugins"]
   -> Orchestrator (channelOrder: ... marketplaces, plugins ...)
        -> Marketplaces.Observe(known_marketplaces.json) / Apply(claude plugin marketplace add|remove)
        -> Plugins.Observe(installed_plugins.json)        / Apply(claude plugin install|uninstall|update)
```

---

## Implementation units

### U1. Manifest schema: `marketplaces` block and plugin `marketplace` field

**Goal:** The manifest can declare marketplaces and reference them from plugins.

**Files:**
- Modify: `internal/manifest/types.go`
- Modify: `internal/manifest/validate.go`
- Modify: `internal/manifest/validate_test.go`
- Modify: `internal/manifest/load.go` (if layer-merge enumerates channels)

**Approach:**
- Add `Marketplace struct { Source string \`yaml:"source"\` }` and
  `Manifest.Marketplaces map[string]Marketplace \`yaml:"marketplaces"\``.
- Add `Marketplace string \`yaml:"marketplace"\`` to the `Plugin` struct.
- `Validate`: a marketplace with an empty `source` is an error. A plugin must
  name a `marketplace`, and that marketplace must be declared in the (merged)
  manifest — replace the current "plugin declares no source" / "remote plugin
  must pin version" checks, which no longer apply.
- Verify layer-merging picks up the new `marketplaces` map (mirror how
  `mcpServers` etc. merge across layers).

**Patterns to follow:** the existing `mcpServers`/`hooks` validation blocks in
`validate.go`; the `Plugin`/`Skill` struct shape in `types.go`.

**Test scenarios:**
- A manifest with a valid `marketplaces` entry and a plugin referencing it
  passes `Validate`.
- A marketplace with an empty/missing `source` fails with a clear diagnostic.
- A plugin whose `marketplace` names an undeclared marketplace fails with a
  diagnostic naming the plugin and the missing marketplace.
- A plugin with no `marketplace` field fails validation.
- Marketplaces declared in different layers all merge into the validated set.

**Verification:** `go test ./internal/manifest/...` passes; a manifest using the
new schema validates.

---

### U2. Lockfile and lock pipeline: marketplace entries

**Goal:** `ainfra lock` resolves marketplaces into the lockfile.

**Files:**
- Modify: `internal/lockfile/types.go`
- Modify: `internal/resolve/pipeline.go`
- Modify: `internal/resolve/pipeline_test.go`

**Approach:**
- Add `Marketplaces map[string]Entry \`yaml:"marketplaces"\`` to
  `lockfile.Entries`.
- In `RunLock` (`pipeline.go`): in the per-layer loop, resolve each
  `m.Marketplaces` entry into `lock.Entries.Marketplaces[name]` with
  `Layer`, and `ContentHash` over `{source}`. Initialize the map in the
  `lockfile.Lock` literal and in `splitByLayer`'s `mk()`, and add a `route(...)`
  call for `Marketplaces` in `splitByLayer`.
- Plugin entries already lock; ensure their `ContentHash` covers the new
  `marketplace` field (and `version`).

**Patterns to follow:** how `CLITools`/`Plugins` entries are built in the
second layer loop of `pipeline.go`; the `mk()` + `route()` pattern in
`splitByLayer`.

**Test scenarios:**
- `lock` of a manifest with marketplaces writes `marketplaces` entries with a
  content hash and layer.
- A marketplace declared in the personal layer routes to the personal lock; a
  team-layer one routes to the committed lock.
- Changing a marketplace `source` changes its content hash.
- A plugin's content hash changes when its `marketplace` changes.

**Verification:** `go test ./internal/resolve/... ./internal/lockfile/...`
passes; `ainfra lock` on a marketplace-bearing manifest produces
`marketplaces:` lock entries.

---

### U3. RenderResources: marketplaces resources and reworked plugins payload

**Goal:** `RenderResources` emits `marketplaces` resources and a `plugins`
payload carrying `marketplace` instead of `git+` source.

**Files:**
- Modify: `internal/resolve/render.go`
- Modify: `internal/resolve/render_test.go`
- Modify: `internal/resolve/render.go` `mergedEntries` (add a `marketplaces`
  entry map)

**Approach:**
- Add a `marketplaces` channel block to `RenderResources`, mirroring the
  existing per-channel blocks: iterate `m.Marketplaces`, emit
  `provider.Resource{Channel: "marketplaces", Payload: {"source": ...}}` with
  `ContentHash`/`Layer`/`Requires` from the merged lock.
- Rework the `plugins` block: payload becomes
  `{"marketplace": p.Marketplace, "version": p.Version}` (drop `source`).
- Extend `mergedEntries` and `mergeLockEntries` with a `marketplaces` map.

**Patterns to follow:** the `cliTools` and `plugins` blocks already in
`RenderResources`.

**Test scenarios:**
- `RenderResources` on a marketplace-bearing manifest returns a `marketplaces`
  channel with one resource per declared marketplace, payload carrying `source`.
- The `plugins` resources carry `marketplace` and `version` in their payload,
  not `source`.
- A marketplace resource's `ContentHash` is populated from the lock.

**Verification:** `go test ./internal/resolve/...` passes.

---

### U4. `Marketplaces` channel provider

**Goal:** a provider that registers/reconciles marketplaces via the `claude`
CLI.

**Files:**
- Create: `internal/provider/claudecode/marketplaces.go`
- Create: `internal/provider/claudecode/marketplaces_test.go`

**Approach:**
- `Marketplaces` struct implementing `provider.Provider`; `Channel()` returns
  `"marketplaces"`.
- `Observe`: read `~/.claude/plugins/known_marketplaces.json` (under `env.Home`);
  return a `provider.Resource` per registered marketplace keyed by its name.
  A missing file means no resources.
- `Apply`: for Create — `claude plugin marketplace add <source>` via
  `env.Runner`; treat "already exists"/"already added" stderr as success. For
  Delete (ledger-scoped) — `claude plugin marketplace remove <name>`. Honor
  `env.DryRun`.
- Payload key consumed: `"source"`.

**Patterns to follow:** `internal/provider/shared/clitools.go` (delegates to an
external CLI, idempotent, honors `DryRun`); `plugins.go` `Observe` (reads a
JSON state file).

**Test scenarios:**
- `Observe` with a populated `known_marketplaces.json` returns a resource per
  marketplace; a missing file returns none.
- `Apply` Create runs `claude plugin marketplace add <source>` (assert via
  `FakeRunner`).
- `Apply` Create when the marketplace is already registered (stderr says so) is
  a no-op success, not an error.
- `Apply` Delete runs `claude plugin marketplace remove <name>`.
- `DryRun` Apply issues no runner calls.

**Verification:** `go test ./internal/provider/claudecode/...` passes.

---

### U5. Rework the `Plugins` channel provider

**Goal:** the `plugins` channel installs/updates/uninstalls plugins via the
`claude` CLI instead of writing `plugins.json`.

**Files:**
- Modify: `internal/provider/claudecode/plugins.go`
- Modify: `internal/provider/claudecode/plugins_test.go`

**Approach:**
- `Observe`: read `~/.claude/plugins/installed_plugins.json` (under `env.Home`);
  return a resource per installed plugin. The file keys plugins as
  `name@marketplace`; the resource `ID` is the bare `name` so it matches the
  manifest plugin key. ContentHash left empty — orchestrator backfills from the
  ledger.
- `Apply`: Create — `claude plugin install <id>@<marketplace>` (marketplace from
  payload); "already installed" stderr is success. Update — when a `version` is
  pinned and differs, `claude plugin update <id>@<marketplace>` (best-effort:
  log/skip on failure, do not abort). Delete — `claude plugin uninstall <id>`.
  Honor `env.DryRun`.
- Remove the `.claude/ainfra/plugins.json` writing and its `pluginsPath` helper.
- Payload keys consumed: `"marketplace"`, `"version"`.

**Patterns to follow:** `clitools.go` Apply (CLI delegation, idempotency,
ledger-scoped delete); the just-rewritten `hooks.go` `Observe` for the
ledger-backed shape if `installed_plugins.json` cannot be parsed to bare names.

**Test scenarios:**
- `Observe` parses `installed_plugins.json` and returns one resource per
  installed plugin, `ID` = bare plugin name.
- `Apply` Create runs `claude plugin install name@marketplace`.
- `Apply` Create when already installed is a no-op success.
- `Apply` Update with a differing pinned version runs `claude plugin update`;
  an update failure does not abort the channel.
- `Apply` Delete runs `claude plugin uninstall name`.
- `DryRun` Apply issues no runner calls and writes no files.
- The legacy `.claude/ainfra/plugins.json` is no longer created.

**Verification:** `go test ./internal/provider/claudecode/...` passes.

---

### U6. Orchestrator wiring

**Goal:** the `marketplaces` channel is registered and ordered before
`plugins`.

**Files:**
- Modify: `internal/provider/orchestrator.go` (`channelOrder`)
- Modify: `cmd/ainfra/reconcile.go` (`allProviders`)
- Modify: `cmd/ainfra/reconcile_test.go` if it asserts the provider set

**Approach:**
- `channelOrder`: insert `"marketplaces"` immediately before `"plugins"`.
- `allProviders()`: add `claudecode.Marketplaces{}` to the returned slice.

**Patterns to follow:** the existing `channelOrder` slice and `allProviders`
list.

**Test scenarios:**
- `Test expectation: none -- wiring only; covered by U2/U4/U5 unit tests and the
  U7 end-to-end apply verification.`

**Verification:** `go build ./...` and `go test ./...` pass.

---

### U7. Update the tvt-config manifest and verify end to end

**Goal:** the real tvt-config `ainfra.yaml` uses the marketplace model, and a
contained `apply` actually installs the plugins.

**Files:**
- Modify: `ainfra.yaml` in the `claude-config` repo (`trein-vertraging/claude-config`)
- Modify: gap report `docs/assessment-vs-real-config.md` (mark #3 closed)

**Approach:**
- Add a `marketplaces:` block: `trein-vertraging` →
  `source: github:trein-vertraging/claude-config` (or the local repo path for a
  developer working from a clone).
- Rewrite the `plugins:` block: each of the 5 plugins (`tvt-config`,
  `claude-ads`, `expo`, `compound-engineering`, `higgsfield`) becomes
  `{ marketplace: trein-vertraging }`, dropping the `git+` source and the
  `0.0.0-main` version placeholders.
- Add a `vpn`-style `precondition` (or reuse preconditions) declaring the
  `claude` CLI must be on `PATH`.
- Run a contained `apply` (isolated `HOME`) and confirm `claude plugin install`
  is invoked per plugin and `installed_plugins.json` reflects them; confirm
  `check` after apply reports zero plugin/marketplace drift.

**Execution note:** verification runs against the live `claude` CLI; if network
or marketplace access fails, record the failure rather than masking it.

**Test scenarios:**
- `Test expectation: none -- manifest data + integration verification; the
  behavior is covered by U4/U5 unit tests.`

**Verification:** `ainfra validate` passes on the updated manifest; a contained
`apply` registers the marketplace and installs the 5 plugins; `check` after
apply shows no marketplace/plugin drift.

---

## System-wide impact

- **Lockfile schema** gains a `marketplaces` section — an additive change;
  existing locks without it still read.
- **`channelOrder`** gains a channel — `plan`/`apply`/`check` output now lists
  marketplaces.
- The legacy `.claude/ainfra/plugins.json` is removed; nothing else consumed it.

## Risks

- **`claude` CLI surface drift** — the plan pins to `marketplace add/remove`,
  `install`, `uninstall`, `update`. If those change, the two channels break;
  mitigated by the channels surfacing the raw `claude` error.
- **Same plugin name across two marketplaces** — the plugin resource `ID` is the
  bare name, so two marketplaces offering the same plugin name would collide.
  Out of scope to solve; documented limitation (the tvt case uses one
  marketplace).
- **`marketplace add` network dependency** — registering a GitHub-sourced
  marketplace clones it; `apply` depends on network. Acceptable — same as any
  install step.
