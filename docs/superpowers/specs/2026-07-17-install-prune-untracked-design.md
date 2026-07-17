# `ainfra install --prune`: removing untracked repo config

Date: 2026-07-17
Status: Approved design, not yet implemented

## Problem

`ainfra install` converges the machine toward `ainfra.yaml`, but it only ever
adds and updates. A resource ainfra never recorded as its own is left alone by
design (`internal/provider/diff.go::DiffResources`). Over time a repo
accumulates local-only config — an MCP server someone hand-added to `.mcp.json`,
a skill directory dropped into `.claude/skills/` — that no manifest describes
and no install removes. The repo's actual setup drifts from the setup it claims
to have, and nothing converges it back.

`ainfra inspect` already detects this: it classifies every entry as tracked,
untracked, or missing (`cmd/ainfra/cmd_inspect.go::summarize`). Its only remedy
is `ainfra init --adopt --force`, which folds the local entries *into* the
manifest. That is the right answer when the local additions are wanted. It is
the wrong answer when they are cruft.

Note: there is no `ainfra install --force`. `--force` exists only on `init` and
`adopt`, where it means "throw the existing `ainfra.yaml` away and re-scan"
(`cmd/ainfra/cmd_init.go`, `cmd/ainfra/cmd_adopt.go`). That is a
manifest-authoring escape hatch and is unrelated to machine state. This design
adds a new mode; it does not strengthen an existing one.

## Goal

`ainfra install --prune` deletes untracked entries from a repo's config rather
than tolerating them, for the channels where that can be done correctly and
safely.

Scoped honestly: this covers `mcpServers`, `skills`, and `commands` — not "the
manifest is now the whole truth". Untracked `hooks` and `rules` survive a
prune, for reasons that are structural rather than incidental (see Safety
boundary 2). The plan output must not imply otherwise, and `--prune` must never
be described to users as making a repo exactly match its manifest.

## Non-goals

- **Auto-convergence.** `--prune` is opt-in per run. Teammates do not lose
  untracked config on a normal `ainfra install`. Making prune a manifest policy
  (`pruning: strict`) that runs for everyone automatically was considered and
  rejected as too destructive for the default path. Revisit only with evidence.
- **Touching personal global config.** Out of scope by construction (see
  Safety boundary 1).
- **Uninstalling CLI tools or tearing down background services.** Out of scope
  by construction (see Safety boundary 2).

## Prior art in the codebase

The delete-something-ainfra-never-installed mechanic already exists. A desired
resource flagged `Tombstone` (`enabled: false` in the manifest) is an explicit
"ensure absent": `DiffResources` removes it whenever it is present on the
machine, *even if ainfra never installed it* — the observed-but-not-prior case
(`internal/provider/diff.go`). The existing comment draws the exact axis this
design extends:

> This is what makes "absent from the manifest" (leave alone) differ from
> "explicitly retired" (remove everywhere).

`--prune` adds a third policy on that same axis: "absent from the manifest and
pruning is on" → remove. It is not a parallel deletion path; it reuses the
tombstone semantics, changing only which resources receive the flag.

## Design

### Control flow

1. `ainfra install --prune` sets `Prune: true` on the **repo-scope**
   orchestrator only.
2. The orchestrator passes `DiffOpts{Prune: true}` to `DiffResources`, but only
   for providers that implement the `Pruner` interface.
3. `DiffResources` synthesizes a `ChangeDelete` for each observed resource
   present in neither `desired` nor `prior`.
4. Before `Apply`, the orchestrator calls `Pruner.Backup` for each synthesized
   delete. A backup that fails cancels that delete.
5. `Apply` executes, unchanged.

Everything downstream of `Change` is untouched: plan rendering, the confirm
prompt, `--dry-run`, `--strict`, and `Apply` all already speak `Change`.

Two capabilities fall out for free:

- `--dry-run --prune` is an honest preview, because it runs the identical code
  path rather than a simulation of it.
- `--strict --dry-run --prune` is a CI check that fails when a repo has drifted
  from its manifest. This is arguably the better long-term fix for "we keep
  appending": it catches drift at PR time instead of deleting after the fact.

### `DiffResources` change

Signature gains an options struct (2 production callers at
`internal/provider/orchestrator.go:107` and `:169`; 9 test call sites):

```go
type DiffOpts struct {
	// Prune treats an observed resource that is in neither desired nor prior
	// as an implicit tombstone. Off by default: the zero value preserves the
	// historical "leave untracked resources alone" contract.
	Prune bool
}

func DiffResources(channel string, desired, observed, prior []Resource, opts DiffOpts) ChannelPlan
```

New branch, after the existing tombstone and prior loops:

```go
if opts.Prune {
	for id, got := range o {
		if _, wanted := d[id]; wanted {
			continue
		}
		if _, known := pr[id]; known {
			continue // prior-but-not-desired is already a Delete above
		}
		if _, tomb := tombstones[id]; tomb {
			continue // already handled above
		}
		plan.Changes = append(plan.Changes, Change{
			Kind:     ChangeDelete,
			ID:       id,
			Detail:   "untracked — not in ainfra.yaml, will be removed",
			Resource: got,
			Prune:    true,
		})
	}
}
```

`Change` gains a `Prune bool` field, set only here. The orchestrator uses it to
decide what to back up. It is a structural flag rather than a `Detail` string
match, which would be fragile.

### Safety boundary 1: scope

The orchestrator already distinguishes `ScopeRepo` from `ScopeUser`, each with
its own applied ledger and paths (`internal/provider/orchestrator.go::Scope`).
The user-scope orchestrator never sets `Prune`, so personal global config under
`~/.claude` — a teammate's own MCP servers, skills, and hooks — cannot be
reached by a `--prune` typed in any repo.

This is enforced structurally, not by convention. There is no code path from the
flag to the user-scope ledger.

### Safety boundary 2: the `Pruner` interface

A channel is pruneable **only if its provider implements `Pruner`**:

```go
// Pruner is implemented by providers whose channel supports --prune. A
// provider that does not implement it can never have untracked resources
// removed: the orchestrator does not pass Prune to its diff. This makes the
// pruneable set a property of the type system rather than a list that can
// drift.
type Pruner interface {
	// Backup copies the on-disk state of r into dir before r is deleted.
	// Implementations own their storage layout; the orchestrator does not
	// know where a channel keeps its data.
	Backup(env Env, r Resource, dir string) error
}
```

Implemented by exactly three providers: `MCP`, `Skills`, `Commands`. These are
the only channels whose `Observe` both enumerates on-disk state *and* stays
within `env.Root`:

- `MCP.Observe` reads `env.Root/.mcp.json` (`mcp.go::mcpPath`).
- `Skills.Observe` lists `env.Root/.claude/skills/` (`skills.go::skillsDir`).
- `Commands.Observe` lists `env.Root/.claude/commands/` (`commands.go::commandsDir`).

Deliberately **not** implemented by:

| Channel              | Why not                                            |
| -------------------- | -------------------------------------------------- |
| `hooks`              | Cannot work — see below.                           |
| `rules`              | Would breach the scope boundary — see below.       |
| `tools`              | Pruning would uninstall CLI binaries (brew).       |
| `backgroundServices` | Pruning would tear down the prod-DB tunnels.       |
| `plugins`            | Installed artifacts; reinstall is not free.        |
| `marketplaces`       | Removing one silently breaks plugins that need it. |

Adding a channel to the pruneable set is therefore a deliberate act of
implementing an interface, reviewable in a PR.

#### Why `hooks` is unpruneable

`Hooks.Observe` does not read the machine: it sources from the applied ledger
(`internal/provider/claudecode/hooks.go`). Its own comment explains why —
Claude Code's `settings.json` keys hooks by event, not by ainfra hook id, so the
written file cannot be mapped back to managed hooks, and the ledger is the only
authoritative record of what was applied.

The consequence is structural: for hooks, `observed` is by definition equal to
`prior`. The set "observed but not in prior" — the exact set prune operates on —
is therefore *always empty*. A hand-added hook in `settings.json` is invisible
to `Observe` and cannot be detected, let alone removed.

This makes hooks not merely unsupported but actively dangerous to include: a
`--prune` that appeared to cover hooks would report success having done nothing,
giving false confidence that the file matches the manifest. Removing untracked
hooks would first require an `Observe` that can attribute settings.json entries
back to ainfra — a separate problem, out of scope here.

#### Why `rules` is unpruneable

`Rules.Observe` scans two directories: `env.Root/.claude/ainfra` **and**
`env.Home/.claude/ainfra` (`internal/provider/claudecode/rules.go`). It does so
in every scope, because a rule's fragment is co-located with its target and a
`~`-prefixed target is user-level (`rules.go::fragmentFor`).

So a repo-scope `--prune` would observe the user's *global* rule fragments,
find them untracked by this repo's manifest, and delete them from `$HOME` —
precisely the outcome Safety boundary 1 exists to prevent. The scope guard on
the orchestrator does not help here: the leak is inside the provider's own
`Observe`, below the level the guard operates at.

Supporting `rules` would first require a scope-filtered `Observe` that reports
only fragments under the scope being reconciled. That is a worthwhile change but
a separate one, with its own blast radius on `check`/`inspect` output.

### Backup

**Why provider-side.** The obvious design — have the orchestrator serialize
`Change.Resource.Payload` to a backup file — does not work. `Observe` does not
populate `Payload`: `Skills.Observe` returns only `ID` and `Channel`, and
`MCP.Observe` returns `ID` and `ContentHash`. An orchestrator-level backup would
write empty files while reporting success, losing exactly the data it claims to
protect. Only the provider knows its own on-disk layout, so backup must live
there.

**Destination.** `.ainfra/pruned-<RFC3339-timestamp>/<channel>/`, created once
per run. `.ainfra/` is already git-ignored and already hosts the applied ledger
(`internal/provider/applied.go::appliedPath`), so this introduces no new
location and nothing lands in git.

**Per-channel semantics:**

- `Skills`: copy the tree at `.claude/skills/<id>/` to `skills/<id>/`.
- `Commands`: copy `.claude/commands/<id>.md` to `commands/<id>.md`.
- `MCP`: re-read `.mcp.json` and write the entry's JSON fragment to
  `mcpServers/<id>.json`.

**Retention:** none. Backups accumulate under `.ainfra/`; the directory is
git-ignored and the user can delete it. A retention policy is deferred until
someone complains — YAGNI.

**Restore:** manual, by copying the tree back. No `ainfra restore` command in
this iteration. The backup exists so a mistaken prune is *recoverable*, not so
it is a one-command undo. Revisit if prune is used often enough to warrant it.

### Error handling

- **Backup fails for a resource** → that resource's delete is dropped from the
  plan and surfaced as a `ChangeFailure` via the existing aggregation
  (`internal/provider/provider.go::ApplyError`). Never delete what could not be
  backed up.
- **Backup directory cannot be created** → abort the whole run before any
  `Apply`, so a run cannot half-prune with no backups.
- **`--dry-run`** → no backup directory is created and nothing is copied. The
  plan still lists the deletes.
- **Partial apply** → unchanged; the existing `ChangeFailure` / `ChangeSkip`
  machinery already covers it.

### Applied ledger

No change. Pruned resources are by definition absent from the ledger (that is
what makes them untracked), so there is nothing to record or remove. The
post-apply `WriteApplied` snapshot of desired state is already correct.

## Testing

Unit, `internal/provider/diff_test.go`:

- Untracked observed resource → `ChangeDelete` when `Prune: true`.
- Same resource → no change when `Prune: false` (guards the historical
  contract; this is the regression test that matters most).
- `DiffOpts{}` zero value behaves exactly as the old signature did.
- Tombstone still wins over prune for the same ID (no duplicate deletes).
- Prior-but-not-desired still produces exactly one delete under prune.
- Desired resources are unaffected by prune.

Unit, orchestrator:

- User-scope orchestrator never prunes, even when the flag is set.
- A provider not implementing `Pruner` never receives `Prune: true`.
- Backup is called before `Apply` for each pruned delete.
- Backup failure cancels that delete and leaves the resource on disk.
- Backup-dir creation failure aborts before any `Apply`.

Provider-level:

- `Skills.Backup` round-trips a hand-written skill directory: prune it, restore
  from the backup, assert the tree matches byte-for-byte.
- `MCP.Backup` writes the untracked server's JSON fragment, not an empty file.
  (This is the direct regression test for the `Payload`-is-empty trap.)
- Compile-time assertion that exactly `MCP`, `Skills`, and `Commands` satisfy
  `Pruner`, and that `Hooks`, `Rules`, `Tools`, `Services`, `Plugins`, and
  `Marketplaces` do not. A table test over the provider set asserting
  `_, ok := p.(Pruner)` matches expectation, so quietly implementing `Backup`
  on an excluded provider fails the build rather than silently widening the
  blast radius.
- `rules`: an untracked fragment under `$HOME/.claude/ainfra/` survives a
  repo-scope `install --prune`. This is the guard against the `Rules.Observe`
  cross-scope leak; it must hold even if `rules` is later made pruneable.

E2E, `cmd/ainfra/`:

- `install --prune --dry-run` lists untracked deletes and changes nothing on
  disk.
- `install --prune --yes` removes an untracked MCP server and skill, and leaves
  a tracked one and an untracked `tools` entry alone.
- `install` without `--prune` leaves untracked entries alone (the headline
  safety property).

## Open questions

1. Should `--prune` require `--dry-run` first, or is the existing confirm prompt
   enough? Current design: confirm prompt only, consistent with the rest of
   `install`.
2. Should `inspect`'s `nextStepHints` mention `install --prune` alongside
   `init --adopt --force`, so the two remedies for untracked entries are
   discoverable together? Probably yes; low cost.

## Accepted tradeoff

`--prune` puts destructive behavior behind a flag on the command everyone runs
reflexively. A separate `ainfra prune` command was considered — a scary verb
deserving its own name — but rejected because it would duplicate plan
rendering, confirmation, and `--dry-run`, and would drift from `install` over
time. The scope guard, the `Pruner` interface, the confirm prompt, and backups
are judged sufficient containment.
