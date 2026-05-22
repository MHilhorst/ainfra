# Codex Provider Set — Design

Status: approved design, pending implementation plan.

This is Plan 2b of the agent-aware providers work. Plan 2a
(`2026-05-21-agent-aware-providers-design.md`, merged) built the structural
seam: providers regrouped into `claudecode/` and `shared/`, an `agentset.ForAgent`
constructor, and `plan`/`apply`/`check` resolving the target agent. Plan 2a left
`ForAgent(agent.Codex)` returning an error. This plan builds the Codex provider
set so `agent: codex` reconciles for real.

## 0. Scope

A new `internal/provider/codex/` package with two channel providers — MCP
servers and rules — plus the `agentset` wiring and two new `fsmerge` helpers.
After this plan, a manifest with `agent: codex` reconciles its portable
channels (mcpServers, rules, cliTools) onto Codex's native config.

**Out of scope:** `backgroundServices` gating under `agent: codex` (deferred —
template-produced background services need template-resolution-aware
validation, which is a separate, small follow-up). Codex has no skills,
plugins, hooks, tools, or commands; Plan 1 capability gating already rejects
those channels, ungated, under `agent: codex`.

## 1. Package and wiring

`internal/provider/codex/` (package `codex`) exports two zero-value provider
structs:

- `codex.MCP` — `Channel()` returns `"mcpServers"`; reconciles `~/.codex/config.toml`.
- `codex.Rules` — `Channel()` returns `"rules"`; reconciles `<repo>/AGENTS.md`.

`agentset.ForAgent` gains a real `case agent.Codex`:

```go
case agent.Codex:
    return append([]provider.Provider{
        codex.MCP{},
        codex.Rules{},
    }, sharedProviders()...), nil
```

`sharedProviders()` (the `cliTools` provider) is unchanged and reused. The
`default → error` branch stays for unknown agents.

The `Provider` interface, the orchestrator, and `channelOrder` are untouched.
`channelOrder` is already a superset of all channels; `Orchestrator.sortedChannels`
runs only the providers it is handed, so the Codex set reconciles exactly its
three channels.

## 2. The Codex MCP provider

Codex reads MCP servers from `~/.codex/config.toml` under `[mcp_servers.<id>]`
tables. The provider's file path is `filepath.Join(env.Home, ".codex", "config.toml")`
— note this is a per-user global file, unlike Claude's per-repo `.mcp.json`.

### 2.1 Dependency

A new module dependency: `github.com/BurntSushi/toml` — the standard
modern Go TOML library. `go.mod` currently has only `gopkg.in/yaml.v3`.

### 2.2 The `MergeTOMLTables` helper

A new function in `internal/provider/fsmerge`, the TOML analogue of the
existing `MergeJSONKeys`:

```go
func MergeTOMLTables(fs FS, path, topKey string, desired map[string]any, ownedKeys []string) error
```

It reads the file (a missing file is treated as empty), unmarshals it to
`map[string]any`, ensures the `topKey` table (`mcp_servers`) exists, removes
every key in `ownedKeys` from it, sets every entry from `desired`, and marshals
the whole document back.

This is an **own-the-tables round-trip**: foreign keys — `[model]`, approval
policy, MCP servers ainfra does not own — are preserved as data. Comments and
exact formatting are not preserved; a Codex user's `config.toml` is co-owned by
ainfra, and a write that changes it re-serialises the document. `fsmerge`
remains standalone — it imports neither `provider` nor `lockfile`.

### 2.3 Observe and Apply

- `Observe(env)` reads `config.toml`, treats a missing file as no resources,
  unmarshals it, and returns a `provider.Resource{ID: key, Channel: "mcpServers"}`
  for each key under the `[mcp_servers]` table. `ContentHash` is left empty;
  the orchestrator backfills it from the ledger — the same contract the Claude
  `MCP.Observe` follows.
- `Apply(env, plan)` builds, for each non-noop `Create`/`Update` change, a
  TOML table from the resource payload — `command`, `args`, and `env` (a
  string→string table). The payload's `transport` field is ignored: Codex MCP
  servers are command-launched. `Delete` changes contribute their id to
  `ownedKeys` but not to `desired`, so `MergeTOMLTables` removes them. When
  `env.DryRun` is true the result is computed but the file is not written.

## 3. The Codex rules provider

Codex reads repository instructions from `AGENTS.md` at the repo root. It has
no `@import` mechanism, so the Claude fragment-file pattern cannot apply; and
`AGENTS.md` is frequently user-authored, so ainfra cannot own the whole file.
The provider therefore maintains one delimited, ainfra-managed region.

### 3.1 The managed region

```
<!-- ainfra:begin -->
<!-- ainfra:rule incident-response -->
…rule content…
<!-- ainfra:rule pdf-processing -->
…rule content…
<!-- ainfra:end -->
```

Everything outside `<!-- ainfra:begin -->` … `<!-- ainfra:end -->` is the
user's and is never touched. Inside, each rule is a sub-block introduced by a
`<!-- ainfra:rule <id> -->` marker; the block runs until the next rule marker
or the end marker. The per-rule markers are what let `Observe` report one
`Resource` per rule, so the orchestrator diffs per rule exactly as the Claude
rules provider does.

### 3.2 The `fsmerge` region helper

A new `fsmerge` function reads and rewrites the managed region:

```go
func MergeManagedRegion(fs FS, path string, blocks map[string]string, ownedIDs []string) error
```

It reads the file (missing → empty), locates the region (or the insertion
point — appended after a single blank line when absent), removes every id in
`ownedIDs`, writes every id→content pair from `blocks`, and writes the file
back. When the region would become empty it is removed entirely, including its
markers. User content outside the region is preserved; only the blank-line
separators immediately around the region are normalized.

A companion read helper returns the ids currently present in the region, so
`Observe` does not re-implement the parse.

### 3.3 Observe and Apply

- `Observe(env)` reads `AGENTS.md`, treats a missing file or missing region as
  no resources, and returns a `provider.Resource{ID: ruleID, Channel: "rules"}`
  per rule sub-block.
- `Apply(env, plan)` collects non-noop `Create`/`Update` changes into the
  `blocks` map (keyed by id, value the payload's `content`) and `Delete` changes
  into `ownedIDs`-only, then calls `MergeManagedRegion`. The payload's `target`
  field is ignored — Plan 1 §3.3 makes a rule's destination renderer-owned, and
  for Codex it is always `AGENTS.md`. `DryRun` computes without writing.

## 4. Data flow

Unchanged from Plan 2a. `plan`/`apply`/`check` call `providersForDir(dir)`,
which resolves the agent with `manifest.ResolveAgent` and calls
`agentset.ForAgent`. Under `agent: codex` that now returns the Codex set, and
the orchestrator drives it through the same `Observe → Diff → Apply` loop. The
lockfile, the applied-state ledger, and `RenderResources` are untouched: the
desired `Resource` payloads they produce are agent-neutral, and each provider
renders them into its agent's format.

## 5. Error handling

- A missing `~/.codex/config.toml` or `AGENTS.md` is not an error — `Observe`
  treats it as no resources, and `Apply` creates the file.
- A `config.toml` or `AGENTS.md` that cannot be parsed (malformed TOML, a
  region with a begin marker but no end marker) is a hard error from `Observe`,
  surfaced by the command — ainfra will not blindly overwrite a file it cannot
  read.
- Provider-internal errors propagate through the orchestrator exactly as the
  Claude providers' do; this plan introduces no new orchestrator behaviour.

## 6. Testing

- **`MergeTOMLTables`** — unit tests against the in-memory `Filesystem` fake:
  a fresh file; a file with a foreign `[model]` table and a foreign
  `[mcp_servers.other]` server, both preserved across a merge that
  adds/updates/removes ainfra-owned servers.
- **`MergeManagedRegion`** — unit tests: region created in a file with
  pre-existing user content; per-rule add/update/remove; the region removed
  entirely when its last rule is deleted; user content outside the region
  preserved; a begin-without-end file rejected.
- **`codex.MCP`** — `Observe` on missing/empty/populated `config.toml`;
  `Apply` writing correct `[mcp_servers.<id>]` tables; `DryRun` writes nothing.
- **`codex.Rules`** — `Observe` on missing/region-less/populated `AGENTS.md`;
  `Apply` create/update/delete; `DryRun` writes nothing.
- **`agentset.ForAgent(agent.Codex)`** — returns the three-provider set
  (mcpServers, rules, cliTools); the Plan 2a test that expected an error for
  Codex is updated to expect the set.
- **End to end** — `ainfra plan` on a temp repo with an `agent: codex`
  manifest declaring an MCP server and a rule reconciles both, and a second
  `plan` after `apply` is empty (idempotence).
- `go build ./...` and `go test ./...` pass.

## 7. Non-goals

- No comment preservation in `~/.codex/config.toml` (own-the-tables
  round-trip, decided).
- No `backgroundServices` gating under `agent: codex` (deferred).
- No Codex equivalent for skills, plugins, hooks, tools, or commands — Codex
  has none, and Plan 1 capability gating governs their absence.
- No change to the `Provider` interface, the orchestrator, the lockfile, the
  applied-state ledger, or `RenderResources`.
- No third agent.

## 8. Open questions

None. All forks resolved during brainstorming: own-the-tables TOML
round-trip; `AGENTS.md` managed region with per-rule sub-markers;
`BurntSushi/toml`; `backgroundServices` gating deferred; one plan.
