# Agent-Aware Providers — Design

Status: approved design, pending implementation plans.

Reconciles the multi-agent renderers design
(`2026-05-21-multi-agent-renderers-design.md`) with the channel provider layer
that shipped in Phase 3 (`internal/provider/`). It **supersedes §2 of that
document** — the separate `Renderer` interface it proposed is dropped.

## 0. Problem

Two designs were developed on parallel branches and now both sit on `main`:

- **Multi-agent renderers** (merged as "Agent selection and capability
  gating", PR #8) added the `agent` manifest field, the `agents:` gating list,
  the `internal/agent` capability registry, and validation. Its §2 proposed
  that channel providers stay agent-neutral and `plan`/`apply`/`check` take a
  separate `Renderer` object that owns agent-specific paths and formats.
- **Phase 3 channel providers** (merged) built `internal/provider/`: a
  `Provider` interface, the orchestrator, shared diff, an applied-state ledger,
  and eight channel providers — each of which writes Claude Code artifacts
  directly (`channels/mcp.go` → `.mcp.json`, `channels/rules.go` → `CLAUDE.md`,
  and so on).

The conflict: Phase 3's `Provider` is **agent-blind**, and there is no
`Renderer`. But each Phase 3 provider already *is* a Claude-Code-specific
renderer — it bundles channel semantics, agent paths, agent formats, and I/O.
The multi-agent design's separate-`Renderer` layer is therefore redundant.

This design reconciles them so a developer can choose their agent (`agent:
claude-code` or `agent: codex`) and `plan`/`apply`/`check` reconcile onto that
agent's native config.

## 1. The reconciled architecture

**A `Provider` is the per-agent renderer for one channel.** The `Provider`
interface ships unchanged from Phase 3:

```go
type Provider interface {
    Channel() string
    Observe(env Env) ([]Resource, error)
    Apply(env Env, plan ChannelPlan) (ApplyResult, error)
}
```

Agent-awareness is introduced in three places, none of which alter that
interface:

1. **The `plan`/`apply`/`check` commands** resolve the target agent with
   `manifest.ResolveAgent` (already on `main` from PR #8) and construct that
   agent's provider set.
2. **A set constructor**, `provider.ForAgent(id agent.ID, env Env) ([]Provider,
   error)`, assembles the providers for a given agent.
3. **The orchestrator** already accepts the provider set as a constructor
   parameter (`NewOrchestrator(root, env, ps []Provider)`) and runs only the
   providers it is handed — so it needs no structural change. `channelOrder`
   stays a superset of all channels; a set with fewer providers simply
   reconciles fewer channels.

The separate `Renderer` interface from the multi-agent design §2 is **not
built**. The `Provider` fills that role.

## 2. Package structure

The Phase 3 providers in `internal/provider/channels/` split by whether they
are agent-specific:

- **`internal/provider/claudecode/`** — the agent-specific Claude Code
  providers: `mcpServers`, `skills`, `plugins`, `rules`, `tools`, `hooks`,
  `commands`, and `backgroundServices` (it generates Claude Code hook wiring).
  Their code moves essentially unchanged — they already are the Claude Code
  renderers.
- **`internal/provider/shared/`** — providers that are genuinely
  agent-agnostic and reused by every agent set: `cliTools` (installing a binary
  is the same regardless of agent) and `preconditions` (verify-only).
- **`internal/provider/codex/`** — the Codex provider set, built in Plan 2b:
  `mcpServers` → `~/.codex/config.toml`, `rules` → `AGENTS.md`.

`provider.ForAgent` composes a set from the agent-specific package plus the
shared providers:

- `ForAgent(agent.ClaudeCode, env)` → claudecode providers + shared providers.
- `ForAgent(agent.Codex, env)` → codex providers + shared providers.
- An unknown agent id is an error (it should never reach here — validation
  rejects it first — but `ForAgent` fails loudly rather than returning an empty
  set).

`Env.Home`'s doc comment changes from "Claude Code config root" to "user home
directory"; each provider derives its own agent's subpath (`.claude/` vs
`.codex/`) from it.

## 3. Data flow

Unchanged from Phase 3 except for the agent-resolution step:

1. The command loads `ainfra.lock` / `ainfra.personal.lock`.
2. The command resolves the agent: `manifest.ResolveAgent(layers)` →
   `agent.ID`.
3. The command builds the provider set: `provider.ForAgent(id, env)`.
4. `NewOrchestrator(root, env, set)` — then `PlanAll` / `ApplyAll` as today.

The orchestrator, diff, applied-state ledger, and `Resource`/`Change`/
`ChannelPlan` types are untouched.

## 4. Capability gating and the lockfile

`ainfra lock` runs `ValidateAll`, which already rejects (PR #8) any ungated
channel the resolved agent cannot render. So by the time `plan`/`apply`/`check`
read the lockfile, it is agent-consistent: it contains no ungated channel the
agent lacks a provider for.

One residual case: an entry explicitly gated `agents: [claude-code]` is still
written to the lockfile by `RunLock` (resolution does not filter on `agents:`).
Under `agent: codex`, the Codex provider set has no provider for that entry's
channel, so the orchestrator does not reconcile it. That is the correct
outcome — the author scoped the entry away from Codex. No lockfile filtering is
added; the provider set is the filter.

## 5. The Codex provider set (Plan 2b)

- **`mcpServers`** → `~/.codex/config.toml`, table `[mcp_servers.<id>]`
  (command, args, env). Codex's MCP config is TOML, so Plan 2b introduces a
  TOML dependency and a Codex-specific merge that replaces only ainfra-owned
  tables while preserving every user-authored key — the TOML analogue of
  `fsmerge.MergeJSONKeys`.
- **`rules`** → `AGENTS.md` at the repo root, the cross-vendor instruction
  file Codex reads.
- **Shared** — `cliTools` and `preconditions` are reused from
  `internal/provider/shared/`, not reimplemented.
- Codex has no provider for `skills`, `plugins`, `tools`, `hooks`,
  `commands`, or `backgroundServices`; `agent.Supports` already encodes this,
  and capability gating (§4) keeps such entries out of an ungated Codex
  manifest.

## 6. Plan split

- **Plan 2a — the structural seam.** Move the eight providers into
  `claudecode/` and `shared/`; add `provider.ForAgent`; wire the
  `plan`/`apply`/`check` commands to resolve the agent and build the set.
  **Zero behavior change** — `agent` defaults to `claude-code`, so an existing
  manifest reconciles exactly as before, against the same providers, now reached
  through `ForAgent`. A pure, low-risk refactor.
- **Plan 2b — the Codex set.** Add `internal/provider/codex/` with the
  TOML-writing `mcpServers` provider and the `AGENTS.md` `rules` provider; add
  the TOML dependency and the Codex key-preserving merge; `ForAgent(codex)`
  returns the Codex set plus the shared providers.

Each plan is independently shippable.

## 7. Error handling

- **Unknown agent reaching `ForAgent`** — returns an error naming the agent.
  Validation should have caught it at `lock` time; `ForAgent` is the
  defence-in-depth backstop, never a silent empty set.
- **A lockfile entry whose channel the agent set does not cover** — not an
  error; the orchestrator reconciles only the channels its set provides (§4).
- Provider-level errors (`Observe`/`Apply` failures, missing secrets, failed
  preconditions) are handled exactly as Phase 3 already specifies — this design
  changes neither the orchestrator nor the providers' internal error paths.

## 8. Testing

- **Plan 2a** — the existing `internal/provider` and channel-provider test
  suites pass unchanged after the move (the providers' behavior is identical).
  New tests: `ForAgent` returns the expected provider set per agent id and
  errors on an unknown id; `plan` on an `agent: claude-code` manifest produces
  the same result as before the refactor.
- **Plan 2b** — the Codex providers are tested against the in-memory
  `Filesystem` fake: `config.toml` is written correctly, and the merge
  preserves user-authored TOML keys while replacing ainfra-owned tables;
  `AGENTS.md` is written and observed. An end-to-end `plan` on an `agent:
  codex` manifest reconciles the portable channels and is idempotent on a
  second run.
- `go build ./...` and `go test ./...` pass for both plans.

## 9. Non-goals

- No separate `Renderer` interface (this design supersedes that idea).
- No third agent. The structure admits more agent sets, but only `claude-code`
  and `codex` are built.
- No change to the `Provider` interface, the orchestrator's diff/apply logic,
  the applied-state ledger, or the `Resource`/`Change`/`ChannelPlan` types.
- No Codex equivalents for Claude-only channels (skills, plugins, tools, hooks,
  commands); capability gating already governs their absence.
- No new manifest fields — `agent` and `agents:` from PR #8 are sufficient.

## 10. Open questions

None. All forks resolved during brainstorming: `Provider` is the per-agent
renderer (no separate `Renderer`); per-agent provider packages plus a shared
package; `ForAgent` as the set constructor; the structural seam (2a) and the
Codex set (2b) as two independently shippable plans.
