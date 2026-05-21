# Multi-Agent Renderers — Design

Status: approved design, pending implementation plan.
Extends: `docs/design.md` §1, §3, §10. Lands within Phase 3 (channel
provider layer).

## 0. Problem

`ainfra` is Claude Code–specific by name, copy, and channel vocabulary, but
its engine is not. `internal/resolve`, `merge`, `lockfile`, `graph`, `schema`,
`ports`, and `template` contain no agent knowledge — layering, precedence,
content hashing, template instantiation, and the dependency graph are pure
mechanism. The agent-specific knowledge lives only at the edges: the channel
definitions in `internal/manifest`, and the not-yet-built channel provider
layer that will write `.mcp.json`, `.claude/skills/`, and similar.

Phase 3 has not been built in code. That is the cheap window to make the agent
a chooseable axis rather than a hardcoded assumption. The goal: a developer
selects which AI coding agent they use, and `ainfra` reconciles the portable
parts of one shared manifest onto that agent's config.

This design ships two real renderers — Claude Code and Codex — so the
abstraction is exercised against a genuinely different target, not designed in
the abstract.

## 1. Two orthogonal axes

The system separates **what** is configured from **which agent's files are
written**:

- **Channel providers** own channel *semantics*: resolving sources, versions,
  and content hashes; merge rules; dependency-graph edges; what a valid diff
  is. This work is **target-neutral** — an MCP server resolves to the same
  package and hash regardless of agent.
- **Renderers** own agent *I/O*: where an artifact lives on disk, its file
  format, and how to read current on-disk state. One renderer per agent.

Resolution runs once and is target-neutral. Rendering is the deterministic
function `(resolved state, renderer) → artifacts`. That determinism is what
makes the target-neutral lockfile (§4) sound: the lockfile pins inputs; each
renderer derives its agent's output from them.

## 2. Interfaces

### 2.1 ChannelProvider

Phase 3's planned contract, with one addition: `plan`/`apply`/`check` take a
`Renderer`. The provider owns channel semantics; it delegates all agent I/O to
the renderer it is handed.

```
ChannelProvider:
  Resolve(manifest state) -> resolved channel state          // target-neutral
  Plan(resolved state, Renderer) -> diff
  Apply(resolved state, Renderer) -> result
  Check(resolved state, Renderer) -> drift report
```

### 2.2 Renderer

A new plugin, orthogonal to channels. One implementation per agent.

```
Renderer:
  ID() -> string                       // "claude-code" | "codex"
  Capabilities() -> set of channels    // which channels this agent can render

  // per supported channel:
  Locate(channel, entry) -> path       // destination artifact path
  Render(channel, resolved entry) -> desired content
  Observe(channel, entry) -> current on-disk state
```

`Capabilities()` is authoritative. Channel providers never branch on agent
identity; if a renderer does not declare a channel, that channel is not
rendered for that agent, and the gating rule in §3.2 governs whether that is
an error.

## 3. Manifest changes

### 3.1 The `agent` field

A new manifest field naming the target agent.

- A scalar string, allowed in any layer (org/team, repo, personal).
- `agent` is a scalar, so Option-C's `overridable` mechanism — which arbitrates
  id-keyed *map entries* — does not apply to it. Its resolution rule is the
  authority order alone: **the highest-authority layer that declares a
  non-empty `agent` wins** (team, then repo, then personal). A repo that sets
  `agent` standardizes the team on it; a repo that omits it leaves the choice
  to each developer's personal layer.
- Default: `claude-code` when no layer declares one. A manifest that names no
  agent behaves exactly as today.
- The resolved value selects the renderer for `plan`/`apply`/`check`.
- Legal values are the registered renderer IDs (`claude-code`, `codex`). An
  unknown value is a hard validation error listing the registered IDs.

### 3.2 Channel capability gating

Any channel entry may carry an `agents:` list naming the agents it applies to.

Validation rule, evaluated against the resolved `agent`:

- Entry the renderer **can** render → rendered normally.
- Entry the renderer **cannot** render and is **gated away** (`agents:` omits
  the resolved agent) → cleanly skipped, no warning needed; the author has
  stated intent.
- Entry the renderer **cannot** render and is **not gated** → **hard error**
  (`internal/diag` diagnostic with an actionable hint). This upholds the
  design doc's no-silent-drop stance (§13, "Permissive parsing").

An entry gated to agents that *can* all render it is permitted; gating is also
a way to scope an otherwise-portable entry to one agent deliberately.

### 3.3 Naming and the `rules` channel

The agent-selection field is `agent` and the gating field is `agents`,
deliberately distinct from the existing `rules[].target` (which means
"destination filename"). To remove that collision and the Claude-specific
default:

- The `rules` channel destination becomes **renderer-owned**. A rule is
  context content; each renderer places it at its agent's instruction file —
  `CLAUDE.md` for Claude Code, `AGENTS.md` for Codex.
- An explicit `rules[].target` remains allowed as an override for authors who
  need a specific filename.

## 4. Lockfile

`ainfra.lock` structure is **unchanged** and stays **target-neutral**. It pins
inputs — resolved MCP package versions, skill and plugin content hashes,
secret references. Those resolve identically for every agent.

Each renderer derives its agent's artifacts from the same locked inputs,
rendering only its capability subset. `check` recomputes the rendering for its
resolved agent and compares to disk. The committed lockfile remains
byte-identical for every developer regardless of agent choice — preserving the
reproducibility guarantee and keeping personal agent choice out of a committed
file.

## 5. The Codex renderer

Codex consumes a strict subset of the eight channels.

| Channel      | Codex rendering |
|--------------|-----------------|
| `mcpServers` | `~/.codex/config.toml`, `[mcp_servers.<name>]` (command, args, env) |
| `cliTools`   | Identical substrate logic — package-manager adapters, fully agent-agnostic |
| `rules`      | `AGENTS.md` |
| `secrets`    | Environment-variable resolution — agent-agnostic |

Not in the Codex renderer's `Capabilities()`: `skills`, `plugins`, `hooks`,
`builtins`, `commands`. A manifest using those under `agent: codex` must gate
them with `agents:`; an ungated occurrence fails `validate` per §3.2.

The Claude Code renderer covers all eight channels and reproduces today's
intended behaviour.

## 6. CLI surface

No new commands. Behavioural changes:

- `plan` / `apply` / `check` operate against the resolved `agent` and select
  its renderer.
- `plan` output names the active agent and lists any channels skipped by
  capability gating, so a skip is always visible.
- `validate` enforces the §3.1 (unknown agent) and §3.2 (ungated unsupported
  channel) rules.
- `init` scaffolds `agent: claude-code` explicitly in the generated manifest.

## 7. Build order

This work extends Phase 3 and is built in four steps:

1. `Renderer` interface, the `agent` field, capability gating, and the
   validation rules (§3).
2. Claude Code renderer — all eight channels; reproduces intended Phase 3
   behaviour.
3. Codex renderer — the four portable channels (§5).
4. Wire `plan` / `apply` / `check` to resolve the agent, select the renderer,
   and surface gated skips.

## 8. Testing

- **Renderer conformance suite** — a shared contract test every renderer must
  pass: `ID()` is a registered value, `Capabilities()` is non-empty, and every
  declared channel implements `Locate`/`Render`/`Observe`.
- **Golden-file tests** — rendered `.mcp.json` (Claude Code) and
  `~/.codex/config.toml` plus `AGENTS.md` (Codex) compared against committed
  fixtures.
- **Validation tests** — the unknown-`agent` error and the ungated
  unsupported-channel error, each asserting the diagnostic hint.
- **Lockfile invariance** — a manifest resolved under `claude-code` and under
  `codex` produces a byte-identical `ainfra.lock`.

## 9. Non-goals

- No third renderer in this work. Gemini CLI and others are future renderers
  the interface admits but this design does not build.
- No per-agent lockfile, and no per-agent section in the lockfile (§4).
- No `--agent` CLI override flag; the `agent` manifest field under Option-C
  precedence is the single mechanism.
- No attempt to synthesize Claude-only channels (skills, plugins, hooks) onto
  Codex. Unsupported is unsupported; gating makes that explicit.

## 10. Open questions

None. All forks resolved during brainstorming: two-axis architecture, both
renderers fully built, capability-gated channels, `agent` under normal
precedence, target-neutral lockfile.
