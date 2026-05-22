# Apply Hardening — Design

**Status:** design
**Date:** 2026-05-22
**Companion plan:** `docs/superpowers/plans/2026-05-22-apply-dry-run-and-sandbox.md` (covers item 5)

## Problem

`ainfra apply` is the one command that mutates a developer's machine, and today
it is the least safe and least observable command in the tool. Two real bugs
recently shipped behind a green `validate`, `lock`, and `plan` because none of
those commands exercise the apply path. This spec collects six hardening items
that make apply fail loudly, fail partially instead of totally, and be testable
without mutating the machine running the test.

The items are independent. This document is the shared spec; each item is
expanded into its own implementation plan (via the `writing-plans` skill) when
it is scheduled. Item 5 is planned and built first because it makes every other
item safer to test.

## Current state (verified 2026-05-22)

What exists, so later items are not specced against fiction:

- **Orchestrator** — `internal/provider/orchestrator.go`. `ApplyAllRendered`
  loops `sortedChannels()` and calls `Provider.Apply`; on the first error it
  `return err` without writing the applied ledger.
- **Providers** — one per channel under `internal/provider/claudecode/` plus
  `internal/provider/shared/clitools.go`. Each `Apply` honours `env.DryRun`
  already (verified: `mcp`, `hooks`, `commands`, `rules`, `tools`, `skills`,
  `plugins`, `services`, `clitools` all gate writes on `if !env.DryRun`).
- **`provider.Env`** — `internal/provider/env.go` — already carries a `DryRun`
  bool. There is no `--dry-run` flag wired to it. `Home` is set from
  `os.UserHomeDir()` in `cmd/ainfra/reconcile.go` `buildEnv` with no override.
- **Lock** — `resolve.RunLock` (`internal/resolve/pipeline.go`) validates the
  manifest, allocates ports, builds the dependency graph, hashes every entry,
  and writes `ainfra.lock` + `ainfra.personal.lock`. `<resolved:...>`
  placeholders for non-port resolved fields are written into the lock
  (`pipeline.go:114`).
- **Secrets** — the manifest schema has `secrets:` (`manifest.Secret`: `mode`,
  `value`, `ref`, `gateway`, `scope`) and entry-level `secret:` maps on
  `MCPServer` and `CLITool`. **There is no secret resolution code at all** —
  `resolve.Scope.Secret` is never populated, no `op://`/`env://` handling
  exists, and `manifest.Validate` does not inspect secret refs.
- **e2e tests** — `cmd/ainfra/e2e_test.go` has `TestE2EReconciliation`
  (hook + command) and `TestE2EToolsChannel` (tools). No fixture covers
  `cliTools` install blocks, a `rules` entry with a non-default target, a
  templated MCP server, or `requires:` edges.

This changes the original review's framing: items 3, 4, and 6 assumed a secret
resolver that does not exist. They are re-scoped below to what is buildable now
(structural validation, better diagnostics, scaffolding) — the resolver itself
is separate, future work.

---

## Item 1 — Apply isolates failures instead of aborting

**Problem.** `ApplyAllRendered` returns on the first channel error, so one bad
`cliTool` aborts the whole apply and the applied ledger is never written —
even for the 38 resources that would have succeeded. Inside a channel it is the
same: `CLITools.Apply` returns on the first failed `Change`
(`clitools.go:63-68`, `clitools.go:84-87`).

**Approach.**
- `ApplyAllRendered` accumulates `[]error` and a `set[string]` of failed
  resource IDs instead of returning on the first error. After the loop it
  writes the applied ledger for everything that succeeded, then returns an
  aggregated error if the slice is non-empty.
- Make it `requires`-aware. `Resource.Requires` already carries node refs
  (`"cli:x"`, `"svc:y"`) and `RunLock` builds the dependency graph. Before
  applying an entry, if any `requires:` edge points at a failed resource, mark
  it **skipped** (a distinct state — not failed, not attempted) and record why.
  This stops a failed `cliTool: ssh` cascading misleading errors into a tunnel
  service that depends on it.
- Per-entry isolation inside a channel: provider `Apply` collects per-`Change`
  failures into `ApplyResult` rather than returning on the first.
- Output: a summary — `applied 38, skipped 2 (blocked by failed deps), failed 1`
  — instead of one opaque error.

**Scope.** Touches `orchestrator.go`, `provider.go` (`ApplyResult` gains a
`Failed`/`Skipped` shape), every provider's `Apply`, and the apply command's
output rendering. The orchestrator change is small; threading
collect-instead-of-return through every provider is the bulk — mechanical.

**Effort.** Medium.

## Item 2 — A representative apply fixture and a render↔channel contract test

**Problem.** Both recently shipped bugs (a `cliTools` payload type assertion and
a `rules` `~`-target handling bug) only manifested in `apply`. `validate`,
`lock`, and `plan` stayed green. `e2e_test.go` exists but its two fixtures are
deliberately minimal.

**Approach.**
- Add a `testdata/` fixture manifest that is deliberately representative: a
  `cliTools` entry with a real `install:` block, a `rules` entry with a
  non-default `target:`, a templated MCP server, and entries with `requires:`
  edges.
- Add an e2e test that runs the real pipeline (`RunLock` → `RenderResources` →
  `ApplyAllRendered`) against it and asserts the produced `.mcp.json`,
  `CLAUDE.md`, and plugin/tool files.
- Highest-value addition: a **render↔channel contract test**. For each channel,
  render a resource via `resolve.RenderResources` and feed it to that channel's
  `Apply`; assert the payload shape the provider reads matches the payload shape
  the renderer produces. A single test class of this kind would have caught the
  `map[string]map[string]any` mismatch immediately.

**Scope.** New `cmd/ainfra/testdata/`, new tests in `cmd/ainfra/` and
`internal/provider/`. No production code change.

**Effort.** Medium — mostly fixture + assertions.

## Item 3 — Lock preflight so a green lock is not false confidence

**Problem.** `RunLock` records refs and content hashes. It does **not** confirm
that each `cliTool`'s declared install method has a registered adapter, and it
writes `<resolved:...>` placeholders into the lock silently. A green lock
therefore says nothing about whether `apply` can actually proceed.

**Approach (buildable now).**
- Add a preflight pass — folded into `ainfra check` or a new `ainfra lock
  --check` — that confirms each `cliTool`'s declared `install:` methods include
  at least one that `pkg.Select` recognises (`brew`, `npm`, `npm-g`), or that
  the binary is already present. An unrecognised-only install block is a
  guaranteed apply failure that should surface at lock time.
- Surface `<resolved:...>` placeholders explicitly in `plan` output as a
  "pending at apply" section instead of letting them sit silently in the lock.
- At minimum, change `lock`'s success message to point forward:
  `Next: run 'ainfra check' to verify the lock can be applied.`

**Deferred.** "Resolve each secret / `git ls-remote` each remote source" from
the original review depends on a secret resolver and a fetch layer for remote
sources that do not exist yet. Out of scope here; revisit when the resolver
lands.

**Effort.** Small–Medium.

## Item 4 — The gitignored personal layer is an onboarding cliff

**Problem.** `manifest.LoadLayers` loads `ainfra.personal.yaml` only if present.
A teammate cloning the repo has no personal layer, and `ainfra init --personal`
scaffolds only a fixed `mcpServers: {}` stub — it does not tell the developer
what their personal layer actually needs to contain.

**Approach (buildable now).**
- Add a cross-layer structural check (shared with item 6): every entry-level
  `secret:` reference must resolve to a `secrets:` key defined in some layer.
  When it does not and no personal layer was loaded, emit a targeted
  diagnostic — *"secret 'X' is not defined; if it is a personal secret, run
  `ainfra init --personal` to scaffold your personal layer."* — instead of a
  bare unknown-reference error.
- Make `ainfra init --personal` useful: have it read the team/repo manifest,
  find every entry `secret:` reference with no definition, and generate
  `ainfra.personal.yaml` with a stub `secrets:` entry per dangling reference
  (`mode: reference`, `scope: personal`, `ref: "REPLACE-ME"` plus a comment).
  That converts the cliff into a fill-in-the-blanks step.

**Deferred.** Anything depending on actually resolving the secret value.

**Effort.** Small.

## Item 5 — A safe way to test apply *(planned — see companion plan)*

**Problem.** `runApply` has no dry-run. Apply runs real `brew install` /
`npm install -g`. There is no way to see what apply *would* do, and no way to
exercise the file-writing channels on a real machine without also mutating
installed packages.

**Approach.**
- `ainfra apply --dry-run` — nearly free: `provider.Env.DryRun` exists and every
  channel honours it (verified). Add the flag, thread `DryRun: true` into the
  apply `Env`, and make the orchestrator skip the ledger write under dry-run.
  Real "what would happen" with zero writes and zero installs.
- `ainfra apply --no-install` — reconcile config files but skip `cliTool`
  installs, so the file-writing channels can be tested on a real machine
  without mutating installed packages. Adds a `NoInstall` field to
  `provider.Env`, honoured by `CLITools.Apply`.

**Dropped from the original review: `--home` / `AINFRA_HOME`.** The review
proposed a `--home` override because "`buildEnv` hardcodes `Home`." Verified
otherwise: `env.Home` is set by `buildEnv` but consumed by **no provider** —
every provider writes under `env.Root`. `env.Root` is already overridable with
the existing global `--chdir` flag (the e2e tests prove apply writes entirely
under `--chdir`). A `--home` flag would wire to dead state. The sandbox story
for apply is `--dry-run` + the existing `--chdir` + `--no-install`. (If a
provider later consumes `env.Home` — e.g. `~`-target expansion for `rules` —
`--home` becomes worth adding then, alongside that work.)

**Scope.** `cmd/ainfra/commands.go`, `internal/provider/env.go`,
`internal/provider/orchestrator.go`, `internal/provider/shared/clitools.go`.
See the companion plan.

**Effort.** Small — `--dry-run` is the big win and almost entirely plumbing.

## Item 6 — Structural validation of secret references

**Problem.** `manifest.Validate` does no checking of `secrets:` entries or
`secret:` ref strings. A typo in a ref string sails through `validate` and
`lock`.

**Approach (buildable now).**
- In `manifest.Validate`, add structural checks on each `secrets:` entry:
  - `mode: reference` requires a non-empty `ref`.
  - `mode: direct` requires `value` and rejects `ref` (and vice versa).
  - When `ref` uses a known scheme — `op://` or `env://` — check its shape:
    `op://<vault>/<item>/<field>` must have three non-empty segments;
    `env://<VAR>` must name a non-empty variable. This catches missing-field
    typos with no network call.
- Add the cross-layer reference check from item 4: every entry `secret:`
  reference resolves to a defined `secrets:` key.
- Keep deep verification (does the `op://` item actually exist) for a future
  `ainfra check` once a resolver exists; make the boundary discoverable in
  `validate` and `lock` output.

**Effort.** Small — a pure-structural check in `validate.go` plus messaging.

---

## Recommended sequencing

1. **Item 5** — `--dry-run` / `--home` / `--no-install`. Cheapest, and makes
   every later item safe to test. *(Planned — built first.)*
2. **Item 1** — failure isolation. Biggest correctness win.
3. **Item 2** — the representative fixture + contract test, to lock in item 1
   and guard against the next render↔channel mismatch.
4. **Items 3, 4, 6** — preflight, onboarding diagnostics, secret-ref
   validation. All small; 4 and 6 share the cross-layer reference check and
   should be planned together.

## Out of scope

A secret-resolution engine (reading `op://`, `env://`, brokered gateways) and a
remote-source fetch layer (`git+`, `npm:`). Several original review items
assumed these exist; they do not. Items 3, 4, and 6 are scoped to structural
validation and scaffolding that stand on their own and will compose cleanly
with a resolver when one is built.
