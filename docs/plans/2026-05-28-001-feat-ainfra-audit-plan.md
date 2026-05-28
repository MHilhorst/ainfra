---
date: 2026-05-28
type: feat
status: active
origin: docs/brainstorms/2026-05-28-ainfra-audit-requirements.md
---

# feat: Add `ainfra audit` command

## Summary

Add a read-only `ainfra audit` command that inventories Claude Code config across two filesystem layers — Global (`~/.claude/`) and Project (`.claude/`) — for every channel (mcpServers, skills, plugins, hooks, agents, slash commands, settings, CLAUDE.md / AGENTS.md). Each row is tagged with management status (`managed` / `unmanaged` / `shadowed` / `stale`), annotated with source manifest when known, and the command closes with an adoption-summary footer pointing at the next action.

---

## Problem Frame

ainfra exists to manage Claude config across global and project scopes, but a developer adopting ainfra (or diagnosing drift inside it) has no single command that lays out *what they already have*. `ainfra list` (`cmd/ainfra/cmd_list.go`) only knows about entries already in the lockfile; `ainfra staleness` answers a different question; `ainfra adopt` writes a draft manifest but doesn't print an inventory.

The result is three friction moments — first-run adoption pitch, teammate onboarding, drift diagnosis — none of which have a coherent surface today. `audit` fills that gap with a single read-only inventory command.

See origin: `docs/brainstorms/2026-05-28-ainfra-audit-requirements.md`.

---

## Actors

Carried from origin (see origin: `docs/brainstorms/2026-05-28-ainfra-audit-requirements.md`).

- A1. **Newcomer evaluating ainfra** — drives the adoption-footer and `unmanaged` annotation requirements.
- A2. **Existing ainfra user diagnosing drift** — drives `managed` / `shadowed` / `stale` annotations and inline drift signal.
- A3. **Teammate onboarding** — drives Project-layer visibility distinct from Global, and source-annotation per row.

---

## Key Technical Decisions

- **Filesystem layers in the display, manifest layers in the annotation.** Two-layer output (Global, Project) matches Claude's real on-disk model and what the brainstorm landed on. ainfra's existing three-layer manifest model (`team`, `repo`, `personal` — see `internal/manifest/types.go`) is preserved internally and surfaces as the per-row source annotation. This keeps the display honest about where files live while reusing the existing manifest/lockfile cross-reference logic from `cmd_list.go`.
- **Reuse `adopt.Layout` for filesystem entry points.** `internal/adopt/scan.go` already abstracts repo-vs-user scanning via `Layout` (`RepoLayout(dir)` / `UserLayout(home)`). Audit composes against this rather than reinventing path resolution. New scanner functions added in `internal/audit` handle the channels `adopt.Scan` doesn't enumerate yet (skills, plugins, agents).
- **New `internal/audit` package owns scan + reconcile.** Keeps `cmd/ainfra/cmd_audit.go` thin (flag parsing + rendering) and isolates the scan/reconcile logic for unit testing. Mirrors how `internal/adopt` separates scanning from the `cmd_adopt.go` CLI surface.
- **Management status derived by cross-referencing disk inventory with lockfile + manifest.** A row is `managed` when present in `ainfra.lock` (committed) or `ainfra.personal.lock`. Shadowing reuses the same precedence logic `cmd_list.go:collectShadowedFromManifest` already implements (`team` < `repo` < `personal`). Staleness defers to whatever signal `ainfra _staleness-check` exposes — audit reads it, does not recompute it.
- **Source annotation on by default.** Per origin call-out: every managed row shows its origin (e.g., `from: repo manifest`, `from: personal manifest`, `from: <extends-source>@<version>`) without `--verbose`. Annotation is computed from `lockfile.Entry.Layer` plus the manifest's `Source` field when known.
- **`audit` is read-only.** No filesystem writes, no lockfile mutation, no manifest edits. Footer suggests `ainfra adopt --scope=user` or similar but does not run it.

---

## Requirements Trace

| Origin R-ID | Plan coverage |
|---|---|
| R1 (read-only) | U1 command shape; reinforced by Phase 5 review. |
| R2 (works without manifest) | U1, U2 — scanner is filesystem-first, lockfile/manifest are optional inputs. |
| R3 (`--json`) | U6 (rendering) — JSON Lines mirrors `cmd_list.go` convention. |
| R4 (two layers) | U2 (scanner), U6 (renderer). |
| R5 (omit Project section when no repo) | U2 detects layer applicability; U6 renders the note. |
| R6 (no team layer) | U4 source annotation; team entries appear inline as `from: …`. |
| R7 (channel coverage) | U2 (existing channels via adopt + lockfile), U3 (new scanners for skills, plugins, agents). |
| R8 (unmanaged channels still visible) | U3. |
| R9 (status tags) | U4 reconcile. |
| R10 (source annotation default-on) | U4 reconcile + U6 render. |
| R11 (shadowed rows shown in originating layer) | U4 reuses `collectShadowedFromManifest` precedence. |
| R12 (settings.json summarized) | U3 settings scanner. |
| R13 (`settings.local.json` distinct row) | U3 settings scanner emits separate row. |
| R14 (adoption footer with counts + next command) | U5. |
| R15 (positive-health footer when clean) | U5. |
| AE1–AE6 | Covered by integration tests in U7 (see test scenarios). |

---

## High-Level Technical Design

*This illustrates the intended approach and is directional guidance for review, not implementation specification. The implementing agent should treat it as context, not code to reproduce.*

Data flow at a glance:

```
                ┌────────────────────────────────────┐
                │  cmd_audit.go (flag parse, render) │
                └─────────────────┬──────────────────┘
                                  │
                                  ▼
                    ┌─────────────────────────────┐
                    │  internal/audit.Run(ctx)    │
                    └──────────┬──────────────────┘
              ┌────────────────┼────────────────┐
              ▼                ▼                ▼
   ┌──────────────────┐  ┌──────────────┐  ┌────────────────────┐
   │ ScanLayer(global)│  │ ScanLayer    │  │ ManifestLoadLayers │
   │  (filesystem)    │  │  (project)   │  │  + lockfile.Read   │
   └──────────────────┘  └──────────────┘  └────────────────────┘
              │                │                    │
              └────────────────┴────────────┬───────┘
                                            ▼
                                  ┌──────────────────────┐
                                  │  Reconcile(rows)     │
                                  │  - status tagging    │
                                  │  - shadowing         │
                                  │  - source annotation │
                                  └──────────┬───────────┘
                                             ▼
                                  ┌──────────────────────┐
                                  │  Render (text/JSON)  │
                                  │  + footer summary    │
                                  └──────────────────────┘
```

A row is an `audit.Row{Layer, Channel, ID, Status, Source, ShadowedBy, StaleReason}` value. The scanner emits rows from disk; the reconcile pass merges them with lockfile + manifest data to compute `Status` and `Source`. Renderers consume `[]Row` plus a summary footer struct.

---

## Output Structure

New files this plan creates:

```
cmd/ainfra/
  cmd_audit.go              (new)
  cmd_audit_test.go         (new)
internal/audit/
  audit.go                  (new — Run, Row, FooterSummary types)
  scan.go                   (new — per-channel disk scanners)
  scan_test.go              (new)
  reconcile.go              (new — status + source annotation logic)
  reconcile_test.go         (new)
  render.go                 (new — text + JSON renderers)
  render_test.go            (new)
  testdata/                 (new)
```

Modified files:

```
cmd/ainfra/commands.go      (register audit command)
```

The directory tree is a scope declaration — the implementer may collapse `scan.go` + `reconcile.go` into one file if the line count stays small. Per-unit `**Files:**` lists are authoritative.

---

### U1. Command surface

**Goal:** Add `ainfra audit` as a registered subcommand with flags and a top-level `Run` that delegates to `internal/audit`.

**Requirements:** R1, R2, R3.

**Dependencies:** none.

**Files:**
- `cmd/ainfra/cmd_audit.go` (new)
- `cmd/ainfra/cmd_audit_test.go` (new)
- `cmd/ainfra/commands.go` (modify — register `newAuditCommand()`)
- `internal/audit/audit.go` (new — `Run(ctx cli.Context, opts Options) int`, `Options{JSON bool}`)

**Approach:**
- Mirror the shape of `newListCommand` in `cmd/ainfra/cmd_list.go`: cobra-free `cli.Command` value with `Name`, `Summary`, `UsageLine`, `Example`, `SetFlags`, `Run`.
- Single flag in v1: `--json`. No `--channel` filter in v1 (audit's value is the full picture; deferred to future if requested).
- `Run` resolves `ctx.Dir`, calls `audit.Run`, exits non-zero only on real errors (a missing repo manifest is not an error per R2).

**Patterns to follow:** `cmd/ainfra/cmd_list.go` (flag setup, `cli.Command` value, JSON gate).

**Test scenarios:**
- `audit --json` emits one JSON object per line and no decorative text; exit code 0.
- `audit` outside a git repo / repo without `.claude/` exits 0 and shows only the Global section.
- `audit` with no `ainfra.yaml` and no lockfiles still exits 0 (no `ainfra.lock not found` error path — this is the difference vs `list`).
- Unknown flag returns exit 2 and the usage line.

**Verification:** `go test ./cmd/ainfra -run TestAudit` passes; running `ainfra audit` on the worktree itself produces non-empty output with a footer.

---

### U2. Scan existing channels via `adopt.Layout`

**Goal:** Enumerate the channels `internal/adopt` already understands (mcpServers, hooks, commands, rules) from both layers and produce raw `audit.Row` values.

**Requirements:** R4, R5, R7.

**Dependencies:** U1.

**Files:**
- `internal/audit/scan.go` (new — `ScanLayer(layer audit.Layer, layout adopt.Layout) ([]Row, error)`)
- `internal/audit/scan_test.go` (new)
- `internal/audit/testdata/` (new — fixture `.claude/` trees)

**Approach:**
- Two `audit.Layer` constants: `LayerGlobal`, `LayerProject`. These are display layers, distinct from `manifest.Layer`.
- `ScanLayer` calls `adopt.ScanLayout(layout)` and converts the returned `manifest.Manifest` into `[]Row` per channel.
- When `layout.MCPFile` is empty / missing, the channel is reported as `(channel detection pending)` rather than empty — see U3 framing for the same idea applied to skills/plugins/agents.
- Project layer is omitted when `ctx.Dir` has no `.claude/` and no `ainfra.yaml` (R5). Emit a one-line `audit.FooterNote` explaining the omission for the renderer.

**Patterns to follow:** `internal/adopt/scan.go:Scan`, `RepoLayout`, `UserLayout`.

**Test scenarios:**
- Fixture with `.claude/settings.json` containing hooks → hooks rows appear in Project layer.
- Fixture with `.mcp.json` at repo root → mcpServers rows appear in Project layer.
- Empty fixture (no `.claude/`, no `.mcp.json`) → Project rows empty, FooterNote captures the reason.
- Global layout pointed at fixture home → mcpServers detection is intentionally skipped (matches `adopt.UserLayout` behavior), surfaced as `(detection pending)`.

**Verification:** `go test ./internal/audit -run TestScanLayer` passes; rows have expected `Channel`, `ID`, `Layer`.

---

### U3. New scanners for skills, plugins, agents, settings

**Goal:** Enumerate skills, plugins, agents, and settings (channels `adopt` doesn't cover today) directly from `.claude/` and `~/.claude/` so unmanaged entries surface per R7/R8.

**Requirements:** R7, R8, R12, R13.

**Dependencies:** U1.

**Files:**
- `internal/audit/scan.go` (extend with `scanSkills`, `scanPlugins`, `scanAgents`, `scanSettings`)
- `internal/audit/scan_test.go` (extend)
- `internal/audit/testdata/` (add skills, plugins, agents, settings fixtures)

**Approach:**
- **Skills:** enumerate top-level directories under `<layer>/.claude/skills/`. Each directory is one row keyed by directory name; surface presence/absence of `SKILL.md` as part of the row metadata.
- **Plugins:** enumerate top-level entries under `<layer>/.claude/plugins/`. Each plugin (directory) is one row.
- **Agents:** enumerate `<layer>/.claude/agents/*.md` and `<layer>/.claude/agents/*/agent.md`. Each agent is one row keyed by stem name.
- **Settings:** scan `<layer>/.claude/settings.json` and (Project layer only) `<layer>/.claude/settings.local.json`. Emit one row per file, summarizing notable fields: count of allowlisted permissions, presence of hooks block, presence of model override, count of env vars. Do not dump file contents (R12). The `.local.json` row carries a `gitignored: true` marker for the renderer (R13, AE6).
- All scanners tolerate missing directories — return empty slice, no error.

**Patterns to follow:** `internal/adopt/scan.go:readHooks`, `readCommands` (directory-walking style, return-empty-on-missing).

**Test scenarios:**
- Fixture `.claude/skills/foo/` + `.claude/skills/bar/SKILL.md` → both skills appear; `bar` row has the `SKILL.md` marker.
- Fixture `.claude/plugins/myplugin/plugin.json` → one plugins row for `myplugin`.
- Fixture `.claude/agents/reviewer.md` → one agents row keyed `reviewer`.
- Fixture `.claude/settings.json` with 12 permission entries + hooks block → settings row summarizes `12 permissions · hooks block · no model override`.
- Fixture `.claude/settings.local.json` → distinct row labeled gitignored/local (AE6).
- Missing `<layer>/.claude/skills/` → no skills rows, no error.

**Verification:** `go test ./internal/audit -run TestScanLayer` continues to pass with new sub-tests for each scanner; combined run on a fixture with all four channels produces the expected row count.

---

### U4. Reconcile against manifest + lockfile

**Goal:** Tag each scanned row with `Status` (`managed` / `unmanaged` / `shadowed` / `stale`) and a `Source` annotation by cross-referencing scanned rows with `manifest.LoadLayers(ctx.Dir)` and `lockfile.Read`.

**Requirements:** R6, R9, R10, R11.

**Dependencies:** U2, U3.

**Files:**
- `internal/audit/reconcile.go` (new — `Reconcile(rows []Row, m map[manifest.Layer]*manifest.Manifest, committed, personal *lockfile.Lock) []Row`)
- `internal/audit/reconcile_test.go` (new)

**Approach:**
- For each scanned row, look up `(Channel, ID)` in the merged lockfile (committed first, then personal). A match → `Status = managed`, `Source` derived from `lockfile.Entry.Layer` (`team` → `from: <manifest-id>@<version>` if available via manifest's `Source` field; `repo` → `from: repo manifest`; `personal` → `from: personal manifest`). No match → `Status = unmanaged`, `Source = ""`.
- Cross-layer shadowing: reuse the precedence logic from `cmd/ainfra/cmd_list.go:collectShadowedFromManifest` (`team` < `repo` < `personal`). When the same `(Channel, ID)` exists at two manifest layers, the lower-priority row is tagged `Status = shadowed`, `ShadowedBy = <winning manifest layer>`. Shadowed rows appear in their originating layer per R11.
- Staleness: read whatever signal `ainfra _staleness-check` already exposes (see `cmd/ainfra/cmd_staleness.go`). Audit does not re-compute it; if the staleness command is wired to a separate ledger, audit reads it; otherwise this tag is left empty for v1 and a Deferred-to-Planning note is captured.
- Status tags may combine (`managed + stale`), encoded as multiple bool fields on `Row` (`Status` is a bitset-like struct, not a single string) so the renderer can compose `[managed][stale]`.

**Patterns to follow:** `cmd/ainfra/cmd_list.go:annotateShadowed`, `collectShadowedFromManifest`.

**Test scenarios:**
- Scanned row matches lockfile entry at `repo` layer → `managed`, `Source = "from: repo manifest"`.
- Scanned row matches lockfile entry whose manifest has `extends:` source `github:org/team-config@1.2.0` → `Source` includes that ref.
- Same `(channel, id)` declared at `repo` and `personal` → `repo` row tagged `managed`, `personal` row tagged `managed` + `shadowed` with `ShadowedBy = repo`.
- Scanned row with no lockfile match → `unmanaged`, `Source = ""`.
- Reconcile on empty scanned rows + empty lockfile → empty result, no panic.
- Covers AE2. Skill installed from a team-published manifest reconciles to a single `managed` row whose `Source` includes the manifest ref; no separate "team" section is emitted.
- Covers AE4. Same skill in Global and Project layers reconciles to two rows — Global row tagged `shadowed-by: project`, Project row tagged normally.

**Verification:** `go test ./internal/audit -run TestReconcile` passes; combined reconcile + scan flow on a fixture produces expected row count and tags.

---

### U5. Footer summary

**Goal:** Compute and render the audit footer per R14 (counts + suggested next command) and R15 (positive-health line when clean).

**Requirements:** R14, R15.

**Dependencies:** U4.

**Files:**
- `internal/audit/audit.go` (extend — `FooterSummary{Adoptable, Stale, Drift int; Suggested string; Healthy bool}`)
- `internal/audit/reconcile.go` (extend — `BuildFooter(rows []Row) FooterSummary`)
- `internal/audit/reconcile_test.go` (extend)

**Approach:**
- `Adoptable` = count of `unmanaged` rows whose channel is in the adoptable set (mcpServers, hooks, commands, rules — i.e., the channels `ainfra adopt` can currently emit into a manifest). Skills/plugins/agents rows count as visible-but-not-yet-adoptable and are not in `Adoptable` — the footer's `Suggested` next-command must not lie about what `adopt` will pick up.
- `Stale` and `Drift` derive from the corresponding status tags (left at 0 in v1 if U4 deferred staleness wiring).
- `Suggested` selection: when `Adoptable > 0` in Global only → `ainfra adopt --scope=user`. In Project only → `ainfra adopt`. In both → suggest `--scope=user` first (the cross-repo win), document Project follow-up in the footer prose. When `Stale > 0` and `Adoptable == 0` → suggest `ainfra update`. (See Outstanding Questions — exact branching is captured below for confirmation during implementation.)
- `Healthy = true` when all scanned managed rows are non-stale, non-drift, and there are zero unmanaged-adoptable rows. Renderer prints the positive line and skips `Suggested`.

**Patterns to follow:** none specific — new logic, but keep deterministic ordering for tests.

**Test scenarios:**
- 3 unmanaged mcps in Global, 0 elsewhere → `Adoptable = 3`, `Suggested = "ainfra adopt --scope=user"`.
- 0 unmanaged, 2 stale → `Adoptable = 0`, `Stale = 2`, `Suggested = "ainfra update"`.
- 0 unmanaged, 0 stale, 0 drift, ≥1 managed row → `Healthy = true`.
- 0 unmanaged, 0 stale, 0 drift, 0 managed rows (true fresh-machine) → `Healthy = true`, footer notes "no Claude config detected".
- Covers AE1. Global has unmanaged mcpServers, plugins, and a CLAUDE.md, no ainfra manifest → footer reports `N items adoptable` with `ainfra adopt --scope=user` and the Adoptable count matches only the adopt-eligible channels (not plugins).
- Covers AE5. All managed, no unmanaged, no stale → `Healthy = true`, no next-command suggestion.

**Verification:** `go test ./internal/audit -run TestFooter` passes; rendered output on a healthy fixture shows the positive line and no next command.

---

### U6. Render text + JSON output

**Goal:** Render reconciled rows + footer to either a human-readable layered text view (default) or JSON Lines (when `--json`).

**Requirements:** R3, R4, R5, R10, R11, R12, R13, R14, R15.

**Dependencies:** U4, U5.

**Files:**
- `internal/audit/render.go` (new — `RenderText(w io.Writer, c *ui.Colorizer, rows []Row, footer FooterSummary, notes []FooterNote)`, `RenderJSON(w io.Writer, rows []Row, footer FooterSummary)`)
- `internal/audit/render_test.go` (new)

**Approach:**
- Text layout: two top-level sections, `GLOBAL (~/.claude)` and `PROJECT (.claude in <repo-name>)`. When a layer is omitted per R5, render the section header replaced by a one-line `notes` entry explaining why.
- Within each layer, group rows by channel in a fixed order matching `cmd_list.go`: mcpServers, hooks, commands, skills, plugins, agents, settings, rules.
- Per-row format: `  <channel-padded>  <id-padded>  <version-or-dash>  <status-tags>  <source-annotation>`. Status tags rendered as `[managed]`, `[unmanaged]`, `[shadowed-by: project]`, `[stale]`, combinable.
- Source annotation always rendered when non-empty (R10 — default-on).
- Footer block: blank line, then either the count line + suggested command, or the healthy line, then optional notes.
- JSON Lines: one row per line as `audit.Row`, then one final line for `FooterSummary` keyed `{"footer": {...}}`. Keep field names aligned with `cmd_list.go:listEntry` JSON conventions.
- Reuse `internal/ui.Colorizer` for ANSI color, gated by `ctx.NoColor`.

**Patterns to follow:** `cmd/ainfra/cmd_list.go:runList` (column padding, JSON gate, `ui.NewColorizer`).

**Test scenarios:**
- Mixed rows across both layers + adoptable footer → text output golden-tested; JSON output decodes back to expected struct shape.
- `--json` mode: each line is valid JSON; final line contains the footer object.
- Project section omitted with note → text output shows the note, JSON output omits Project rows but includes the note in `notes`.
- Covers AE3. Run outside a repo → only Global section + one-line note explaining Project omission.
- Covers AE6. Project settings group lists `settings.json` and `settings.local.json` as separate rows, `.local.json` row clearly labeled gitignored/local.

**Verification:** `go test ./internal/audit -run TestRender` passes; visual smoke test by running `ainfra audit` against `internal/audit/testdata/` fixtures.

---

### U7. End-to-end CLI integration tests

**Goal:** Pin the user-visible behavior end-to-end at the `cmd/ainfra` boundary so the acceptance examples are guarded against regression.

**Requirements:** all (AE1–AE6 specifically).

**Dependencies:** U1–U6.

**Files:**
- `cmd/ainfra/cmd_audit_test.go` (extend with table-driven E2E cases)
- `cmd/ainfra/testdata/audit/` (new — full fixture trees)

**Approach:**
- Mirror the style of the existing `cmd/ainfra/e2e_test.go`: each case sets up a fixture `ctx.Dir` (and optionally a fake `HOME` pointing at a fixture `~/.claude/`), runs the audit command, and asserts on stdout / stderr / exit code.
- One case per acceptance example. Fixture trees should be tiny — a single skill, one MCP entry, one CLAUDE.md — to keep tests legible.

**Test scenarios:**
- Covers AE1. Fixture: `HOME/.claude/` contains 2 unmanaged mcpServers, 2 plugins, 1 CLAUDE.md; no ainfra manifest. Run `ainfra audit`. Assert: Global section rendered with each item `[unmanaged]`, no source annotation, footer reports `N items adoptable` with `ainfra adopt --scope=user`.
- Covers AE2. Fixture: a managed skill in the lockfile sourced from a team manifest. Run `ainfra audit`. Assert: skill appears `[managed]` with `from: <team-source>@<version>`; no separate Team section header in output.
- Covers AE3. Fixture: empty directory, no git repo. Run `ainfra audit`. Assert: Global section only; one-line note explains Project omission.
- Covers AE4. Fixture: skill `foo` present in both `~/.claude/skills/foo/` and `<repo>/.claude/skills/foo/`. Run `ainfra audit`. Assert: two rows; Global tagged `shadowed-by: project`, Project tagged normally.
- Covers AE5. Fixture: every entry is managed and current. Run `ainfra audit`. Assert: footer renders positive health line; no next-command suggestion.
- Covers AE6. Fixture: project with both `settings.json` and `settings.local.json`. Run `ainfra audit`. Assert: two settings rows, `.local.json` row labeled gitignored/local.

**Verification:** `go test ./cmd/ainfra -run TestAuditE2E` passes for every case; running `ainfra audit` manually against the worktree produces sensible output.

---

## Success Criteria

- A user with accumulated Claude config but no ainfra setup runs `ainfra audit` once and sees (a) what's on disk, (b) what's adoptable, (c) the suggested next command, without consulting docs.
- An existing ainfra user gets `list` + `staleness` + filesystem inspection unified in one command.
- `ce-work` can implement this plan without inventing the layer model, status taxonomy, footer behavior, or test scope.
- All six acceptance examples from the origin doc pass as automated tests in `cmd/ainfra/cmd_audit_test.go`.

---

## System-Wide Impact

- New top-level command — increases CLI surface area by one. No conflict with existing subcommands.
- New `internal/audit` package — isolated; no other package imports it in v1.
- No changes to `internal/manifest`, `internal/lockfile`, `internal/adopt` shapes — audit consumes them read-only.
- No new external dependencies.
- Help output (`ainfra --help`) gains one line; docs/quickstart may want an updated example (captured under Documentation Plan).

---

## Scope Boundaries

### Deferred for later

- **`--resolved` flag** — resolved effective-config view (per-key provenance after layer resolution). Defer per origin Scope Boundaries.
- **Channel filter** (`--channel` flag) — not in v1. Audit's value is the whole picture; revisit if users request it.
- **Watch mode / re-run on change** — out of scope; one-shot only.

### Outside this product's identity

- **Performing adoption from audit** — audit stays read-only; the footer points at `ainfra adopt` but does not run it.
- **Mutating staleness / drift state** — audit reports, `update` / `reconcile` reconciles.

### Deferred to Follow-Up Work

- **Status-first restructuring** (Approach C from brainstorm) — adoption framing is carried by the footer in v1. If telemetry shows users want a managed-vs-adoptable split view, revisit.
- **Per-channel adopt eligibility table in `--help`** — useful onboarding, not required for v1.

---

## Dependencies / Assumptions

- `internal/adopt.Layout` + `adopt.ScanLayout` will continue to be the canonical filesystem entry points. *(Verified — see `internal/adopt/scan.go`.)*
- `internal/manifest.LoadLayers` returns the merged personal layer (repo personal + global personal). *(Verified — see `internal/manifest/load.go`.)*
- `lockfile.Read` tolerates missing files and returns an empty `*lockfile.Lock` rather than erroring. *(Assumption — verify against `internal/lockfile/lockfile.go` during U1.)*
- Staleness signal can be read passively (without invoking the hook). If it requires invocation, U4 defers staleness wiring to a follow-up (see Outstanding Questions).
- `cli.Context.NoColor` is honored everywhere `ui.NewColorizer` is used. *(Pattern verified in `cmd_list.go`.)*

---

## Outstanding Questions

### Deferred to Planning

- *(none — all planning-time questions resolved.)*

### Deferred to Implementation

- [Affects U4][Technical] How to read the existing staleness signal without re-invoking the hook. If `ainfra _staleness-check` exposes a queryable ledger, U4 reads it; otherwise v1 leaves the `stale` tag empty and a follow-up task wires it in. Resolve during U4 by reading `cmd/ainfra/cmd_staleness.go`.
- [Affects U5][Technical] Exact priority when both `Adoptable > 0` and `Stale > 0` for the footer's `Suggested` field. Default in plan: lead with adoption, mention stale as a secondary note in the footer text. Confirm during U5 with a short integration test.
- [Affects U3][Technical] Whether to enumerate plugins from `.claude/plugins/<name>/plugin.json` or from a manifest cache file. If plugins have a richer cache surface, prefer it; otherwise the directory walk is the v1 floor.
- [Affects U6][Needs research] JSON Lines schema stability — should `audit --json` emit the same row shape as `list --json` (extended), or its own shape? Resolve by reading `cmd_list.go:listEntry` and deciding additive vs distinct.

---

## Documentation Plan

- Update `README.md` to mention `ainfra audit` in the command list (one line).
- Add `ainfra audit` to `docs/quickstart.md` if quickstart enumerates commands.
- No new top-level docs file in v1.
