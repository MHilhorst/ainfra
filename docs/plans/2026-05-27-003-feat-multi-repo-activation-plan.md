---
title: "feat: Multi-repo activation — user-level install + SessionStart staleness hook"
status: active
created: 2026-05-27
type: feat
depth: standard
origin: docs/brainstorms/2026-05-27-multi-repo-and-global-setup-requirements.md
---

# feat: Multi-repo activation — user-level install + SessionStart staleness hook

## Problem Frame

ainfra is repo-first today. `ainfra install` only acts when a directory has
an `ainfra.yaml`; the staleness of any materialized state is invisible until
the user remembers to re-install. Two real frictions follow:

1. **The user-level gap.** If you have a personal skill or MCP server you
   want available in *every* repo, including the ones that haven't been
   onboarded to ainfra, there's nowhere to put it that takes effect. The
   XDG personal layer (`$XDG_CONFIG_HOME/ainfra/personal.yaml`) exists in
   the data model but only materializes when a repo manifest drives an
   install.

2. **The staleness gap.** Even when a repo *is* onboarded, you can sit
   there running Claude against yesterday's state for hours after pulling
   a teammate's manifest change. There's no warning, no auto-detect, no
   prompt before the first Claude session.

The brainstorm at `docs/brainstorms/2026-05-27-multi-repo-and-global-setup-requirements.md`
took the npm lessons seriously — global installs become an anti-pattern when
they're the easy on-ramp; the win isn't "better globals" but deleting the
question entirely. This plan implements both pieces in one document, in two
delivery slices.

## Scope

**In scope**

- One `install` verb that reads every available layer (team via `extends:`,
  repo `ainfra.yaml`, user `$XDG_CONFIG_HOME/ainfra/personal.yaml`), merges
  with the existing precedence table, and writes to `repo/.claude/` AND
  `~/.claude/` as appropriate. No `--user` flag.
- `ainfra install` in a directory with no `ainfra.yaml` applies the user
  layer alone. Exit zero with a one-line summary.
- A user-global applied ledger at `~/.config/ainfra/applied.lock` that
  tracks ownership of files written under `~/.claude/`, so cleanup is safe
  and ainfra never deletes a file it didn't write.
- `ainfra list` shows shadowed entries explicitly (when a repo skill
  overrides a user-level one of the same id).
- A `SessionStart` hook ainfra installs by default in every managed repo.
  Runs a fast staleness check; prints one stderr line *only when stale*.
  Repos can opt out with `staleness_warning: false` in `ainfra.yaml`.
- An `ainfraVersion:` field in the manifest. `ainfra install` warns on
  mismatch between the binary running and the version a repo expects.
- Refresh `README.md`, `docs/quickstart.md`, `skills/using-ainfra/SKILL.md`,
  and `ainfra.yaml` to teach the new merge model, the hook, and the
  binary-version pin.

**Deferred for later** (carried from brainstorm)

- A `claude` wrapper / `ainfra claude`. The hook covers the staleness need;
  fragmenting the docs with a second "how to launch Claude" path isn't
  worth the cost.
- direnv-style shell hooks on `cd`. Real ergonomics win but its own design
  surface; not load-bearing once the SessionStart hook exists.
- `ainfra workspace install` (bulk install across multiple repos).
- Tag- or folder-based scoping ("personal layer X applies only to repos
  under `~/work/`").
- Auto-init of `ainfra.yaml` for unmanaged repos. The user-layer-everywhere
  model covers the actual use case without needing onboarding.
- A staleness-only `ainfra status` verb. `ainfra list` showing a staleness
  column is enough until scripting demand emerges.

**Outside this product's identity** (carried from brainstorm)

- Becoming an MCP runtime / gateway.
- Replacing secret managers.
- A daemon that watches and auto-reapplies.
- A skills marketplace / discovery layer.
- Intercepting `claude`. The canonical command stays untouched.

## Key Technical Decisions

1. **One verb, no flag.** `ainfra install` reads every available layer and
   writes wherever appropriate. In a non-ainfra directory, it acts on the
   user layer alone. The user thinks about *what skill belongs in which
   layer*, not *which verb to run*. (see origin §Decisions/3)

2. **Layering vs install target are different axes.**
   - Layering = where a thing is *declared* (team / repo / user-personal).
     All merge with existing precedence.
   - Install target = where files *land* (`repo/.claude/` vs `~/.claude/`).
   The merged result of all layers fans out to both targets in one apply.
   Mirrors npm's clean split between `.npmrc` hierarchy and `node_modules`
   location. (see origin §Decisions/4)

3. **User-global ledger at `~/.config/ainfra/applied.lock`.** Honors the
   XDG Base Directory spec; one file per user; tracks file ownership the
   same way the per-repo `.ainfra/applied.lock` already tracks repo-local
   ownership. Cleanup never deletes a file ainfra didn't write.

4. **`ainfraVersion:` is a manifest field, not a separate file.** Lives in
   `ainfra.yaml` as a top-level key. Keeps the repo root clean (no
   `.ainfra-version` file). Trade-off: less familiar to users coming from
   pyenv/nvm, where the convention is a standalone file. Acceptable —
   the value is in the pinning behavior, not the file shape.

5. **SessionStart hook is silent on clean, prints one line on stale.**
   No "ainfra in sync" chatter; the absence of a warning is the
   confidence signal. Mirrors how `git status` doesn't print "clean"
   unless asked. Repos opt out with `staleness_warning: false`.

6. **The hook is built-in, not declared in `ainfra.yaml`.** ainfra's
   reconciler always emits the hook into `.claude/settings.json` unless
   the manifest opts out. Users do *not* list it in their hooks block.
   This keeps the manifest focused on team intent, not infra plumbing.

7. **Staleness check is fast — a manifest-vs-applied-ledger hash compare.**
   No full re-resolve, no live filesystem walk; just compare the
   manifest's current hash to the hash recorded in the applied ledger.
   Same idea as `git status` not reading every blob.

8. **No `ainfra claude` wrapper.** The hook covers the underlying need.
   Adding a wrapper fragments the docs ("two ways to launch Claude") and
   tempts users into a path that breaks if ainfra is uninstalled. The
   canonical `claude` command stays the only entry point we recommend.

9. **No engine rewrite.** All work is concentrated in:
   - `internal/manifest/` (load XDG personal layer always; add
     `ainfraVersion`)
   - `cmd/ainfra/cmd_install.go` (merge + dual-target write)
   - `internal/provider/applied.go` (split ledger by scope)
   - `internal/provider/claudecode/hooks.go` (built-in SessionStart hook
     emission)
   - `cmd/ainfra/cmd_list.go` (shadowed display)

---

## High-Level Technical Design

The merged-install flow, end to end:

```
ainfra install
  └─ load layers:
       team (via extends:)  ┐
       repo (ainfra.yaml)   ├─ merge with existing precedence
       user (XDG personal)  ┘
  └─ resolve → merged manifest + lockfile
  └─ partition resolved entries by install target:
       repo-rooted entries     → write to repo/.claude/ + .mcp.json
       user-only entries       → write to ~/.claude/
  └─ update both applied ledgers:
       repo  : .ainfra/applied.lock
       user  : ~/.config/ainfra/applied.lock
  └─ if repo manifest exists AND staleness_warning != false:
       ensure SessionStart hook is in .claude/settings.json
  └─ check ainfraVersion: against binary version → warn on mismatch
```

> *This illustrates the intended flow and is directional guidance for
> review, not implementation specification.*

The SessionStart hook itself is a one-shot:

```
SessionStart hook runs:
  manifestHash := hash(ainfra.yaml + ainfra.personal.yaml)
  appliedHash  := read .ainfra/applied.lock → manifestHash field
  if manifestHash != appliedHash:
    print "⚠ ainfra state is stale; run `ainfra install` to refresh"
```

---

## Implementation Units

### U1. User-global applied ledger; ledger split

**Requirements:** A1, A4; F4, F5; AE5, AE7
**Dependencies:** none
**Goal:** Introduce a second applied ledger at `~/.config/ainfra/applied.lock`
that tracks user-global file ownership. The existing repo-local ledger at
`<repo>/.ainfra/applied.lock` continues to track repo-local files.

**Files:**
- `internal/provider/applied.go` (modify — accept a scope parameter)
- `internal/provider/applied_test.go` (modify)
- `internal/provider/scope.go` (new — defines `LedgerScope` enum: Repo, User)
- `cmd/ainfra/xdg.go` (new — resolves `$XDG_CONFIG_HOME/ainfra/` with fallback to `~/.config/ainfra/`)
- `cmd/ainfra/xdg_test.go` (new)

**Approach:** Today `applied.go` reads/writes `<dir>/.ainfra/applied.lock`.
Generalize: take a `LedgerScope` + base path. Repo scope = today's behavior.
User scope = `$XDG_CONFIG_HOME/ainfra/applied.lock` (fallback `~/.config/ainfra/applied.lock`).
The on-disk format is unchanged.

**Patterns to follow:** `internal/provider/applied.go` for the existing ledger
read/write. `internal/lockfile/io.go` for filesystem-resilient read (treat
missing as empty).

**Test scenarios:**
- A user-scope ledger at `~/.config/ainfra/applied.lock` round-trips: write entries, read them back, no loss.
- Missing user-scope ledger reads as an empty ledger (no error).
- `XDG_CONFIG_HOME` set to a custom path resolves there; unset falls back to `~/.config/`.
- Per-repo ledger continues to work unchanged (regression).

**Verification:** `go test ./internal/provider/...` passes; existing repo-ledger callers compile without behavior change.

---

### U2. `ainfra install` merges all layers and writes to both targets

**Requirements:** A1, A2, A4; F3, F4, F6; AE5, AE6, AE8
**Dependencies:** U1
**Goal:** `ainfra install` reads every available layer (team via `extends:`,
repo `ainfra.yaml`, XDG personal) and writes to BOTH `repo/.claude/` (for
repo-declared entries) AND `~/.claude/` (for user-only entries). In a
non-ainfra directory, it acts on the user layer alone.

**Files:**
- `cmd/ainfra/commands.go` (modify — `runApply` / `runInstall` body)
- `cmd/ainfra/cmd_install_test.go` (modify; add new scenarios)
- `internal/manifest/load.go` (modify — always attempt XDG personal load)
- `internal/manifest/load_personal_test.go` (modify)
- `internal/provider/orchestrator.go` (modify — accept dual-target write paths)
- `internal/provider/orchestrator_test.go` (modify)

**Approach:** The install path today resolves a merged lockfile, then writes
each channel's payload under `<repo>/.claude/` (or `<repo>/.mcp.json`). Add a
partitioning step: each resolved resource carries its origin layer; if origin
is "user-personal AND no repo entry of the same id exists", route writes to
`~/.claude/<channel>/<id>/` (or the right user-scope target). Everything else
keeps today's repo-rooted target. When there's no `ainfra.yaml` at all,
`runInstall` still proceeds: it loads only the XDG personal layer and writes
only to `~/.claude/`. No error.

**Patterns to follow:** `internal/provider/orchestrator.go` for the per-channel
apply loop; `internal/provider/claudecode/skills.go` for the channel-specific
write path.

**Test scenarios:**
- *Covers AE5.* Repo with `ainfra.yaml` + a user-personal layer: `install`
  writes repo entries to `repo/.claude/` and user entries to `~/.claude/`
  in one move.
- *Covers AE6.* Empty directory with no `ainfra.yaml` but a user-personal
  layer exists: `install` exits 0, prints "N user-level entries installed",
  writes only to `~/.claude/`.
- Empty directory with no `ainfra.yaml` AND no user layer: `install`
  prints "Nothing to do." and exits 0.
- A repo-layer skill and a user-layer skill with the same id: repo wins;
  the user-layer version is NOT written to `~/.claude/` (would shadow).
- A non-ainfra-managed file already exists in `~/.claude/skills/foo/`:
  ainfra refuses to overwrite, errors clearly with a hint to remove or
  reclaim.

**Verification:** A fresh `~/` plus a tempdir-only repo show both write
locations end-to-end after `ainfra install`. The non-ainfra-directory case
produces a working `~/.claude/` install.

---

### U3. Cleanup of user-global files when entries leave the user layer

**Requirements:** F4; AE7
**Dependencies:** U1, U2
**Goal:** Removing a skill from `personal.yaml` and running `ainfra install`
removes the materialized files from `~/.claude/skills/<id>/`, but only if
ainfra owned them (tracked via the user-global ledger).

**Files:**
- `internal/provider/orchestrator.go` (modify — extend delete-diff against the user ledger)
- `internal/provider/orchestrator_test.go` (modify)
- `internal/provider/claudecode/skills.go` (modify — accept a base path so user-scope deletes hit `~/.claude/skills/<id>/`)

**Approach:** Apply's delete branch already removes repo-local files when an
entry leaves the lockfile. Extend the diff to consult the user-global ledger
when the entry's origin layer is user-personal. Never touch files not in the
ledger.

**Patterns to follow:** the existing delete path in `internal/provider/orchestrator.go`.

**Test scenarios:**
- *Covers AE7.* Personal layer declares skill X → install → remove X from
  personal.yaml → install again → `~/.claude/skills/X/` is gone.
- A user manually created `~/.claude/skills/Y/` before ainfra ever ran:
  ainfra does not touch it (not in the ledger).
- A skill that was repo-scoped is moved to the user layer: install,
  repo's `.claude/skills/<id>/` cleans up; `~/.claude/skills/<id>/`
  appears. No orphan in repo.

**Verification:** A round-trip add+remove on a personal-layer skill leaves
`~/.claude/` byte-equal to its pre-add state.

---

### U4. `ainfra list` shows shadowed entries

**Requirements:** F6; AE8
**Dependencies:** U2
**Goal:** When `ainfra list` enumerates installed entries, mark entries where
a repo declaration is shadowing a user-personal-layer entry of the same id.

**Files:**
- `cmd/ainfra/cmd_list.go` (modify — compute shadow set; add column or annotation)
- `cmd/ainfra/cmd_list_test.go` (modify)

**Approach:** Today `cmd_list.go` reads the merged lockfile and prints one
row per entry. Build a sibling map from the same merge inputs that captures
which entries had multiple-layer declarations and which won. Display the
losers with an indicator (e.g., `(shadowed by repo)`).

**Test scenarios:**
- *Covers AE8.* Same skill id in repo + user layer: `list` prints two rows
  (or one with annotation) — the repo entry as active, the user entry as
  shadowed.
- No collision: `list` output unchanged from today.
- `list --json` includes a `shadowedBy` field on shadowed rows.

**Verification:** A repo that intentionally overrides a user-personal skill
makes the relationship visible at a glance.

---

### U5. SessionStart staleness hook, installed by default

**Requirements:** A2, A3; F2; AE2, AE3, AE4
**Dependencies:** U2 (so the install path can emit the hook)
**Goal:** `ainfra install` writes a `SessionStart` hook into
`.claude/settings.json` of any repo it manages. The hook compares the
manifest hash to the applied-ledger's recorded hash and prints one stderr
line *only when they differ*. Repos opt out with `staleness_warning: false`.

**Files:**
- `internal/manifest/schema.go` (modify — add top-level `StalenessWarning *bool` field)
- `internal/manifest/schema_test.go` (modify)
- `internal/provider/claudecode/hooks.go` (modify — built-in hook emission)
- `internal/provider/claudecode/hooks_test.go` (modify)
- `cmd/ainfra/hook_staleness.go` (new — the actual check, callable as a subcommand or built-in)
- `cmd/ainfra/hook_staleness_test.go` (new)
- `internal/manifest/load_personal.go` (modify if needed to surface `staleness_warning`)

**Approach:** The hook command itself is a small ainfra subcommand —
something like `ainfra _staleness-check` (hidden, prefixed with `_` to
indicate "internal"). It reads the manifest, computes its hash, compares
to the applied-ledger entry, prints one stderr line if they differ, exits 0
either way (the hook should never block Claude). The reconciler's hook
provider always emits this hook unless the manifest sets
`staleness_warning: false`.

**Execution note:** Start with a failing test that asserts the hook line
appears in `.claude/settings.json` after a clean install with no manifest
opt-out. Then make the reconciler emit it.

**Test scenarios:**
- *Covers AE2.* After `install`, edit `ainfra.yaml` (manifest hash changes)
  but don't re-install. Run the hook command directly. It prints one stderr
  line containing "stale" and the suggested command.
- *Covers AE3.* Clean state: hook prints nothing and exits 0.
- *Covers AE4.* `ainfra.yaml` has `staleness_warning: false`. After
  install, the hook is NOT in `.claude/settings.json`.
- The hook's exit code is always 0 (Claude doesn't get blocked).
- The hook command can be invoked manually (`ainfra _staleness-check`) and
  produces the same output.
- A repo with no `ainfra.yaml`: `install` still works (user-only mode),
  no hook is installed anywhere.

**Verification:** Opening Claude in a stale repo produces a visible
warning in the first session; opening in a clean repo produces no extra
output.

---

### U6. `ainfraVersion:` manifest field; warn on mismatch

**Requirements:** A2; F7; AE9
**Dependencies:** U2
**Goal:** A repo's `ainfra.yaml` can declare `ainfraVersion: 0.2.0`. On
install, ainfra compares the running binary version to that declaration
and warns if they don't match.

**Files:**
- `internal/manifest/schema.go` (modify — add top-level `AinfraVersion string`)
- `internal/manifest/schema_test.go` (modify)
- `cmd/ainfra/commands.go` (modify — version check inside install)
- `cmd/ainfra/cmd_install_test.go` (modify)
- `internal/version/version.go` (read-only)

**Approach:** Tiny addition: schema gets a new optional field; install
reads it during preflight; if set and not equal to `version.Version`, print
a one-line warning to stderr and continue. Constraint matching (`>=0.2,<0.3`)
is deferred — exact-match is enough for v1.

**Test scenarios:**
- *Covers AE9.* `ainfraVersion: 0.2.0`, binary is `0.1.5`: install
  proceeds, stderr contains a one-line warning naming both versions and
  the upgrade hint.
- `ainfraVersion: 0.2.0`, binary is `0.2.0`: no warning.
- Missing field: no warning (backward-compat default).
- `ainfra _staleness-check` (the hook) does *not* version-mismatch warn
  again — only `install` does, so the user sees it at most once per session.

**Verification:** A repo with a future version pin warns the current
binary on every install.

---

### U7. Refresh README, quickstart, using-ainfra skill, and showcase manifest

**Requirements:** A1, A2, A3; AE11
**Dependencies:** U2, U5, U6 (so docs match real behavior)
**Goal:** All public docs teach the merged-install model, the SessionStart
hook, and `ainfraVersion:`.

**Files:**
- `README.md` (modify)
- `docs/quickstart.md` (modify)
- `skills/using-ainfra/SKILL.md` (modify)
- `ainfra.yaml` (modify — add `ainfraVersion:` and possibly a personal-layer comment)
- `docs/reference/design.md` (modify — note the dual-target install + ledger split in the architecture section)

**Approach:**

- **README:** One new sentence in the "Why ainfra" section about the
  SessionStart hook. Update the Commands table to note install's expanded
  behavior. No new verbs to document.
- **Quickstart:** Add a "Personal skills everywhere" section showing the
  XDG personal layer. Show what happens when running install in a
  non-ainfra directory.
- **using-ainfra skill:** Update the four-commands table to reflect that
  `install` is the only relevant verb; add a note about the staleness
  hook so AI agents don't think it's noise.
- **Showcase ainfra.yaml:** Add an `ainfraVersion:` line; comment showing
  what `staleness_warning: false` looks like (commented out).

**Test scenarios:** none (pure docs).

**Verification:** A reader following only `docs/quickstart.md` can:
1. Install ainfra.
2. Create a `~/.config/ainfra/personal.yaml` with one skill.
3. Run `ainfra install` in any directory and see it applied.
4. `cd` into an ainfra-managed repo and see both layers active.

---

## System-Wide Impact

- **Backwards compatibility:** Every existing `ainfra install` invocation
  in an onboarded repo continues to produce the same files in the same
  places. The new behavior is purely additive (user-global writes happen
  when a user layer is present; nothing changes when it isn't).
- **Lockfile schema:** Unchanged. The applied ledger gets a sibling at
  `~/.config/ainfra/applied.lock`; the in-repo one is unchanged.
- **Manifest schema:** Two new optional top-level fields:
  `staleness_warning: bool` and `ainfraVersion: string`. Both default to
  "absent = today's behavior."
- **CI integrations:** No change to `install --dry-run --strict`. The
  SessionStart hook never affects exit codes.
- **`.claude/settings.json`:** Now carries one extra hook entry by default.
  Repos that already had hooks see the new one alongside. Repos that
  opt out are byte-equal to today.
- **Multi-machine consistency:** Without `ainfraVersion:` pinning,
  teammates can produce slightly different lockfiles when running
  different ainfra versions. With pinning, mismatches are warned at install.

---

## Risks

| Risk | Severity | Mitigation |
|---|---|---|
| Writing to `~/.claude/` could clobber files the user installed manually via Claude Code's own skill mechanism or by hand | High | Ledger-tracked ownership. ainfra refuses to overwrite a file not in the user-global ledger; errors clearly with the offending path and a hint to either move the file out of the way or `ainfra install --adopt` (deferred). |
| The SessionStart hook adds startup latency to every Claude session in an ainfra repo | Low | The hook's hash compare is on the order of a few ms (read a manifest file, hash it, compare a string). Default opt-out for repos that care. Re-measure if anyone reports latency. |
| Users may not realize the SessionStart hook is silently keeping them informed and try to disable it not knowing what it does | Low | One-line comment in the auto-emitted `settings.json` block points at docs. Quickstart explicitly explains the hook. |
| `ainfraVersion:` mismatch warning could spam onto an early-adopter team where versions drift across teammates | Low | Warning is one stderr line per `install` call. `AINFRA_QUIET=1` (already exists for deprecation warnings) suppresses it. Acceptable. |
| The XDG path resolution differs across operating systems and shells | Medium | `cmd/ainfra/xdg.go` centralizes the resolution and has tests for `XDG_CONFIG_HOME` set/unset. On Windows (out of scope today; ainfra hasn't shipped Windows support), defer. |
| A user with a heavy personal layer hits the merged-install slowly | Low | Same code path as today's repo install. Personal layers in practice are 1–5 entries. Re-measure if it becomes painful. |
| Cleanup deletes a user-edited file because the ledger said ainfra wrote it | Medium | The ledger records a content hash. On delete, ainfra checks the file's current hash against the recorded one; if they don't match, refuses to delete and warns with the hash diff. Same behavior the repo-local apply has today. |

---

## Verification Strategy

End-to-end demo (also smoke-tests the plan's full scope):

1. Fresh `~/`, fresh tempdir for the repo.
2. `mkdir -p ~/.config/ainfra && cat > ~/.config/ainfra/personal.yaml << EOF`
   declaring one personal skill.
3. `cd /tmp && ainfra install` — confirms one user-level entry installed
   to `~/.claude/skills/<id>/`; no repo writes.
4. `cd <tempdir>/repo-a` (has `ainfra.yaml` with `ainfraVersion: 0.99.0`
   and one repo-scoped skill).
5. `ainfra install` — stderr contains the version-mismatch warning;
   stdout shows both the repo skill (to `repo-a/.claude/skills/<id>/`)
   and the user skill (already in `~/.claude/skills/<id>/`).
6. Open `.claude/settings.json`: the staleness hook is present.
7. `ainfra _staleness-check` — exit 0, no output (clean).
8. Edit `repo-a/ainfra.yaml` without running install. Run the hook again
   — stderr has the staleness warning.
9. Open `repo-a/ainfra.yaml`, set `staleness_warning: false`, run
   `ainfra install`. Check `.claude/settings.json`: the hook is gone.
10. Remove the user-personal skill from `~/.config/ainfra/personal.yaml`,
    re-run `ainfra install` (in any directory): the materialized file is
    cleaned up from `~/.claude/skills/<id>/`.
11. `ainfra list` from `repo-a` with a same-id skill in both layers:
    shadowed user-layer entry shows the annotation.

Anything that passes 1–11 without manual intervention is shippable.

---

## Deferred to Implementation

- Exact phrasing of the staleness warning ("⚠ ainfra state is stale" vs
  "ainfra: your manifest has changed since the last install" vs
  something tighter). Final wording during U5.
- Exact phrasing of the version-mismatch warning. Final wording during U6.
- Whether `ainfra _staleness-check` is the hook command's literal name
  vs. a different convention for "internal subcommand."
- The exact one-line summary printed by install in non-ainfra-directory
  mode ("N user-level entries installed" — wording TBD).
- Whether `ainfra list --shadowed` is a separate flag or always-inline.
  Pick the simpler form during U4.
- Granularity of the per-entry origin-layer tracking in the resolved
  lockfile: today entries carry a `Layer` field; planning may need to
  refine its meaning to disambiguate "user-personal local" vs "team via
  extends." Confirm during U2.
- Adoption flow: `ainfra install --adopt` to take ownership of a
  manually-placed file. Out of scope here; surfaces as a hint in the
  refusal message.
