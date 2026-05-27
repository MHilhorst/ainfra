---
title: "Multi-repo activation + user-level layering, npm lessons applied"
status: ready-for-planning
created: 2026-05-27
revised: 2026-05-27
type: brainstorm
tier: standard
---

# Multi-repo activation + user-level layering, npm lessons applied

## Problem frame

ainfra is repo-first today. Each repo's `ainfra.yaml` materializes its own
skills/MCP servers/commands into `repo/.claude/` and `.mcp.json`. Claude Code
reads from `cwd`, so per-repo isolation already works — two repos can pin
totally different setups without interference (the same way `pyenv` gives
you per-project Python isolation).

Two friction points remain:

1. **The "did I install?" question.** Even when a repo is onboarded, there's
   no signal that the materialized state matches the current manifest. If a
   teammate updated `ainfra.yaml` and you pulled, you're running Claude
   against stale state until you remember to run install. No warning, no
   auto-detect, no hint in the session.

2. **The user-level gap.** ainfra's data model has three layers (team →
   repo → personal) but the `install` verb only acts when there's a repo
   `ainfra.yaml` to drive it. If you have a personal skill you want
   *everywhere*, including in repos that aren't ainfra-onboarded, there's
   nowhere to put it that takes effect — the XDG personal layer exists but
   doesn't materialize without a repo manifest pulling it in.

The npm community has spent a decade learning the right shape for this
class of problem. The redesign below applies those lessons.

## What npm taught us (and how this brainstorm responds)

| npm lesson | Applied to ainfra |
|---|---|
| Global installs become an anti-pattern when they're the easy path. `-g` got overused; users polluted globals; version mismatches followed. | Don't market user-global as the on-ramp. The recommended share path is a **team manifest included via `extends:`**. The personal/user layer is for *genuinely* user-specific things, not "everything everywhere." |
| The real win wasn't better globals — it was `npx`: project-local install made transparent at invocation time. | ainfra equivalent: a `SessionStart` hook that warns on staleness so users discover the issue *at the moment they care*, without owning a wrapper or shell hook. Optional `ainfra claude` wrapper for users who want auto-fix on launch. |
| npm separates the *config layering* axis (project > user > global, all merged) from the *install location* axis (local vs global, one-or-the-other). Not confusing them is a design win. | ainfra layering (team → repo → user-personal) always merges. The merged result writes to `repo/.claude/` for repo-declared entries, `~/.claude/` for user-only entries. One verb, two write paths, never confusing. |
| nvm + globals = lost tools when you switch Node version. Per-version-manager-version-pinning is needed. | `.ainfra-version` pins the binary version per repo so a teammate on a newer ainfra doesn't silently produce a different lockfile. Promoted from deferred to in-scope. |
| `npx` won because it removed a question users were tired of answering. | Same principle: the staleness hook removes "did I run install?" The user-merged install removes "which verb do I run for user-global?" Both delete questions, don't add verbs. |

## Actors

- **A1. Solo engineer with many repos.** Some onboarded, many not. Wants
  personal skills (note-taking helper, formatting hook, a favourite MCP
  server) to follow them everywhere without per-repo onboarding.
- **A2. Team engineer.** Pulls a teammate's manifest change; opens Claude;
  wants either no surprise (state up to date) or a clear warning (it isn't).
- **A3. New contributor.** First clone of a repo. `claude` works
  immediately; if state is stale, a one-line warning at session start tells
  them what to run.
- **A4. Repo without `ainfra.yaml`.** A repo the user works in that isn't
  onboarded. Their global personal skills still apply because they live in
  `~/.claude/`; ainfra never writes to that repo's tree.
- **A5. CI.** Wants a clean exit code answer to "is this checkout in sync
  with the manifest?" — already provided by `install --dry-run --strict`.

## Key flows

- **F1. Direct `claude` keeps working.** Typing `claude` (or `claude code`
  etc.) is unchanged. ainfra never intercepts it.
- **F2. Staleness is surfaced at session start.** ainfra installs a built-in
  `SessionStart` hook in every repo it manages. The hook runs a fast
  staleness check; on stale state it prints one line to the session
  (something like *"⚠️ ainfra state is stale; run `ainfra install`"*) and
  proceeds. Hook is opt-out per repo (`staleness_warning: false` in
  `ainfra.yaml`).
- **F3. One install verb for everything.** `ainfra install` reads every
  available layer (team via `extends:`, repo `ainfra.yaml`, user
  `$XDG_CONFIG_HOME/ainfra/personal.yaml`) and merges. The merged result
  writes to `repo/.claude/` for repo-declared entries and `~/.claude/` for
  entries that come only from the user layer. No `--user` flag, no
  separate verb.
- **F4. Run in a non-ainfra repo.** `ainfra install` with no repo manifest
  still does something useful: it applies the user layer to `~/.claude/`.
  No error, no required onboarding.
- **F5. Cleanup is symmetric.** Removing an entry from any layer's manifest
  cleans up its materialized files on the next install. Ownership is
  tracked in a user-global applied ledger (under `$XDG_DATA_HOME/ainfra/`)
  alongside the per-repo `.ainfra/applied.lock` that already exists.
- **F6. Conflict precedence.** When a user-personal skill and a repo skill
  have the same `id`, the repo wins (matches today's layer precedence). The
  user-global version is shadowed; `ainfra list` shows this explicitly so
  users aren't surprised.
- **F7. Binary version pinning.** A repo can declare `.ainfra-version` (or
  an `ainfraVersion:` field in `ainfra.yaml`); `ainfra install` warns if
  the binary version doesn't match. Prevents the nvm-globals trap where
  different teammates produce subtly different lockfiles.
- **F8. Opt-in wrapper for power users.** `ainfra claude` is shipped but
  is not the recommended daily verb. Useful for users who want
  "install-if-stale, then exec claude" in one move; everyone else uses the
  hook.

## Acceptance examples

- **AE1.** Typing `claude` in any directory works exactly as today —
  ainfra never intercepts the command.
- **AE2.** In an ainfra-managed repo with stale state, `claude` prints one
  one-line warning to the session before the first prompt, originating from
  the ainfra-installed `SessionStart` hook.
- **AE3.** In an ainfra-managed repo with current state, the warning is
  absent. (Or it prints "ainfra in sync" — a question for planning.)
- **AE4.** A repo can disable the warning with one line in `ainfra.yaml`
  (`staleness_warning: false` or equivalent). The hook is not installed in
  that repo's `.claude/settings.json`.
- **AE5.** `ainfra install` in a repo writes both repo-local files (to
  `repo/.claude/`) and user-only files (to `~/.claude/`) in one move.
- **AE6.** `ainfra install` in a directory with **no** `ainfra.yaml` reads
  only the user layer and writes to `~/.claude/`. Exit zero with a brief
  summary (e.g., "3 user-level entries installed").
- **AE7.** Removing a skill from `personal.yaml` and running `ainfra
  install` again removes the materialized files from `~/.claude/skills/`.
  ainfra never deletes a file it didn't write — verified against the
  user-global applied ledger.
- **AE8.** A user-personal skill and a repo skill with the same `id`: the
  repo's version wins. `ainfra list` shows both with the shadowed one
  marked.
- **AE9.** A repo with `ainfra-version: 0.2.0` warns on first install if
  the binary running is `0.1.x` (or vice-versa). The warning names the
  expected version and where to upgrade.
- **AE10.** `ainfra claude` (opt-in wrapper) runs `ainfra install
  --dry-run --strict` first; if stale, it offers to install (or installs
  automatically with `--yes`); then `exec`s claude with the resolved env.
- **AE11.** Documentation explicitly names the recommended share path —
  team manifest via `extends:` — and frames the user layer as
  "genuinely yours, on every machine you own, rare." Mirrors npm's
  current `-g` guidance.

## Decisions

1. **Don't intercept `claude`.** The canonical entry point stays
   unchanged. Users who type `claude` directly are first-class. Any
   "auto-activate" mechanism must coexist with that.

2. **Staleness lives in a `SessionStart` hook ainfra installs by
   default.** It's the lowest-friction way to surface the "did I install?"
   question — fires at the exact moment the user cares (session start),
   uses a mechanism ainfra already owns, doesn't require a wrapper or
   shell hook. Repos can opt out per-manifest.

3. **One `install` verb, no `--user` flag.** Reads every available layer
   (team → repo → user-personal), merges with the existing precedence
   table, and writes to `repo/.claude/` and `~/.claude/` as appropriate.
   In a non-ainfra directory, it acts on the user layer alone. The user
   thinks about *what skill belongs in which layer*, not *which verb to
   run*. This is the lesson from npm's `npx` win: delete questions, don't
   add verbs.

4. **Separate layering from install target.** Layering = where it's
   declared (team / repo / user-personal), all merged. Install target =
   where files land (`repo/.claude/` or `~/.claude/`). Two axes, never
   confused. Mirrors npm's clean separation between `.npmrc` config
   hierarchy and `node_modules` location.

5. **Lead with team manifests for sharing.** The documented recommendation
   for "we want skill X in every backend repo" is **`extends:` a team
   manifest**, not "drop it in your global personal layer." The user
   layer is for *personal* things (your note-taking helper) not *shared*
   things (your team's pgcli MCP). Matches npm's post-mortem on `-g`:
   reserve global for rare, deliberate uses.

6. **Promote `.ainfra-version` from deferred to in-scope.** The nvm
   lesson applies: when global state is layered, version-of-the-tool
   matters. Pinning the ainfra binary per-repo prevents "different
   teammates produce different lockfiles" silently.

7. **Wrapper exists but is opt-in.** `ainfra claude` ships as a
   convenience verb. It is not the recommended daily entry. The
   SessionStart hook covers the 80% case; the wrapper covers the "I want
   auto-fix, not just a warning" power user.

8. **The user-global applied ledger lives under `$XDG_DATA_HOME/ainfra/`.**
   Per-user, not per-repo. Tracks file ownership for safe cleanup.

## Success criteria

- A user with zero onboarded repos can still install their personal
  layer's skills/MCP via `ainfra install` and have them work everywhere
  Claude is launched.
- A user with mixed repos sees consistent behaviour: repo entries scoped
  to that repo, user entries everywhere.
- A user who pulls a teammate's manifest change and opens Claude
  immediately sees a one-line warning if their machine is stale. They
  did not need to remember anything.
- `claude` direct continues to work for users who never adopt the hook.
- Removing an entry from any layer cleans up its materialized files;
  no orphans.
- The README front page can plausibly add: *"ainfra warns you in
  Claude's first message when your config is stale."*

## Scope boundaries

### Deferred for later

- **direnv-style shell hooks on `cd`.** Real ergonomics win but its own
  design surface (security, latency, shell variants). The SessionStart
  hook addresses the underlying need; shell hooks are a separate-doc
  follow-up if users push for them.
- **Cross-repo aggregation (`ainfra workspace install`).** Bulk
  install/check across many repos in one move. Useful but orthogonal.
- **Tag/folder-based scoping** (e.g., "personal layer X applies only to
  repos under `~/work/`"). Smaller, niche. Revisit when demand shows.
- **Auto-init of `ainfra.yaml` in unmanaged repos.** Heavier. The
  user-layer-applies-everywhere model already covers the actual use case
  without forcing onboarding.
- **A staleness-only `ainfra status` verb.** `ainfra list` showing a
  staleness column is enough for now; a dedicated verb is a follow-up if
  scripting demand emerges.

### Outside this product's identity

- Becoming an MCP runtime / gateway.
- Replacing secret managers — references only, never values.
- A daemon that watches and auto-reapplies.
- A skills marketplace / discovery layer.
- Intercepting `claude` directly. Wrappers are opt-in; the canonical
  command stays untouched.

## Dependencies / assumptions

- **Assumption:** `~/.claude/` is the right user-global path on macOS and
  Linux for Claude Code. Verified today; needs re-verification for Codex
  and other targets.
- **Assumption:** Claude Code's `SessionStart` hook fires reliably enough
  to be the staleness surface. If it has latency or skip conditions, the
  fallback is `ainfra list` showing the same info on demand.
- **Assumption:** The XDG personal layer becomes the canonical
  "user-global" surface. Today it's named "personal" but conceptually it's
  "your machine, your scope." Naming may shift in planning (`user.yaml`?)
  but the data model is unchanged.
- **Dependency:** Existing layer-merge, apply, and ownership-tracking code
  paths. No new engine work; this is a new entry point + a ledger split +
  one new built-in hook.

## Open questions (resolved at planning)

- **Hook signal phrasing.** *"⚠️ ainfra state is stale; run `ainfra
  install`"* vs *"ainfra in sync"* on the clean case vs silent on clean.
  Planning-time decision; default probably silent-on-clean.
- **Conflict display in `ainfra list`.** How prominently to mark shadowed
  global entries. Inline column vs a separate `--shadowed` flag.
- **Cleanup safety net.** When `ainfra install` removes a skill from
  `~/.claude/skills/<id>/`, should it warn if the user hand-edited that
  directory since the last install?
- **`.ainfra-version` filename vs `ainfraVersion:` field.** `.python-version`
  is a separate file (auto-detected by pyenv); `package.json` embeds
  `engines.node`. Both shapes work. Pick one in planning.
- **Should the wrapper (`ainfra claude`) auto-confirm install?** Today's
  `ainfra install` prompts unless `--yes`. The wrapper's whole job is
  removing friction, so `--yes` should probably be implicit there, with
  `--no` to opt out.
