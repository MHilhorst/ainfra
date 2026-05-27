---
title: "feat: Commit ainfra CLI to the package-manager mental model"
status: active
created: 2026-05-27
type: feat
depth: standard
origin: docs/brainstorms/2026-05-27-cli-package-manager-shape-requirements.md
---

# feat: Commit ainfra CLI to the package-manager mental model

## Problem Frame

The brainstorm at `docs/brainstorms/2026-05-27-cli-package-manager-shape-requirements.md`
re-audited every verb in ainfra's CLI against actual moments in a user's day,
under the lens that ainfra is a package manager (npm/brew/apt), not an
infrastructure provisioner. Most current verbs fail that test:

- `apply` is `install` with the wrong name.
- `plan` duplicates `apply --dry-run`. Terraform has it because applies are
  destructive and slow; ainfra's aren't.
- `validate` duplicates the first half of `lock`.
- `check` is `plan` re-skinned for CI.
- `sync`/`exec` are subsets of what `apply` already does or have no concrete user.
- `schema` would be better shipped as a committed `ainfra.schema.json` file.
- `history` is `cat .ainfra/history.jsonl` with a table.

Conspicuously *missing* are the verbs every package manager has and every
user already knows: `add`, `remove`, `update`, `outdated`, `list`. Without
them, every dependency change requires hand-editing YAML and then running
two more verbs.

This plan commits to the package-manager mental model end-to-end in two
delivery slices: a read-only rename phase that hides or collapses the Terraform
debris and introduces the inspection verbs, and a mutation phase that adds the
manifest-editing verbs on top of a YAML round-trip foundation.

## Scope

**In scope**

- Rename `apply` → `install`. Keep `apply` as a hidden alias with a stderr
  deprecation note until 0.2.
- Fold `plan` into `install --dry-run`. Fold `validate` into `install --dry-run`'s
  syntax-check pass and into `lock`. Keep `plan` and `validate` as hidden
  aliases.
- Collapse `check` into `install --dry-run --strict` (CI shape). Keep `check`
  as a hidden alias.
- Add `ainfra list` and `ainfra outdated` as new top-level verbs.
- Add `ainfra add <channel> <id>`, `ainfra remove <channel> <id>`, and
  `ainfra update [<channel> <id>]` as manifest-editing verbs.
- Hide `sync`, `exec`, `history`, `schema` from the overview (already done
  on prior branch; reaffirmed here as part of the new shape).
- Pull in `goccy/go-yaml` (or equivalent) as a comment-preserving YAML
  round-trip library — `gopkg.in/yaml.v3` does not preserve comments cleanly
  through marshal/unmarshal.
- Refresh `README.md`, `docs/quickstart.md`, and `ainfra.yaml` to match the
  new front-page surface and reference `install`/`add`/`remove` in examples.

**Deferred for later** (carried verbatim from the brainstorm)

- Registry-aware `add` shorthand (`ainfra add github` without a channel).
  Requires building or adopting a curated registry of well-known MCP servers
  and skills.
- Interactive `add` flows (TUI menus, fuzzy search).
- Template-aware `add` (instantiating an existing template via flags).
- `update` strategy options (semver range, latest-minor, latest-patch).

**Outside this product's identity** (carried verbatim from the brainstorm)

- Becoming an MCP runtime / gateway.
- Replacing secret managers — ainfra holds references, never values.
- A drift remediator that auto-applies in the background.
- A service manager for the running MCP servers themselves.

**Deferred to Follow-Up Work**

- Tighten-only layer inheritance (APM-style narrowing-only).
- APM-style security defaults (hidden-Unicode scan, transitive-MCP trust
  prompts, overwrite consent). Conceptually orthogonal.
- Full removal of deprecated aliases at 0.2.0 — this plan ships the
  deprecation, not the removal.

## Key Technical Decisions

1. **Two delivery slices, one plan.** Phase 1 (rename + read-only + drops, U1–U4)
   and Phase 2 (mutation verbs, U5–U7) live in the same plan but ship as
   separate PRs. Phase 1 is independently shippable and lands the new
   front-page surface; Phase 2 builds on it without rework. Phase 3 (U8) is
   docs.

2. **Comment-preserving YAML library.** Adopt `goccy/go-yaml` for the
   round-trip path. `gopkg.in/yaml.v3` decoder works fine; it does not
   re-emit comments through marshal/unmarshal, which would silently delete
   every team's careful annotations the first time someone runs
   `ainfra add`. The current `yaml.v3` import stays for read-only paths
   (loading layers, computing manifest hash) — the new library is only used
   in the writer.

3. **`add` writes to the lowest-precedence non-template entry by default.**
   `ainfra add mcp github` writes to `ainfra.yaml`. `ainfra add --personal mcp
   local-fs` writes to `ainfra.personal.yaml`. Templated MCP instances are
   out of scope for CLI `add`; they require complex `params:` blocks and stay
   hand-edited.

4. **`add` performs a full reconcile by default.** The mental model is
   `npm install foo` — manifest change + install in one move. `--no-install`
   skips the reconcile (manifest write + lock only) for users who want to
   batch multiple `add` calls before applying. Same flag on `remove` and
   `update`.

5. **`update` bumps to the latest resolvable version.** Strategy variants
   (semver range, minor-only) are deferred. The single supported semantic
   is "the new lock would resolve as if no prior lock existed."

6. **`list` reads the merged lockfile, not the manifest.** What a user
   actually wants to know is "what's currently installed on this machine,"
   which is the lockfile + applied ledger, not the manifest's intent.
   Format: one line per entry, columns `channel · id · version · layer · status`.

7. **`outdated` is read-only and exits 0 by default.** `--strict` exits
   non-zero when anything is stale, for CI use. Mirrors `npm outdated` /
   `npm outdated --strict`.

8. **Deprecated aliases print a stderr line only on first call per process.**
   Prevents script spam. Suppression via `AINFRA_QUIET=1` or `--no-color`.

9. **No engine rewrite.** Resolution pipeline, lockfile schema, manifest
   schema, provider interface — all unchanged. The work is concentrated in
   `cmd/ainfra/` (new verb handlers), `internal/cli/` (help-text rendering,
   alias mechanism), and a new `internal/manifest/writer/` package for the
   YAML round-trip.

---

## High-Level Technical Design

The new CLI surface, end state:

```
ainfra install               reconcile manifest → machine (was: apply)
  --dry-run                  preview only (was: plan)
  --strict                   exit non-zero on drift (CI shape; was: check)
  --print-schema             emit JSON schema and exit (was: schema)

ainfra add <ch> <id> [src]   add entry → lock → install
  --personal                 write to ainfra.personal.yaml
  --no-install               skip reconcile

ainfra remove <ch> <id>      remove entry → lock → install
  --no-install               skip reconcile

ainfra update [<ch> <id>]    bump locked versions → install
  --no-install               skip reconcile

ainfra outdated              list entries with newer resolvable versions
  --strict                   exit non-zero when anything is stale

ainfra list                  one line per installed entry
  --channel <ch>             filter to one channel

ainfra init                  scaffold an ainfra.yaml (unchanged)
ainfra version               unchanged
```

Hidden aliases retained through 0.2: `apply`, `plan`, `check`, `validate`,
`schema`, `lock`, `sync`, `exec`, `history`, `publish`, `installer`. Each
prints one stderr line on first use per process pointing to the new shape.

> *This illustrates the intended surface and is directional guidance for
> review, not implementation specification. The implementing agent should
> treat it as context, not code to reproduce.*

---

## Implementation Units

### U1. Rename `apply` → `install`; fold `plan`/`validate`/`check` into `install --dry-run [--strict]`

**Requirements:** A2, A3, A4; F1, F7; AE1, AE8, AE9, AE10
**Dependencies:** none
**Goal:** Make `install` the primary verb. `plan`, `validate`, `check`, and
`apply` continue to work as hidden aliases that delegate to `install` with
appropriate flags and print one deprecation line per process.

**Files:**
- `cmd/ainfra/cmd_install.go` (new — extracted from `commands.go`)
- `cmd/ainfra/cmd_apply_alias.go` (new — thin alias)
- `cmd/ainfra/cmd_plan_alias.go` (new — thin alias adding `--dry-run`)
- `cmd/ainfra/cmd_check_alias.go` (new — thin alias adding `--dry-run --strict`)
- `cmd/ainfra/cmd_validate_alias.go` (new — thin alias to `install --dry-run`)
- `cmd/ainfra/main.go` (modify — register install + aliases)
- `cmd/ainfra/commands.go` (modify — `newPlanCommand`/`newApplyCommand`/`newCheckCommand` move to alias files; shared helpers stay)
- `internal/cli/command.go` (modify — add `DeprecatedFor string` field to `Command` for stderr message)
- `internal/cli/help.go` (modify — show deprecation note in per-command help, suppress from overview)
- `cmd/ainfra/cmd_install_test.go` (new)
- `cmd/ainfra/cmd_apply_alias_test.go` (new)
- `cmd/ainfra/dispatch_test.go` (modify — assert `--help` shows `install`, not `apply`)

**Approach:** `install` is the renamed body of today's `runApply`. The
`--strict` flag is added: with `--dry-run --strict` and a non-empty plan,
exit 1 instead of 0 (the existing dry-run path always exits 0). Aliases
share the same handler; they pre-set flags and emit a deprecation line via
a once-per-process latch keyed on the alias name.

**Patterns to follow:** `cmd/ainfra/cmd_validate.go` for the simple
alias-with-flag pattern (where `--print-schema` already routes to a separate
handler in the same command).

**Test scenarios:**
- `ainfra install` with a clean lockfile and a manifest delta produces the
  same exit code and stdout/stderr as today's `ainfra apply`.
- `ainfra install --dry-run` produces the same plan output as today's
  `ainfra plan` and exits 0 even with a non-empty plan.
- `ainfra install --dry-run --strict` exits 1 with a non-empty plan and 0
  with an empty one. *Covers AE10 (CI shape).*
- `ainfra apply --yes` still works, prints exactly one deprecation line to
  stderr, and produces a byte-equal stdout to `ainfra install --yes`.
- The deprecation line is printed exactly once even when `ainfra apply` is
  called twice in the same process via the test harness's in-process driver.
- `ainfra plan` prints a deprecation line and delegates to
  `install --dry-run`. *Covers AE8.*
- `ainfra check` prints a deprecation line and delegates to
  `install --dry-run --strict`.
- `ainfra validate` prints a deprecation line and delegates to
  `install --dry-run`.
- `ainfra --help` lists `install` and not `apply`/`plan`/`check`/`validate`.
  *Covers AE9.*

**Verification:** `go test ./...` passes; `ainfra --help` output matches the
new shape; the four hidden aliases each produce byte-equal artifacts to the
new verb form.

---

### U2. Hide remaining niche verbs and drop redundant ones

**Requirements:** A3; AE9
**Dependencies:** U1
**Goal:** `sync`, `exec`, `history`, `schema`, `lock`, `publish`, `installer`
are all hidden from `ainfra --help`. The four that the brainstorm dropped
(`sync`, `exec`, `history`, `schema`) print a one-line deprecation note on
first use, pointing to the replacement (or, for `history`, to the log file
path). The three subscriber/CI helpers (`lock`, `publish`, `installer`) stay
hidden without a deprecation note — they're useful, just niche.

**Files:**
- `cmd/ainfra/cmd_schema.go` (already `Hidden: true`; add `DeprecatedFor: "validate --print-schema"`)
- `cmd/ainfra/cmd_sync.go` (already `Hidden: true`; add `DeprecatedFor: "install (auto-syncs at end of run)"`)
- `cmd/ainfra/cmd_exec.go` (already `Hidden: true`; add `DeprecatedFor: ""` — drop without replacement, custom note: "no longer supported; use --no-install on install if you need YAML-only changes")
- `cmd/ainfra/cmd_history.go` (already `Hidden: true`; add note pointing at `.ainfra/history.jsonl`)
- `cmd/ainfra/cmd_history_test.go` (modify — assert deprecation prints)
- `cmd/ainfra/cmd_schema_test.go` (modify — assert deprecation prints)
- `cmd/ainfra/cmd_sync_test.go` (modify — assert deprecation prints)
- `cmd/ainfra/cmd_exec_test.go` (modify — assert deprecation prints)

**Approach:** Use the same once-per-process deprecation latch added in U1.
No behavior change beyond the stderr line.

**Test scenarios:**
- Each of `sync`, `exec`, `history`, `schema` prints exactly one deprecation
  line on first call.
- `--help` overview lists exactly: `init`, `install`, `add`, `remove`,
  `update`, `outdated`, `list`, `version` once Phase 2 lands. For Phase 1
  end state: `init`, `install`, `outdated`, `list`, `version`.
- Existing test fixtures invoking the hidden verbs continue to pass — none
  of the existing functionality is removed.

**Verification:** Help output matches the locked surface. All previously
passing tests still pass.

---

### U3. Add `ainfra list` and `ainfra outdated`

**Requirements:** A2; F4, F6; AE5, AE6
**Dependencies:** U1 (so the new front-page surface is in place)
**Goal:** Two new read-only verbs. `list` shows what's currently installed
on this machine, derived from the merged lockfile and applied ledger.
`outdated` shows what could be bumped.

**Files:**
- `cmd/ainfra/cmd_list.go` (new)
- `cmd/ainfra/cmd_list_test.go` (new)
- `cmd/ainfra/cmd_outdated.go` (new)
- `cmd/ainfra/cmd_outdated_test.go` (new)
- `cmd/ainfra/main.go` (modify — register the two new verbs)
- `internal/ui/table.go` (new — small text-table helper; or extend an existing renderer if there is one)

**Approach:**

`list`: Read `ainfra.lock` + `ainfra.personal.lock`, merge with the same
precedence used by `runPlan`, and print one row per entry with columns
`channel · id · version · layer`. `--channel <ch>` filters. `--json` prints
JSON Lines for scripting. No interaction with the live tree — this is what
the lockfile *says* is installed, which is the right answer for "what does
this repo declare for me right now."

`outdated`: For each entry whose version is resolvable (today: package-launched
MCP servers with a `version:` pin from npm), check the latest published
version against the locked version. Print a row with `channel · id · current · latest`.
`--strict` makes the exit non-zero when at least one row is printed. Non-resolvable
entries (template-resolved, inline, http transport) are silently skipped — they
have no concept of "newer."

Resolution path for `outdated`: reuse `internal/provider/pkg/` if it exposes
a "latest version" lookup; otherwise stub the lookup behind an interface and
return "unknown" for every entry in the first cut, with a follow-up unit
deferred for the npm-registry probe.

**Patterns to follow:** `cmd/ainfra/cmd_history.go` for the table-vs-JSON
output split.

**Execution note:** Build `list` first; its output is reused by `outdated`'s
test fixtures.

**Test scenarios:**
- `ainfra list` after a clean apply prints exactly the entries in the
  merged lockfile, sorted by `channel` then `id`.
- `ainfra list --channel mcp` filters to MCP servers only.
- `ainfra list --json` emits one JSON object per entry; the object keys
  match the column names.
- Personal-layer entries are shown with `layer=personal`. *Covers AE5.*
- `ainfra outdated` with all entries at latest exits 0 and prints "Up to
  date." *Covers AE6.*
- `ainfra outdated` with one stale entry exits 0 (read-only default) and
  prints the row.
- `ainfra outdated --strict` with one stale entry exits 1.
- Entries with no resolvable version concept (HTTP transport, templated,
  inline) are excluded from `outdated` output, not shown as "unknown."

**Verification:** Both verbs appear in `ainfra --help`. Output is stable
across runs (deterministic sort). CI usage of `outdated --strict` works as
documented.

---

### U4. YAML round-trip foundation: comment- and order-preserving manifest writer

**Requirements:** F2, F3, F5; AE2, AE3, AE7; dependency from the brainstorm's
"YAML round-tripping" requirement
**Dependencies:** none (independent foundation work)
**Goal:** Add a manifest writer that can read `ainfra.yaml` (and
`ainfra.personal.yaml`), apply a targeted edit (add or remove one entry under
one top-level channel key), and write back with comments, whitespace, and
key order preserved. This is the prerequisite for U5/U6/U7.

**Files:**
- `go.mod`, `go.sum` (modify — add `github.com/goccy/go-yaml`)
- `internal/manifest/writer/writer.go` (new)
- `internal/manifest/writer/writer_test.go` (new)
- `internal/manifest/writer/fixtures/` (new — golden YAML files for tests)

**Approach:** `goccy/go-yaml` exposes an AST API (`ast.File`,
`ast.MappingNode`, `ast.SequenceNode`, etc.) that round-trips comments and
key order. The writer exposes three operations:

```
AddEntry(path, channel, id string, payload map[string]any) error
RemoveEntry(path, channel, id string) error
UpdateEntryVersion(path, channel, id, version string) error
```

Internally each operation parses the file as an AST, locates the channel
node (`mcpServers:`, `hooks:`, etc.), and inserts or removes a key while
preserving existing comments around adjacent siblings. New entries are
emitted with a consistent style (two-space indent matching the file's
detected indent).

`yaml.v3` stays as the read-only library for the resolver pipeline — this
package is the only writer.

**Patterns to follow:** No close in-repo precedent. The `goccy/go-yaml`
README and `_examples/` directory cover the AST manipulation pattern; cite
the library version in `go.mod` once chosen.

**Execution note:** Start with golden-file tests — `fixtures/before.yaml` →
operation → `fixtures/after.yaml`. The writer's correctness is judged by
diff, not by re-parsing.

**Test scenarios:**
- Adding `mcpServers.github` to a manifest with one existing MCP server
  produces a file where: the new entry sits at the bottom of the
  `mcpServers:` block; existing entries' comments are untouched; the file's
  trailing newline is preserved.
- Adding to an empty channel (no `mcpServers:` key yet) creates the channel
  key in the correct top-level position (alphabetical among siblings, or at
  the end if no convention is detected).
- Removing `mcpServers.foo` from a 3-server manifest leaves the other two
  servers' comments and order intact, and removes the trailing comma/dash
  if YAML syntax requires.
- Updating a version inline on a non-templated server changes only the
  `version:` value; surrounding comments are untouched.
- Round-trip identity: a manifest with N entries, read and written without
  modification, produces a byte-identical file.
- Adding an entry that already exists returns a typed error
  (`ErrEntryExists`) without writing.
- Removing an entry that does not exist returns a typed error
  (`ErrEntryNotFound`) without writing.
- Comments above the channel key (e.g., `# --- MCP servers ---`) are
  preserved exactly through any operation.

**Verification:** All golden-file tests pass. The writer can round-trip the
showcase `ainfra.yaml` and the `examples/multi-database/ainfra.yaml`
without any byte change.

---

### U5. `ainfra add <channel> <id>` — manifest write + lock + install

**Requirements:** A2; F2; AE2, AE3, AE4
**Dependencies:** U1, U4
**Goal:** `ainfra add mcp github` writes the new entry into `ainfra.yaml`,
re-locks, and runs `install` — one verb, one move, one reconcile.

**Files:**
- `cmd/ainfra/cmd_add.go` (new)
- `cmd/ainfra/cmd_add_test.go` (new)
- `cmd/ainfra/cmd_add_e2e_test.go` (new — exercises the full flow against
  a tempdir manifest)
- `cmd/ainfra/main.go` (modify — register `add`)

**Approach:**

Signature: `ainfra add <channel> <id> [source] [flags]`.

Channel is one of `mcp`, `hook`, `command`, `skill`, `cliTool`,
`marketplace`, `plugin`, `rule`, `tool` (the brainstorm enumerates these).
The short forms (`mcp` not `mcpServers`) are alias mappings; canonical key
goes into YAML.

Behavior:
1. Validate channel name; print did-you-mean on typo (reuse the
   levenshtein helper already in `internal/cli/help.go`).
2. Confirm the manifest file exists; error cleanly otherwise. *Covers AE4.*
3. Build the minimal payload for the channel — for MCP, today, that's
   `{transport, command, args, version}` with a `source` positional if given.
4. Call `manifest/writer.AddEntry`.
5. Run the existing lock pipeline against the now-modified manifest.
6. If `--no-install` not set, run the existing apply pipeline and confirm.

`--personal` swaps the target file to `ainfra.personal.yaml`. *Covers AE3.*

**Patterns to follow:** `cmd/ainfra/cmd_init.go` for confirming-or-creating
a manifest file; `cmd_install.go` (U1) for the apply-pipeline wiring.

**Test scenarios:**
- `ainfra add mcp github` on an existing manifest writes the entry, runs
  lock, runs install, and the resulting `.mcp.json` contains a `github`
  server. *Covers AE2.*
- Re-running the same command is idempotent: errors with `ErrEntryExists`
  and writes nothing. The user can then run `update` or remove first.
- `ainfra add --personal mcp local-fs` writes to `ainfra.personal.yaml`,
  never to `ainfra.yaml`. *Covers AE3.*
- `ainfra add mcp github --no-install` writes the manifest entry and updates
  the lockfile but does not touch `.mcp.json`.
- `ainfra add mcp` (missing id) errors with usage text, exits 2.
- `ainfra add mcps github` (typo channel) errors with "did you mean 'mcp'?".
- `ainfra add mcp github` with no `ainfra.yaml` errors with "no ainfra.yaml
  here — run `ainfra init` first." *Covers AE4.*
- `ainfra add command audit ./commands/audit.md` adds a sourced command and
  materializes the file as part of install.

**Verification:** End-to-end test in a tempdir produces the same state as
hand-editing the YAML and running `lock` then `install`.

---

### U6. `ainfra remove <channel> <id>` — manifest delete + lock + install

**Requirements:** A2; F3; AE7
**Dependencies:** U1, U4
**Goal:** Inverse of `add`. Removes the entry from the manifest, re-locks,
and runs install — which deletes the materialized artifact (`.mcp.json`
entry, command `.md` file, etc.).

**Files:**
- `cmd/ainfra/cmd_remove.go` (new)
- `cmd/ainfra/cmd_remove_test.go` (new)
- `cmd/ainfra/main.go` (modify — register `remove`)

**Approach:** Same shape as `add`. Errors cleanly when the entry doesn't
exist (`ErrEntryNotFound`).

**Test scenarios:**
- `ainfra add mcp X && ainfra remove mcp X` produces a zero diff against
  the pre-add tree, including `.mcp.json`. *Covers AE7.*
- `ainfra remove mcp nonexistent` errors and exits 1 without modifying
  anything.
- `ainfra remove mcp github --no-install` removes the entry and updates
  the lockfile but does not modify `.mcp.json`.
- `ainfra remove --personal mcp local-fs` only modifies
  `ainfra.personal.yaml`.

**Verification:** Round-trip add/remove leaves a clean diff. `.mcp.json` no
longer contains the removed key after a normal (`--no-install` unset) run.

---

### U7. `ainfra update [<channel> <id>]`

**Requirements:** A2; F5
**Dependencies:** U1, U4, U3 (shares the latest-version lookup with `outdated`)
**Goal:** Bump locked versions to the latest resolvable. Bare `ainfra update`
bumps all; `ainfra update mcp github` bumps one.

**Files:**
- `cmd/ainfra/cmd_update.go` (new)
- `cmd/ainfra/cmd_update_test.go` (new)
- `cmd/ainfra/main.go` (modify — register `update`)

**Approach:**

Bare form: re-run the lock pipeline with a flag that forces version
resolution to "latest" instead of "what the lock says." Per-entry form:
update only the named entry, leave others at their locked version. Both
forms then run install unless `--no-install`.

For non-resolvable entries, the update is a no-op (same behavior as
`outdated` — an entry with no `version:` concept has nothing to bump).

**Test scenarios:**
- `ainfra update mcp github` with a stale version bumps the version pin
  in `ainfra.lock` (and in the args of `.mcp.json` after install).
- `ainfra update` (bare) bumps all stale entries.
- `ainfra update mcp github` when github is already at latest is a no-op
  and exits 0 with "Already up to date."
- `ainfra update mcp nonexistent` errors and exits 1.
- `ainfra update mcp github --no-install` updates the lockfile but not
  `.mcp.json`.
- An entry with no resolvable version (HTTP transport) is silently skipped
  in bare form; named update errors with "no version concept for this
  entry."

**Verification:** After `update`, `outdated` returns empty for any
previously-stale entry the update targeted.

---

### U8. Refresh `README.md`, `docs/quickstart.md`, `ainfra.yaml`, and `docs/sx-comparison.md`

**Requirements:** A1, A2, A3; AE9
**Dependencies:** U1, U3, U5, U6, U7 (everything user-facing must exist)
**Goal:** Every public document reflects the new front-page surface.
Examples use `install`/`add`/`remove`. The README's "three promises" copy
still holds; only the command examples change.

**Files:**
- `README.md` (modify — Commands table, quick-start snippet, "Joining a
  team" subsection)
- `docs/quickstart.md` (modify — walk-through uses `install`/`add`)
- `docs/sx-comparison.md` (touch lightly — note the package-manager pivot)
- `docs/design.md` (modify — update §10 build phases reference if it
  enumerates verbs; otherwise no change)
- `ainfra.yaml` (modify — add a comment block at the top showing the new
  `ainfra add mcp X` form for context)

**Test scenarios:** none (pure docs).

**Verification:** A reader following `docs/quickstart.md` end-to-end on a
fresh checkout reaches a working tree without needing to consult any
other doc. The README's "Commands" table lists exactly 8 verbs. No
documentation references `apply`, `plan`, `check`, or `validate` outside
the "Deprecated aliases" details block.

---

## System-Wide Impact

- **Backwards compatibility:** Every old verb continues to work. One stderr
  line per process per alias is the only behavior delta. Subscriber-mode
  artifacts (`publish`/`installer` output) are byte-equal.
- **CI integrations:** Teams using `ainfra check` see the deprecation line
  in their CI logs once per job. Real fix is a one-line edit to
  `install --dry-run --strict`. Migration window: through 0.2.
- **Manifest hashing:** Adding `goccy/go-yaml` only as a writer means the
  parser used to compute the manifest hash is unchanged; lockfile hashes
  do not shift.
- **Lockfile schema:** Unchanged.
- **Provider interface:** Unchanged.
- **`.ainfra/history.jsonl`:** Continues to be written by `install`
  (formerly `apply`). The `history` verb stays as a hidden alias.

---

## Risks

| Risk | Severity | Mitigation |
|---|---|---|
| Comment-preservation in `goccy/go-yaml` is incomplete for some edge case (anchor, merge key, multi-line string) and `add` quietly mangles a real team manifest | High | U4's round-trip identity test is the gate. If any in-repo manifest fails round-trip, `add` is held in `--no-install` mode and the failure surfaces before any reconcile. Worst case, fall back to `yaml.v3` re-emit, accepting comment loss with an explicit warning line. |
| `outdated` and `update`'s "latest version" lookup needs network access to npm, which CI may not allow | Medium | First cut returns "unknown" for entries without local resolution. The full npm probe is a follow-up unit; `update` is still useful at the lockfile-arithmetic level for templated/resolved entries. |
| Users running `ainfra apply --yes` in scripts see a new stderr line and assume something broke | Medium | One-line deprecation note is non-alarming and includes the replacement verb. `AINFRA_QUIET=1` suppresses it. Release notes call this out. |
| The `add` channel name (`mcp` vs `mcpServers`) creates a glossary problem — users wonder what to type | Low | `ainfra add --help` lists the exact channel names. Did-you-mean on typo covers the common drift. Documented in README. |
| Phase 1 ships and Phase 2 stalls; the CLI has new inspection verbs but no mutation verbs, looking half-baked | Low | Phase 1 is independently shippable and improves the surface even alone. If Phase 2 stalls, the user is no worse off than today. |

---

## Verification Strategy

End-to-end demo, post-Phase-2 (also a CI smoke test):

1. Fresh tempdir, `ainfra init`.
2. `ainfra add mcp github` — confirms manifest, lock, and `.mcp.json` all
   carry `github`.
3. `ainfra list` — single row for `github`.
4. `ainfra add command audit ./commands/audit.md` — second entry appears.
5. `ainfra add --personal mcp local-fs` — third entry shows
   `layer=personal`.
6. `ainfra outdated` — empty.
7. `ainfra install --dry-run` — empty plan.
8. `ainfra remove mcp github` — list shows two entries, `.mcp.json` no
   longer contains `github`.
9. `ainfra --help` — lists 8 verbs.
10. `ainfra apply --yes` — works, prints one deprecation line.

Anything passing 1–10 without manual intervention is shippable.

---

## Deferred to Implementation

- Exact wording of the deprecation lines (kept short; final phrasing during
  U1).
- Whether `outdated` queries npm directly or via a tiny in-repo helper —
  depends on what `internal/provider/pkg/` already exposes.
- Whether `list` reads the applied ledger as an authoritative source of
  "installed" or trusts the lockfile alone — judgment call on `check` parity.
- Whether `add` accepts a source-string shorthand (`ainfra add skill
  github:org/repo/path`) in this phase or defers it. Brainstorm suggests
  allowed-when-channel-given but left as implementation discretion.
- Final indentation style for newly-emitted YAML entries (match detected
  indent, fall back to 2 spaces).
