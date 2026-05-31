# ainfra plugin build/release — design

Date: 2026-05-31
Status: approved (design); pending implementation plan
Branch: worktree-mossy-honking-creek

## Problem

`claude-config` is simultaneously the team's ainfra manifest **and** the source of
the `tvt-config` Claude Code plugin. ainfra already manages the *consumer* side of
plugins (register a marketplace, install/update/uninstall a plugin via the `claude`
CLI). It does **not** help author or release the team's own plugin:

- `.claude-plugin/plugin.json` is hand-edited.
- The `tvt-config` entry in `.claude-plugin/marketplace.json` is hand-maintained.
- The version bump is a manual checklist step (README: "Bump `.claude-plugin/plugin.json` version").

The sharp failure mode: someone edits a skill but forgets to bump the version, so
Claude Code sees the same version and **teammates never receive the change**. Nothing
catches this today.

## Goal

Make ainfra the manager of the plugin's *wrapper* (manifest, version, validation,
marketplace listing) while the plugin *content* (skills/commands/hooks/.mcp.json) stays
hand-authored. Close the release loop in one tool: define → generate → validate →
version → (install, already exists).

Non-goal: generating skill/command/hook bodies, or managing third-party marketplace
re-exports.

## Decisions (locked during brainstorming)

1. **Version policy: explicit bump + drift guard.** The maintainer runs
   `ainfra plugin release --patch|--minor|--major`. ainfra refuses to release if the
   plugin content changed since the last release but no bump flag was given. Semver
   meaning stays under human control.
2. **Scope: `plugin.json` + own marketplace entry.** ainfra generates
   `.claude-plugin/plugin.json` and keeps the `tvt-config` entry in
   `.claude-plugin/marketplace.json` in sync. Third-party entries (`claude-ads`,
   `expo`, `compound-engineering`, `higgsfield`) are read, preserved, and rewritten
   untouched. Content directories are never modified.
3. **Surface: maintainer-only subcommand, not part of `apply`.** Consumers never
   regenerate or rebuild the plugin.
4. **Build layout: in-place.** The repo *is* the plugin; generate
   `.claude-plugin/plugin.json` at the repo root. No `dist/`.
5. **No auto-commit/tag** in v1. The change is carried through the normal `/pr` flow.

## Surface

New subcommand `ainfra plugin`, run from the plugin repo:

- `ainfra plugin build` — (re)generate `plugin.json` and sync the marketplace entry
  from the `plugin:` block. No version change, no guard. Safe to run anytime; useful
  to preview the generated manifest.
- `ainfra plugin release [--patch|--minor|--major]` — gated release:
  validate → drift-check → bump → regenerate → record new baseline.

Not wired into `apply` or `lock`.

## Manifest: new `plugin:` block

Added to `ainfra.yaml` (repo layer only):

```yaml
plugin:
  name: tvt-config
  description: "Team configuration for TVT-NL and AirHelp rail projects."
  marketplace: trein-vertraging          # marketplace.json entry to keep in sync
  author:     { name: Trein-Vertraging, url: https://github.com/trein-vertraging }
  repository: https://github.com/trein-vertraging/claude-config
  license:    UNLICENSED
  content: [ skills/, commands/, hooks/, .mcp.json ]   # paths the drift-guard hashes
```

- `version` is intentionally absent here. It lives in the generated `plugin.json`
  (single source of truth) and is only changed by `release`.
- `content` defines which paths count as a release-worthy change (the drift hash
  inputs). Defaults to the standard plugin payload dirs when omitted.

## Generated artifacts

### `.claude-plugin/plugin.json` (fully generated)
Rendered from the `plugin:` block. Mirrors the current hand-written shape, e.g.:

```json
{
  "name": "tvt-config",
  "version": "2.11.0",
  "description": "Team configuration for TVT-NL and AirHelp rail projects.",
  "author": { "name": "Trein-Vertraging", "url": "https://github.com/trein-vertraging" },
  "repository": "https://github.com/trein-vertraging/claude-config",
  "license": "UNLICENSED",
  "skills": ["./skills/"],
  "agents": []
}
```

### `.claude-plugin/marketplace.json` → `tvt-config` entry only (merge-in-place)
ainfra reads the whole file, replaces/updates **only** the object in `plugins[]`
whose `name == <plugin.name>`, and rewrites the file with all other entries
byte-preserved (2-space indent, matching the existing file, to keep diffs clean).
Synced fields: `name`, `description`. Preserved if already present, else taken from
the block: `source`, `author`, `repository`, `license`, `keywords`, `category`,
`tags`. The marketplace self-entry has **no** `version` field — version lives only in
`plugin.json` — so version is never written here.

## Drift guard + version flow (core)

Baseline `{version, contentHash}` is recorded in `ainfra.lock` under a new `plugin:`
section. `ainfra lock` and `ainfra apply` must preserve this section (they do not
compute it).

`contentHash` = stable hash over the `content:` paths (file contents + relative
paths, sorted; order-independent) plus the rendered `plugin.json` metadata fields
that affect consumers.

`ainfra plugin release` algorithm:

1. **Validate** — run `claude plugin validate` (via `env.Runner`). Abort on failure.
2. Recompute `contentHash`.
3. `hash == baseline.hash` and no bump flag → report "nothing changed since
   v<baseline.version>"; exit 0 (no-op).
4. `hash != baseline.hash` and **no bump flag → ERROR**: "content changed since
   v<baseline.version> — pass --patch / --minor / --major." Exit non-zero.
5. Bump flag given → bump `baseline.version` per semver, write `plugin.json` +
   marketplace entry with the new version, record new `{version, hash}` in the lock.

`ainfra plugin build` performs only the regeneration (steps that write `plugin.json`
+ marketplace entry from the current version), with no guard and no version change.

## Out of scope (YAGNI)

- Generating skill/command/hook bodies (stay hand-authored files).
- Touching third-party marketplace entries.
- Auto-commit / auto-tag (future `--commit` flag if wanted).
- Participating in `apply`.

## Implementation map

- `internal/manifest/types.go` — new `PluginBuild` struct + `Plugin *PluginBuild`
  field on `Manifest` (named to avoid clashing with the existing consumer-side
  `Plugin` install type). Validation in `validate.go` (name required, marketplace
  required, content paths exist).
- `internal/plugin/` (new package):
  - `hash.go` — content hashing over `content:` paths (pure, deterministic).
  - `build.go` — render `plugin.json`; merge the self-entry into `marketplace.json`
    preserving other entries.
  - `release.go` — the guard/bump state machine; reads/writes the lock baseline.
  - `semver.go` — minimal `--patch|--minor|--major` bump (no external dep unless one
    already vendored).
- `cmd/ainfra/cmd_plugin.go` — subcommand wiring with `build`/`release` and the bump
  flags. Mirrors `cmd_remove.go` structure.
- `internal/lockfile` — add the `plugin` baseline section; ensure existing
  lock read/write round-trips it unchanged.

## Testing (TDD)

Unit:
- hashing: stable across file ordering; changes when any content file changes; ignores
  unrelated files.
- `plugin.json` generation: golden-file comparison.
- marketplace merge: third-party entries preserved exactly; only the self-entry
  changes.
- release state machine: the four cases in the algorithm (no-op, guard-error, patch,
  minor/major).

Integration:
- `ainfra plugin release` against a fixture repo with a fake `claude` runner
  (`env.Runner` is already abstracted), asserting file outputs and exit codes.

## Validation / dogfooding gate (claude-config as the test case)

Before this feature is considered done, adopt it on the real `claude-config` repo:

1. Add the `plugin:` block to `claude-config/ainfra.yaml` describing the existing
   `tvt-config` plugin.
2. Run `ainfra plugin build` and **diff the generated `.claude-plugin/plugin.json`
   and the `tvt-config` marketplace entry against the committed hand-written
   versions.** Acceptance criterion: **no meaningful difference** (only stable
   formatting/normalization). This proves adoption is non-destructive.
3. Seed the `ainfra.lock` baseline at the current version (`2.11.0`) and confirm:
   - a no-op `release` reports "nothing changed";
   - editing a skill then `release` (no flag) triggers the drift guard;
   - `release --patch` bumps to `2.11.1` and updates both files + the lock baseline.

If step 2 shows unexpected differences, the generator must be adjusted to match the
existing manifest (or the difference explicitly accepted and documented) before
rollout.
