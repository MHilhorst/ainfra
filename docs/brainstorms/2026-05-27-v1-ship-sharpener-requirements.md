# v1 Ship Sharpener — Requirements

**Date:** 2026-05-27
**Status:** Brainstorm output — ready for `/ce-plan`
**Inputs:**
- `docs/problem-space.md` (evidence from real teams)
- `docs/design-philosophy-references.md` (npm + Terraform principles)
- `docs/reference/design.md` (current ainfra design)
- `ainfra.yaml`, README "Status" section

## 0. Frame

ainfra's current design already implements most of the npm/Terraform principles cleanly. Cross-referencing the design against the evidence surfaced a small set of specific, named gaps. This document captures the four chosen to land before v1 is declared shipped. Each is small enough that planning can produce a concrete spec; together they close the gap between "designed" and "feels finished to a team adopting cold."

Explicitly deferred for separate work: a Cursor renderer (v1.5), Sigstore signature verification (v2), persona-drift hardening (evidence thin), and path-scoped render targets (design §15 already defers this).

## 1. `ainfra adopt` — brownfield import

### Problem
Today `ainfra init` scaffolds a blank `ainfra.yaml`. Every real adopting team already has an existing `.mcp.json`, `.claude/` bundle, hooks, slash commands, and `CLAUDE.md`. They have to hand-translate this to ainfra's schema. `problem-space.md` §2 ranks onboarding pain second only to supply-chain rug-pulls; the philosophy review (`design-philosophy-references.md`) names brownfield support as an explicit Terraform provider design rule.

### What ships
A new command, `ainfra adopt`, that reads a repo's existing AI-tooling config and writes a draft `ainfra.yaml` plus a first `ainfra.lock`.

Sources to read:
- `.mcp.json` (project) and `~/.config/claude/claude_desktop_config.json` (user, opt-in)
- `.claude/settings.json`, `.claude/settings.local.json`
- `.claude/hooks/`, `.claude/commands/`, `.claude/skills/`
- `CLAUDE.md`, `AGENTS.md`
- For Codex target: the equivalent Codex config locations

The output is a starting point, not a one-shot migration. The draft should be runnable through `ainfra validate` and `ainfra lock` without manual edits in the common case.

### Success criteria
- A team with a non-trivial existing Claude Code setup can run `ainfra adopt && ainfra validate && ainfra lock && ainfra plan` and see zero changes in `plan`.
- Literal credentials found in any source file are **never** copied to the manifest. They are stripped and replaced with a `direct` secret reference placeholder; adopt prints a one-line warning per stripped secret naming the env var the user must wire to a real reference.
- Adopting twice is safe: re-running adopt against an already-adopted repo merges new entries without destroying user edits, or refuses with a clear "manifest exists — use `--merge` or `--force`" message.
- Adopt is the first command in the new README quick-start above `init`. `init` remains for greenfield.

### Open questions (resolve in `/ce-plan`)
- Q1.1 Channel coverage scope for v1 — minimum viable is MCP servers + hooks + commands + CLAUDE.md; skills (which currently arrive via `git clone` per design §1) may be a no-op for adopt. Plugins and tools/permissions: include or defer?
- Q1.2 Layer splitting — does adopt produce a single `ainfra.yaml` (repo layer only) or attempt to split team-vs-personal heuristically? Recommend: repo-only in v1, leave personal layer as a user follow-up.
- Q1.3 Output formatting — emit canonical-formatted YAML with the same channel order as `ainfra.yaml` showcase? Recommend yes.

## 2. Tool-description hashing — lockfile catches description drift

### Problem
`problem-space.md` §3 documents the single sharpest exploit class against MCP-using teams: a server's *tool descriptions* changing without a version bump. Descriptions are model-visible prompts; changing them can hijack agent behavior. ainfra's current lockfile hashes the resolved package version, not the runtime tool description blob. That gap means a same-version server with a poisoned description passes `ainfra check` cleanly today.

### What ships
At `ainfra lock` time, for every MCP server entry, ainfra starts the server in a constrained subprocess, calls `tools/list`, and stores a content hash of the tool list (name + description + input schema per tool) in `ainfra.lock`. `ainfra check` re-fetches and compares; mismatch is a drift event.

This converts the lockfile from a reproducibility control into a reproducibility *and* integrity control. It is the killer differentiator the evidence is loudest about and the smallest concrete change in the four.

### Success criteria
- `ainfra.lock` includes a `toolsHash` (or equivalent) field per MCP server entry that has been successfully introspected.
- `ainfra check` exits non-zero when an MCP server's `tools/list` output hashes differently than locked.
- Servers that require credentials to start are handled with one of: (a) graceful skip with explicit `toolsHash: unverified` lockfile marker, (b) opt-in flag to provide read-only lock-time credentials. Decide which in `/ce-plan`.
- Lock-time MCP server execution runs with no network egress beyond what the server itself initiates, no access to the user's shell environment beyond what's declared in the manifest, and a wall-clock timeout (default 15s).
- Per-tool granularity: the lockfile records each tool's hash separately so `check` can report *which* tool changed, not just "tools changed."

### Open questions (resolve in `/ce-plan`)
- Q2.1 Sandbox model — subprocess with restricted env + timeout is the minimum; further sandboxing (firejail/macOS sandbox-exec) is out of scope for v1.
- Q2.2 Transport coverage — stdio first; HTTP/SSE deferred. Confirm.
- Q2.3 Check severity — hard fail (non-zero exit) vs warn with `--strict` to escalate. Recommend hard fail; this is the whole point.
- Q2.4 Hashing scheme — include input schema JSON in the hash? Description-only is the prompt-injection surface; schema changes are also worth surfacing. Recommend: hash `name + description + JSON-canonicalized input schema`.

## 3. Remote-source resolver — execute the designed model

### Problem
Skills, commands, plugins, and templates can declare `source: github:org/repo/path@version`, `source: npm:@org/pkg@version`, or `source: https://...`. design §1 and §8 specify this model; README "Status" admits implementation is unbuilt. The community currently distributes these via git submodules (`problem-space.md` §6) — painful, unversioned, and not content-hashed.

This is not a new design — it is the execution of one already locked in. The brainstorm role here is to confirm scope and call out the cross-cutting decisions.

### What ships
The resolver executes `source:` references at `ainfra lock` time, fetches content, content-hashes it, and stores both the resolved version (e.g., the git commit SHA the tag pointed to) and the content hash in `ainfra.lock`. `apply` materializes content from the resolved address into the agent's native directory layout. `check` re-verifies content against the lockfile hash.

### Success criteria
- The three v1 schemes work end-to-end: `github:`, `npm:`, `https:` (in addition to existing `local:` / file paths).
- Lockfile pins both the resolved address (commit SHA for git; tarball URL + integrity for npm; effective URL for https) and the content hash.
- A team's skills/commands repo can be referenced as one line in `ainfra.yaml` and applied identically on every teammate's machine.
- An unreachable source at apply time is a clean error, not a partial render.
- Caching is keyed on content hash; cache hits are silent and offline-capable.

### Open questions (resolve in `/ce-plan`)
- Q3.1 Auth — SSH agent for `github:`, `NPM_TOKEN` env for `npm:`, bearer token env for `https:`. Confirm conventions.
- Q3.2 Sub-path semantics — `github:acme/skills/incident-response@2.3.0` selects the `incident-response` subdirectory of the `acme/skills` repo. Confirm.
- Q3.3 Cache location — `$XDG_CACHE_HOME/ainfra/sources/` or `.ainfra/cache/` per repo? Recommend XDG global, content-addressed.
- Q3.4 Tag vs commit pinning — accept tags in the manifest, resolve to commits in the lockfile. Confirm.

## 4. `ainfra status` — explicit inventory

### Problem
`problem-space.md` §10 documents that developers can't easily answer "what MCP servers, skills, hooks, and commands are active for me right now, with versions?" Claude Code's `/mcp` and `/doctor` are diagnostics, not inventories. Both npm (`npm ls`) and Terraform (`terraform state list`) ship this as a primitive. ainfra's `plan` and `check` are diff-shaped; there is no just-show-me view.

### What ships
A new read-only command, `ainfra status`, that prints the resolved manifest + lockfile as a flat inventory: every channel entry, its resolved version, its content hash, its source layer (org/team/repo/personal), and its target agent.

### Success criteria
- `ainfra status` runs offline against `ainfra.lock` alone — no network, no subprocess, no MCP server starts.
- Default output is human-readable, grouped by channel.
- `--json` emits the same data machine-readably for scripting and editor integrations.
- Filtering by channel (`ainfra status mcp`, `ainfra status hooks`) works.
- Output includes which lockfile layer each entry came from, so users can debug precedence visually.

### Open questions (resolve in `/ce-plan`)
- Q4.1 Should `status` show entries currently filtered out by selectors / agent gating, or only what would render now? Recommend: hide by default, expose with `--all`.
- Q4.2 Should `status` flag entries whose lockfile content hash does not match disk? Recommend no — that's `check`'s job. Keep status pure-read.

## 5. Cross-cutting

### Documentation deltas
- README "Try it" section gets a third top-of-funnel command alongside greenfield and team-joining: **adopting an existing repo.**
- README "Status" section can drop the "remote sources are the remaining follow-up" caveat after #3 ships.
- `docs/reference/design.md` §10 (Build Phases) gets a Phase 6 entry covering all four items, or §13 (failure modes ainfra defends against) gets a new row for "MCP tool description drift."
- Lockfile schema spec (`spec/lockfile-schema.md`) updates for the new `toolsHash` field.

### Non-goals reaffirmed
ainfra does not:
- Curate a registry of "trusted" MCP servers.
- Sign or verify upstream package signatures in v1 (separate v2 item).
- Render to Cursor, Copilot, Windsurf, or any other agent beyond the existing `claude-code` and `codex` targets in v1.

### Sequencing recommendation
1. **Tool-description hashing first.** Smallest engineering surface; biggest single message-strengthener. Lands inside the existing lock + check pipeline. Unblocks the security framing of the v1 announcement.
2. **`ainfra status` second.** Trivial; closes a UX gap that newcomers will hit on day one.
3. **`ainfra adopt` third.** Larger surface; benefits from #2 existing (so the user can `adopt` then `status` to inspect the result).
4. **Remote-source resolver fourth.** Largest implementation; mostly mechanical against the existing design.

This ordering also matches risk: each prior item makes the next easier to test and verify.

## 6. Out of scope, named explicitly

The brainstorm explicitly considered and deferred:

| Item | Reason |
|---|---|
| Cursor renderer | Renderer-only, no engine change; deserves its own brainstorm. v1.5 multiplier. |
| Sigstore / npm provenance verification | Reactive; ship after first ainfra-reachable supply-chain incident. v2. |
| Skills persona drift hardening | Evidence is theoretical, not observed. Watch for incidents first. |
| Cross-agent orchestration coordination | No documented incidents. Premature. |
| MCP registry curation / quality scoring | Not ainfra's job; consume registries, don't own them. |
| Path-scoped render targets | Already deferred in design §15. |

## 7. Open questions (master list)

Carry these into `/ce-plan`:

- Q1.1 Adopt channel coverage for v1
- Q1.2 Adopt layer splitting
- Q1.3 Adopt YAML output formatting
- Q2.1 Lock-time sandbox model
- Q2.2 Tool-description hash transport coverage (stdio-only?)
- Q2.3 Check severity on description drift
- Q2.4 Hash scheme — include input schema?
- Q3.1 Remote-source auth conventions
- Q3.2 Sub-path semantics for `github:` sources
- Q3.3 Cache location
- Q3.4 Tag vs commit pinning
- Q4.1 `status` visibility of filtered entries
- Q4.2 `status` flagging of on-disk drift
