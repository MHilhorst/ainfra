---
date: 2026-05-28
topic: ainfra-audit
---

# ainfra audit

## Summary

A read-only `ainfra audit` command that prints a layered inventory of every Claude-related artifact on a machine — Global (`~/.claude`) and Project (`.claude/` in the current repo) — across every channel (mcpServers, skills, plugins, hooks, agents, settings, CLAUDE.md), tagging each row as managed / unmanaged / shadowed / stale with its source manifest annotated when known. A summary footer surfaces adoption opportunities and the next command to run.

---

## Problem Frame

ainfra exists to manage Claude Code config across global and project scopes — but a developer adopting ainfra (or evaluating it) has no single command that lays out *what they already have* so they can see what ainfra would manage. `ainfra list` is scoped to entries ainfra already owns (committed and personal lockfiles), and `ainfra staleness` answers a different question. Existing ainfra users likewise lack a one-shot health view that mixes "what's managed" with "what's drifted, stale, or sitting outside any manifest."

The result is friction in three moments:

- **First-run adoption.** A user with an accumulated `~/.claude` (skills, plugins, MCP servers, settings.json allowlists, hooks, agents) cannot see what ainfra could pick up for them. There is no pitch surface inside the CLI itself.
- **Onboarding.** A developer joining a team can't quickly compare what their machine has against what's expected — `list` only shows ainfra-managed rows.
- **Drift diagnosis.** An existing user wanting to know "what's actually here vs. what my manifest says" has to read multiple commands and reconcile output mentally.

A single layered inventory command closes all three.

---

## Actors

- A1. **Newcomer evaluating ainfra:** has Claude Code installed with accumulated config; not yet using ainfra. Runs `audit` to see what's on their machine and what ainfra could manage.
- A2. **Existing ainfra user diagnosing drift:** has at least one manifest in use; runs `audit` to see managed vs. unmanaged vs. stale in one view.
- A3. **Teammate onboarding:** cloned a repo that uses ainfra; runs `audit` to see what's expected (project layer) vs. what they already have (global layer).

---

## Requirements

**Command shape**

- R1. `ainfra audit` is read-only — it never modifies filesystem state, manifests, or lockfiles.
- R2. `ainfra audit` runs successfully on a machine with no ainfra manifests present; it does not require ainfra to be initialized.
- R3. `ainfra audit` supports `--json` for stable machine-readable output, mirroring the convention used by `ainfra list`.

**Layer model**

- R4. Output is structured around exactly two filesystem layers: **Global** (`~/.claude`) and **Project** (`.claude/` in the current repo, when run inside one).
- R5. When run outside a git repo or outside a project that contains `.claude/`, the Project section is omitted (not rendered empty) and a one-line note explains why.
- R6. "Team" is not a layer. When an entry's origin is known (e.g., installed from a published team manifest), it is shown as a per-row source annotation, not as a third section.

**Channels covered**

- R7. Per layer, audit lists entries for every Claude channel ainfra is aware of: mcpServers, skills, plugins, hooks, agents, slash commands, settings (permissions / env / model), and CLAUDE.md / AGENTS.md files.
- R8. Channels ainfra does not yet manage (e.g., hooks, agents, CLAUDE.md if not yet a managed channel) are still surfaced as inventory rows — flagged so the reader understands they are visible but not yet adoptable.

**Row annotations**

- R9. Each row carries a status tag: `[managed]`, `[unmanaged]`, `[shadowed-by: <layer>]`, `[stale]`, `[drift]`. Tags may combine where applicable (e.g., a managed row that is also stale).
- R10. Each row carries a source annotation when known (e.g., `from: <manifest-id>@<version>`, `from: personal manifest`, or none for unmanaged entries). Source annotation is on by default — not gated behind `--verbose`.
- R11. Shadowed rows are shown in their originating layer, not hidden; the annotation names the shadowing layer.

**Settings handling**

- R12. `settings.json` contents (permissions allowlists, env vars, model overrides, hooks blocks) are summarized per layer — not dumped verbatim. Audit reports counts and notable entries, not a copy of the file.
- R13. `settings.local.json` (gitignored per-user permissions) is surfaced as a distinct row within the Project layer's settings group, clearly labeled as gitignored / local.

**Footer**

- R14. Audit always closes with a one-block footer summarizing: count of adoptable items, count of stale items, count of drift items, and a single suggested next command (e.g., `ainfra adopt --scope=user`).
- R15. The footer renders even when every detected item is already managed and current — in that case it shows a positive health line (e.g., `all detected config is managed by ainfra`) and no next-command suggestion.

---

## Acceptance Examples

- AE1. **Covers R2, R10, R14.** Given a machine with `~/.claude` containing several MCP servers, two installed plugins, and a `CLAUDE.md`, and no ainfra manifest, when the user runs `ainfra audit`, the Global section lists each item with `[unmanaged]` and no source annotation, and the footer reports `N items adoptable` with `ainfra adopt --scope=user` as the next command.
- AE2. **Covers R6, R10.** Given a managed skill installed from a published team manifest, when the user runs `ainfra audit`, the skill appears in the Global section as `[managed]` with annotation `from: <team-manifest-id>@<version>` — no separate "Team" section is rendered.
- AE3. **Covers R4, R5.** Given the command is run outside any git repo or in a repo without `.claude/`, when `ainfra audit` runs, only the Global section is rendered and a one-line note explains the Project section is omitted.
- AE4. **Covers R11.** Given a skill named `foo` is present in both Global and Project layers, when `ainfra audit` runs, both rows appear — the Global one tagged `[shadowed-by: project]` and the Project one tagged with its normal status.
- AE5. **Covers R15.** Given every managed entry is current and no unmanaged entries exist, when `ainfra audit` runs, the footer renders the positive health line and no next-command suggestion.
- AE6. **Covers R13.** Given the current project has both `.claude/settings.json` and `.claude/settings.local.json`, when `ainfra audit` runs, the Project settings group lists the two files as separate rows with the `.local.json` row labeled gitignored/local.

---

## Success Criteria

- A user with accumulated Claude config but no ainfra setup can run `ainfra audit` once and immediately see (a) what ainfra would manage, (b) the suggested next command to start, without consulting docs.
- An existing ainfra user gets the same information they currently piece together from `list`, `staleness`, and filesystem inspection in a single command.
- `ce-plan` can implement audit from this doc without needing to invent the layer model, status taxonomy, or footer behavior.

---

## Scope Boundaries

- **Resolved effective-config view** — what Claude actually sees after layer resolution, with per-key provenance. Deferred to a possible `--resolved` flag in a later iteration. Audit v1 shows raw inventory per layer with shadowing as annotation, not a resolution simulation.
- **Status-first structural framing** — sections by managed / unmanaged / drift instead of by layer. Rejected; the adoption pitch is carried by the footer, while structure stays consistent with `list` and `staleness`.
- **A third "team" layer in the filesystem model.** Team manifests are sources, not locations — surfacing them as a layer would misrepresent Claude's actual two-layer model.
- **Actually performing adoption.** Audit is read-only; the footer points at `ainfra adopt`. Audit never writes.
- **Mutating staleness or drift state.** Audit reports it; reconciling is `update` / `reconcile`'s job.

---

## Key Decisions

- **Two layers, not three:** Global and Project mirror Claude Code's real filesystem model. "Team" is a source annotation, not a structural layer. Rationale: avoids inventing a layer that does not exist on disk and that would confuse users.
- **Source annotation on by default:** every row that came from a known manifest shows its origin without `--verbose`. Rationale: the adoption narrative depends on every unmanaged row implicitly telegraphing "could be managed by a manifest" — defaulting the annotation off would hide that signal.
- **Footer always renders:** even on fully-clean machines. Rationale: turns audit into a recurring health-check that users re-run, which is the retention loop ainfra wants.
- **Audit covers channels ainfra doesn't manage yet:** hooks, agents, CLAUDE.md are inventoried even when they aren't adoptable through ainfra today. Rationale: audit's value as a snapshot beats waiting for full channel coverage; the `[unmanaged]` tag honestly represents the state.

---

## Dependencies / Assumptions

- ainfra already has filesystem scanning infrastructure for Global and Project layers (used by `list`, `adopt`, `staleness`). Audit composes against that rather than re-implementing scanning. *(Assumption — verify against `internal/manifest`, `internal/adopt`, `internal/installer` during planning.)*
- Lockfile and manifest representations expose enough metadata to source-annotate managed rows (manifest id + version). *(Assumption — verify against `internal/lockfile`.)*
- Channels not yet managed by ainfra (hooks, agents, CLAUDE.md) can be enumerated from the filesystem without parsing their content beyond detection. Audit does not need to semantically interpret these files in v1.

---

## Outstanding Questions

### Deferred to Planning

- [Affects R7, R8][Technical] Exact channel coverage for v1 — is every named channel scannable from existing ainfra code, or does any channel require new detectors? Confirm during planning.
- [Affects R12][Technical] How `settings.json` content is summarized — which fields are "notable" enough to surface (allowlist count, hooks block presence, model override) vs. omit. Settle during planning with reference to the schema.
- [Affects R9][Technical] Whether `[drift]` and `[stale]` are distinct tags or one combined `[stale]` tag — depends on how the existing `staleness` command labels states.
- [Affects R14][Needs research] Footer's exact suggested-command logic when there are both adoptable and stale items — which takes priority, or do we show both?
- [Affects R3][Technical] JSON schema for `audit --json` output — shape it to remain stable for downstream tooling; align field naming with `list --json`.
