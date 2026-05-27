---
title: "CLI shape: commit to the package-manager mental model"
status: ready-for-planning
created: 2026-05-27
type: brainstorm
tier: standard
---

# CLI shape: commit to the package-manager mental model

## Problem frame

ainfra's current CLI is shaped like Terraform: `init`, `validate`, `lock`, `plan`,
`apply`, `check`, plus subscriber/secret sidecars (`publish`, `installer`, `exec`,
`sync`, `history`, `schema`). That shape was inherited, not chosen. When the
verbs are audited against actual moments in a user's day, most don't earn their
keep — `plan` duplicates `apply --dry-run`, `validate` duplicates the first half
of `lock`, `check` is structurally `plan` re-skinned, `sync` is a strict subset
of what `apply` already does internally, `schema` could be a committed file.

The mental model that fits the product is **package manager**, not infrastructure
provisioner. ainfra has a declarative manifest of dependencies (MCP servers,
hooks, commands, skills), a content-hashed lockfile, and a machine-local
reconcile — the same shape as `npm`, `brew`, `apt`. The plan is to commit to
that mental model end-to-end: rename `apply` → `install`, add the package-manager
mutation verbs ainfra is conspicuously missing (`add`, `remove`, `update`,
`outdated`, `list`), drop the verbs that exist only because Terraform has them.

## Actors

- **A1. Repo author** — initially sets up `ainfra.yaml` for a team. Runs `init`
  once per repo lifetime.
- **A2. Repo contributor (engineer)** — edits the manifest or runs CLI mutations
  to add/remove dependencies; runs `install` after pulling, runs `outdated` to
  see what could be bumped. The primary daily user.
- **A3. Onboarding teammate** — clones a repo, types one command, gets a working
  AI tool environment. Should never need to learn more than one verb.
- **A4. CI** — runs a non-interactive check that the live tree matches the
  lockfile. Needs a clean exit code and a non-zero on drift.
- **A5. Subscriber (non-engineer)** — out of scope for the front-page surface
  but still a real, rare consumer of the published artifact. Should not see any
  CLI verb dedicated to them on `ainfra --help`.

## Key flows

- **F1. First-time install.** A3 clones a repo, types `ainfra install`. The
  command resolves the manifest if no lock is current, previews changes, asks
  for confirmation, and reconciles. One verb, one decision.
- **F2. Add a dependency.** A2 wants a new MCP server. Today they hand-edit
  YAML, then run `lock`, then run `apply`. Goal: `ainfra add mcp github` does all
  three in one move and prints the resulting diff.
- **F3. Remove a dependency.** Inverse of F2.
- **F4. Check what's outdated.** A2 runs `ainfra outdated`. Lists every entry
  whose source has a newer resolvable version, with current and candidate
  versions side-by-side. No mutation.
- **F5. Bump versions.** `ainfra update <name>` bumps one entry; bare
  `ainfra update` bumps all. Lockfile changes; install runs.
- **F6. Inspect what's installed.** `ainfra list` shows every entry, layered
  (repo vs personal), with one line per entry. Like `npm list --depth=0`.
- **F7. Drift gate in CI.** A4 runs a single command that exits non-zero on any
  divergence between manifest, lockfile, and live tree. Today this is `check`.
  In the new shape, it's the same capability accessible via the `install`
  family — either `install --dry-run --strict` or, if the affordance test
  proves users prefer a distinct verb, `check` survives as a CI-friendly alias.
- **F8. Author publishes an artifact for subscribers (A5).** Code path stays;
  invoked via a hidden verb or flag, never front-page.

## Acceptance examples

- **AE1.** A new contributor clones a repo that has `ainfra.yaml`, runs
  `ainfra install`, sees a preview, confirms, and ends up with a working
  `.mcp.json` and `.claude/` tree. They learned exactly one verb.
- **AE2.** `ainfra add mcp github` adds a new `mcpServers.github` entry to
  `ainfra.yaml`, regenerates `ainfra.lock`, prints the diff, and reconciles.
  Re-running it is idempotent (no-op).
- **AE3.** `ainfra add --personal mcp local-fs` writes to
  `ainfra.personal.yaml`, never to the committed file.
- **AE4.** `ainfra add mcp github` without an existing manifest fails cleanly
  with "no ainfra.yaml here — run `ainfra init` first."
- **AE5.** `ainfra list` prints a stable table of every channel entry with the
  layer (repo / personal / team) and current version.
- **AE6.** `ainfra outdated` is read-only and exits 0 even when entries are
  stale. `--strict` exits non-zero when anything is stale (CI shape).
- **AE7.** `ainfra remove mcp github` removes the entry from
  `ainfra.yaml`, re-locks, and removes the server from `.mcp.json`. A
  subsequent `ainfra install` is a no-op.
- **AE8.** `ainfra install --dry-run` produces the same diff that today's
  `ainfra plan` produces.
- **AE9.** `ainfra --help` shows ≤ 8 verbs. Subscriber-mode verbs are absent
  from the overview but still callable.
- **AE10.** Old verbs (`apply`, `plan`, `check`, `validate`, `lock`) continue
  to work as hidden aliases for one minor release with a one-line deprecation
  note printed to stderr.

## Decisions

1. **Mental model: package manager.** Confirmed by the user during this
   brainstorm. Every verb is justified against an npm/brew/apt habit, not a
   Terraform one. Verbs without a real package-manager equivalent are cut.

2. **`apply` is renamed to `install`** as the primary mutation verb. The old
   verb stays as a hidden alias for one release.

3. **The front-page surface is 8 verbs:** `install`, `add`, `remove`, `update`,
   `outdated`, `list`, `init`, `version`. Diagnostic verbs collapse into flags
   on `install` (`--dry-run`, `--strict`) or into `outdated`.

4. **`add` / `remove` take an explicit channel argument** as the first positional
   (`ainfra add mcp github`, `ainfra remove command audit`). Confirmed by the
   user. Registry-based shorthand (`ainfra add github`) is deferred until a
   curated registry exists. Source-string shape
   (`ainfra add github:anthropics/skills/pdf`) is the natural form for skills
   and commands and is allowed as a second positional once the channel is
   given.

5. **Mutation verbs touch the YAML.** `ainfra add mcp github` writes the entry
   into `ainfra.yaml` (or `ainfra.personal.yaml` with `--personal`). The YAML
   stays the source of truth; the CLI becomes the primary editor. Hand-editing
   continues to work and is the supported path for entries the CLI can't
   express (templates with complex `params:` blocks, hooks with embedded shell,
   secrets with referenced env).

6. **`lock` stays as a hidden verb, not a front-page verb.** Reason: the only
   real use case is CI gating ("regenerate the lock from the manifest, fail if
   the working tree changes"), and CI users will find it. `install` auto-locks
   when the manifest is newer than the lock; bare humans never need `lock`.

7. **`check` is folded into `install --dry-run --strict`** as the CI shape.
   `check` survives as a hidden alias because CI scripts already exist. If real
   users push back ("I want a separate verb for read-only drift"), restore it;
   the underlying capability is identical either way.

8. **Subscriber mode (`publish`, `installer`) stays as code paths but never on
   the front page.** Confirmed "real but rare" by the user. Reachable via flags
   on a hidden verb (or via the existing hidden `publish` verb). The front page
   should not pay the cognitive cost for a workflow most users never touch.

9. **`exec`, `sync`, `history`, `schema` are dropped.**
   - `sync` is a strict subset of what `install` does at the end of its run.
   - `exec` exists only for binaries that don't read `settings.local.json`; no
     concrete user has been named.
   - `history` is `cat .ainfra/history.jsonl` with a table on top. Keep the log
     file, drop the verb.
   - `schema` becomes a committed `ainfra.schema.json` referenced from the
     `$schema:` line in `ainfra.yaml`. `validate --print-schema` keeps working
     for users who prefer regenerating.

10. **Old verbs remain callable as hidden deprecation aliases for one release.**
    No surprise breakage; one stderr line prompting the new shape. Removal
    target: 0.2.0.

## Success criteria

- `ainfra --help` lists ≤ 8 verbs.
- 100% of acceptance examples pass.
- A new contributor with no ainfra exposure can clone a repo with an
  `ainfra.yaml`, run `ainfra install`, and reach a working tree without reading
  documentation.
- `ainfra add mcp <name>` and `ainfra remove mcp <name>` round-trip cleanly:
  `ainfra add X && ainfra remove X` produces a zero diff against HEAD.
- CI users have a single command (`install --dry-run --strict` or `check`) that
  exits non-zero on drift and zero otherwise.
- Subscriber-mode users notice nothing — `publish` and `installer` still
  produce byte-equal artifacts.

## Scope boundaries

### Deferred for later

- **Registry-aware `add` shorthand** (`ainfra add github` without a channel).
  Requires building or adopting a curated registry of well-known MCP servers,
  skills, etc. Real value but real cost — fits a later plan once the explicit
  form has flushed out the data model.
- **Interactive `add` flow** (TUI menus, fuzzy search). Nice to have; not
  load-bearing.
- **Template-aware `add`.** Adding an instance of an existing template via the
  CLI (`ainfra add mcp analytics-db --template mysql-over-ssh-tunnel
  --param host=...`) is doable but deferred until basic non-templated `add`
  lands.
- **`update` strategy options** (latest, latest-minor, latest-patch, semver
  range). Start with "bump to latest resolvable"; refine later.

### Outside this product's identity

- Becoming an MCP runtime / gateway. ainfra dispatches to gateways, runs none.
- Replacing secret managers. ainfra holds references, never values.
- Becoming a configuration drift remediator that auto-applies in the
  background. ainfra runs when invoked; no daemons, no watchers.
- Becoming a service manager for the running MCP servers themselves (lifecycle,
  restart, health probes). That's the harness's job (Claude Code, Codex,
  Cursor, etc.); ainfra writes the config they read.

### Not happening in this round (deferred to follow-up implementation work)

- Tighten-only layer inheritance (APM-style narrowing-only org → repo → personal
  merge). Conceptually distinct; needs its own design pass.
- APM-style security defaults (hidden-Unicode scan on install, transitive-MCP
  trust prompts, overwrite consent). Deferred to a separate plan; affects
  `install` not the verb set.

## Dependencies / assumptions

- **Assumption:** the primary daily user is an engineer who is comfortable in a
  terminal and is the target audience for package-manager ergonomics. The
  "subscriber" persona (non-engineer running Claude Desktop) is real but rare
  per the user, and not in this scope.
- **Assumption:** ainfra is pre-1.0. Rename + alias + one-release deprecation is
  acceptable; we are not constrained to additive-only changes.
- **Dependency:** every channel needs a stable way to identify an entry by name
  (`mcp github`, `command audit`, `skill pdf-processing`). For inline entries
  that's the manifest key; for sourced entries the source URL is the canonical
  ID. This already exists in the lockfile schema.
- **Dependency:** YAML round-tripping that preserves comments and ordering for
  `add` / `remove`. Standard Go libraries (`go-yaml`'s `yaml.v3`) support this;
  needs verification before planning.

## Open questions

- **Should `add` print the resulting diff and confirm, or apply silently?**
  npm runs install silently; brew shows a brief summary. Default to printing a
  one-line summary + the diff and asking for confirmation only on first run per
  channel — but this is a design call for the implementer.
- **Should `update` accept a version pin (`ainfra update mcp github@0.7.0`) or
  always bump to latest?** Defer to implementation.
- **What's the right behavior of `add` when the channel name collides with an
  existing entry?** Likely: error with "already exists; use `update`". Confirm
  during planning.
