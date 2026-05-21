# Phase 3 — Channel Provider Layer + Schema Completion

Status: approved design, ready for implementation planning.

This spec makes `ainfra plan`, `ainfra apply`, and `ainfra check` real. It
covers two things the project's status table conflates: the manifest schema is
only implemented for 4 of its 8 channels, and the channel provider layer that
drives reconciliation does not exist. Both are built here, in one spec, split
into the implementation plans listed in §9.

## 0. Background and the gap

`docs/design.md` §1 names eight channels. `internal/manifest/types.go` defines
Go types for only four — `mcpServers`, `cliTools`, `hooks`, `commands` — plus
the cross-cutting `backgroundServices`, `secrets`, `templates`, and
`preconditions`. The other four channels (skills, plugins, rules, tools) are
specified in `spec/manifest-schema.md` §10 but were never built into the Go
types, the validator, or the resolve/lock pipeline.

Separately, `plan`, `apply`, and `check` are registered as stub commands
(`cmd/ainfra/commands.go`, `newPendingCommand`) that print a notice and exit 1.

So "Phase 3" as scoped here is: **finish the manifest schema for the four
missing channels (unfinished Phase 1 / Phase 4), then build the channel
provider layer and the reconciliation commands (Phase 3 proper).**

## 1. Locked decisions

These were decided during brainstorming and are not open:

1. **Scope** — complete the schema for skills/plugins/rules/tools, then build
   all eight channel providers and the `plan`/`apply`/`check` orchestration.
2. **Desired state is the lockfile.** `plan`/`apply`/`check` consume
   `ainfra.lock` (and `ainfra.personal.lock`). They require `ainfra lock` to
   have been run; they warn — not fail — when the lockfile is stale relative to
   the manifest.
3. **Secrets** — `direct` mode with a literal `value` is applied fully. `direct`
   with a `ref` and `brokered` mode are *checked* (the target env var must be
   present / the gateway reachable) and fail with an actionable error if not.
   ainfra does not shell out to 1Password/Doppler/Vault/SOPS in this increment;
   the pluggable secret resolver is a later increment. This honours design §5's
   non-goal: ainfra never stores or resolves secret values it is not given.
4. **Ownership** — managed-region merge is the primitive; separate
   fully-owned files are used wherever Claude Code's config format allows it;
   the lockfile is the ownership ledger (§5).

## 2. Part A — Schema completion

### 2.1 Manifest types

Add to `internal/manifest/types.go`, following the existing field conventions
(common fields `Requires`, `Enabled *bool`, `Overridable bool`):

```go
// Manifest gains:
Skills  map[string]Skill  `yaml:"skills"`
Plugins map[string]Plugin `yaml:"plugins"`
Rules   map[string]Rule   `yaml:"rules"`
Tools   *Tools            `yaml:"tools"`
```

- `Skill` — `Source string`, `Version string`, common fields. `Source` accepts
  the same schemes as `extends` (§1 of the manifest spec): a local path,
  `git+https://…@<ref>[#subdir]`, or `npm:<pkg>@<version>`.
- `Plugin` — `Source string`, `Version string`, common fields. `Source` is an
  `npm:` ref or a marketplace ref.
- `Rule` — `Target string` (where the file lands, e.g. `CLAUDE.md`),
  `Source string`, `Version string`, common fields.
- `Tools` — a singleton, not a map: `Builtins struct { Disabled []string }`
  and `Permissions struct { Allow []string; Deny []string }`. The manifest key
  is `tools:` (matching `spec/manifest-schema.md` §10).

### 2.2 Validation

Extend `internal/manifest/validate.go`:

- Skills/plugins/rules with a package-launched or remote `source` must pin an
  exact `version` — same rule, same rationale as MCP servers §5.1 (a floating
  ref defeats drift detection). A local-path source needs no version.
- `Rule.Target` must be non-empty.
- `Tools.Builtins.Disabled` entries and `Tools.Permissions` entries are
  non-empty strings; no further semantic validation (the strings are opaque to
  ainfra — Claude Code interprets them).
- Layer precedence (Option C) applies to the new channels exactly as it does to
  the existing ones — no special-casing.

### 2.3 Resolution into the lockfile

Extend `RunLock` in `internal/resolve/pipeline.go`. The four new channels are
non-templated, so they resolve the same way `hooks` and `commands` already do:
hash the declared config, record an `Entry`, add `requires` edges to the graph.

- `lockfile.Entries` gains `Skills`, `Plugins`, `Rules` maps and a `Tools`
  entry (singleton).
- Also resolve **inline (non-templated) MCP servers** — `RunLock` currently
  `continue`s past any `mcpServers` entry with no `template`, leaving them
  unlocked. They must be hashed and recorded like any other entry.
- `splitByLayer` and `portsFromLock` extend to the new entry maps.

### 2.4 Two pipeline fixes this spec depends on

- **`ManifestHash`** — the `Lock` struct has the field; `RunLock` never sets
  it. Populate it (hash of the merged, resolved manifest input). The staleness
  warning in §6 reads it.
- **Per-entry `requires`** — `RunLock` builds the dependency graph from each
  entry's `requires:` edges, then discards it. Persist those edges *on each
  lock entry* as `Entry.Requires []string` (node-refs: `cli:node`,
  `svc:tunnel`, `pre:internet`). The orchestrator rebuilds and topo-sorts the
  graph from the merged locks at `plan`/`apply`/`check` time. A single global
  `Lock.ApplyOrder` was rejected: it would write personal-layer node-refs into
  the committed `ainfra.lock`, which `splitByLayer` exists to prevent. Each
  lock stays self-describing — its own entries and their edges only.

## 3. Part B — The provider layer

New package `internal/provider`.

### 3.1 The Provider interface

```go
type Provider interface {
    Channel() string                                              // "mcpServers", "skills", …
    Observe(env Env) ([]Resource, error)                          // read machine state
    Diff(desired, observed, prior []Resource) (ChannelPlan, error) // PURE — no I/O
    Apply(env Env, plan ChannelPlan) (ApplyResult, error)         // mutate the machine
}
```

- `Resource` is one channel entry in a provider-neutral shape: an `ID`, a
  `Layer`, a `ContentHash`, and the channel-specific payload. The three sets
  `Diff` consumes — desired, observed, prior — are all `[]Resource`, so a
  channel's desired lock entries and its current machine state are expressed in
  one comparable shape.
- `Observe` reads the filesystem and runs probe commands to build the observed
  `[]Resource` — the current machine state for that channel only. The `tools`
  channel is a singleton; it is expressed as a slice of length 0 or 1.
- `Diff` is **pure**: it takes the three resource sets and returns a
  `ChannelPlan` — an ordered list of typed `Change`s, each tagged `Create`,
  `Update`, `Delete`, or `NoOp`, carrying a human-readable description and the
  data `Apply` needs. No I/O, so it is exhaustively unit-testable.
- `Apply` executes a `ChannelPlan`'s non-`NoOp` changes and returns an
  `ApplyResult` (what changed, any per-change error).

`check` is `Observe` + `Diff`; drift is a non-empty plan. `plan` is the same,
rendered. `apply` is the same, then `Apply`.

### 3.2 Env — injected dependencies

```go
type Env struct {
    FS     Filesystem      // read/write/stat/remove; abstracts the real disk
    Runner CommandRunner   // run external commands (installs, probes)
    Home   string          // resolved Claude Code config root
    DryRun bool
}
```

`Filesystem` and `CommandRunner` are interfaces. Production uses real
implementations; tests use an in-memory `Filesystem` fake and a recording
`CommandRunner` fake. No provider touches `os` or `exec` directly — this keeps
`Observe`/`Apply` unit-testable and makes `DryRun` enforceable in one place.

### 3.3 Shared fsmerge helpers

Written once in `internal/provider/fsmerge`, reused by multiple providers:

- `MergeJSONKeys(file, ownedKeys, desired)` — for `.mcp.json` and
  `.claude/settings.json`. Reads the file, replaces only the keys ainfra owns,
  removes owned keys that are no longer desired, leaves every other key (and,
  as far as practical, formatting) untouched.
- `WriteOwnedFile(path, content)` — for skills and commands: files ainfra
  created and fully owns.
- `EnsureImportLine(claudeMdPath, importPath)` — for rules: ensures a single
  `@<importPath>` line exists in `CLAUDE.md`, idempotently.

### 3.4 The eight providers

| # | Channel | Target | `Apply` behaviour |
|---|---------|--------|-------------------|
| 1 | mcpServers | `.mcp.json` (project), user-scope config (personal) | `MergeJSONKeys` under `mcpServers`. `direct`+literal env written; `ref`/`brokered` checked only (§4). |
| 2 | skills | `.claude/skills/<id>/` | Fetch the pinned source, `WriteOwnedFile` the contents; fully owned dir. |
| 3 | plugins | Claude Code plugin install | Install/remove the pinned plugin via the package manager. |
| 4 | rules | `Target` file + an owned fragment | Write the owned fragment file, `EnsureImportLine` into `CLAUDE.md`. |
| 5 | tools | `.claude/settings.json` | `MergeJSONKeys` for the disabled-builtins and permission keys ainfra owns. |
| 6 | cliTools | the system | Package-manager adapter (§3.5). |
| 7 | hooks | `.claude/settings.json` | `MergeJSONKeys` under `hooks`; install any `source` script via `WriteOwnedFile`. |
| 8 | commands | `.claude/commands/<id>.md` | Fetch the pinned source, `WriteOwnedFile`; fully owned. |

Background services and preconditions are cross-cutting, not channels, but are
reconciled by the same orchestrator (§6):

- **Background services** — `Apply` *generates* the start/stop scripts and the
  Claude Code hook wiring that launches the service; it never starts,
  supervises, or restarts the daemon (design §7). `Observe` checks the
  generated definition is present and current.
- **Preconditions** — verify-only. `Observe` runs the declared check; `Diff`
  reports a failed precondition as a blocking change; there is no `Apply`. A
  failed precondition fails the run loudly with the declared `remediation`.

### 3.5 CLI tooling adapters

`cliTools` `Apply` delegates to a package-manager adapter:

```go
type PackageAdapter interface {
    Name() string
    IsInstalled(env Env, tool CLITool) (bool, error)
    InstalledVersion(env Env, tool CLITool) (string, error)
    Install(env Env, tool CLITool) error
}
```

This increment ships the `brew` and `npm -g` adapters (the two that matter on
the team's macOS machines) and a **declare-and-check fallback** for every other
declared install method: ainfra checks whether the tool is present and, if not,
fails with the actionable manual-install instruction from the manifest. Adapter
coverage for `apt`/`uv`/`cargo`/direct-download is a later increment. CLI
reproducibility stays best-effort, exactly as design §6 states.

## 4. Secrets handling at apply time

Per §1.3:

- `secret.mode: direct` with a literal `value` — written into the channel
  entry's `env` (or equivalent) by `Apply`.
- `secret.mode: direct` with a `ref` — `Apply` does not resolve the ref. It
  verifies the referenced environment variable is set in `Env`; if not, it
  fails with an actionable error naming the variable and the `ref` so the
  developer knows what to populate and from where.
- `secret.mode: brokered` — `Apply` verifies the named `gateway` is configured
  and reachable; it never holds a token.

No secret value is ever written to the lockfile, to `.ainfra/`, or to logs.

## 5. The ownership ledger

`apply` must be able to *remove* exactly the entries ainfra previously created —
and nothing a developer added by hand. Knowing what to remove requires a record
of what ainfra last wrote *on this machine*.

The lockfile cannot serve this alone: when an entry is deleted from the
manifest it also leaves the lockfile, so the lockfile no longer knows the
entry ever existed. The solution is Terraform-style local applied-state:

- After a successful `apply`, ainfra snapshots the resolved lock to
  `.ainfra/applied.lock`. `.ainfra/` is already in `.gitignore`; the snapshot
  is per-machine and never committed.
- `Diff` receives three sets: `desired` (current `ainfra.lock`), `prior`
  (`.ainfra/applied.lock`, empty on first run), `observed` (the machine scan).
- Resolution rules:
  - in `prior`, not in `desired` → **Delete** (ainfra owned it, no longer wanted)
  - in `desired`, differs from `observed` → **Create** / **Update**
  - in `desired`, matches `observed` → **NoOp**
  - in `observed`, in neither `prior` nor `desired` → the developer's; **untouched**

This is what makes the "separate files + managed-region merge" ownership model
safe: ainfra deletes only keys and files it has a recorded claim on.

## 6. Part C — Orchestration

`internal/provider/orchestrator.go` drives all providers; the three commands
are thin wrappers over it. Each command:

1. Loads `ainfra.lock` and `ainfra.personal.lock`. If `ainfra.lock` is missing,
   exits with an actionable error: run `ainfra lock` first.
2. Recomputes the manifest hash and compares it to the lock's `ManifestHash`.
   On mismatch, prints a **warning** (not an error): the lockfile is stale, run
   `ainfra lock`. The command still proceeds against the lockfile as written.
3. Loads `.ainfra/applied.lock` (empty on first run).
4. Rebuilds the dependency graph from the merged locks' per-entry `requires`
   node-refs and topo-sorts it, so CLI tools and preconditions are
   observed/applied before the channels that `require:` them. Walks providers
   in that order; for each: `Observe`, then `Diff`.

Then they diverge:

- **`plan`** — renders the aggregated `+`/`~`/`-` diff via `internal/ui` (which
  already carries plan-diff rendering), changes nothing, exits 0.
- **`apply`** — renders the plan; if empty, prints "nothing to do" and exits 0;
  otherwise confirms via `internal/ui` `confirm` (a `--yes` flag skips the
  prompt), runs each provider's `Apply` in `ApplyOrder`, stops on the first
  error and reports it, and on full success writes the new
  `.ainfra/applied.lock`.
- **`check`** — renders any drift and exits non-zero when drift exists, so it
  works as a CI gate; exits 0 when the machine matches the lockfile.

`cmd/ainfra/commands.go` loses `newPlanCommand`/`newApplyCommand`/
`newCheckCommand`'s `newPendingCommand` bodies; `newPendingCommand` itself is
deleted once no command uses it.

## 7. Error handling

- Missing `ainfra.lock` → actionable error, exit 1 (§6.1).
- Stale lockfile → warning, proceed (§6.2).
- A failed precondition → blocking error before any `Apply` runs; the run stops
  and prints the declared `remediation`.
- A missing secret (`ref`/`brokered`) → actionable error naming the variable
  or gateway; the affected provider's `Apply` does not run.
- An adapter cannot install a CLI tool → declare-and-check fallback error with
  the manual-install instruction.
- `apply` stops on the first provider error; already-applied providers stay
  applied (no rollback in v1 — design §2 defers Govern/rollback). The new
  `.ainfra/applied.lock` is written only on full success, so a partial apply
  leaves the ledger pointing at the last fully-consistent state, and a re-run
  re-plans from there.

## 8. Testing

- **`Diff`** — pure; table-driven unit tests per provider covering create,
  update, delete, noop, and the ledger rules of §5.
- **`Observe` / `Apply`** — tested against the in-memory `Filesystem` fake and
  the recording `CommandRunner` fake; assert files written and commands run.
- **Orchestrator** — integration tests with fake providers covering apply
  order, the stop-on-first-error path, and the applied-state write.
- **End-to-end** — `ainfra --chdir examples/multi-database plan|apply|check`
  against a temp HOME and a temp `.ainfra/`; assert the example reconciles and
  a second `plan` is empty (idempotence).
- `go build ./...` and `go test ./...` pass; the existing suites for
  `manifest`, `resolve`, and `lockfile` are extended for the new channels.

## 9. Implementation-plan split

This spec is large; `writing-plans` decomposes it into these plans, built in
order:

1. **Schema completion** — manifest types, validation, resolution into the
   lockfile, the new `Entries` maps, `ManifestHash`, per-entry `requires`,
   inline-MCP resolution. Planned in
   `docs/superpowers/plans/2026-05-21-phase-3-schema-completion.md`.
2. **Provider foundation** — the `Provider` interface, `Env`, `Filesystem` /
   `CommandRunner` and their fakes, `fsmerge`, the orchestrator skeleton, and
   the applied-state ledger.
3. **Filesystem-channel providers** — mcpServers, hooks, commands, rules.
4. **Fetch/install providers** — skills, plugins.
5. **CLI tools + cross-cutting** — `cliTools` with the `brew` and `npm -g`
   adapters, preconditions, background-service generation.
6. **Command wiring** — replace the `plan`/`apply`/`check` stubs, delete
   `newPendingCommand`, add the end-to-end tests; update `README.md` and
   `docs/quickstart.md` so the status table and the "not yet built" notes
   reflect reality.

## 10. Non-goals

Unchanged from design §9, and reaffirmed for this increment specifically: no
secret-value resolution (references are checked, not resolved); no rollback or
Govern workflow on a failed `apply`; no daemon supervision of background
services; no `apt`/`uv`/`cargo`/direct-download adapters yet.
