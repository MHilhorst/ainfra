# `ainfra install --prune`: declare-or-clear for untracked config

Date: 2026-07-17
Status: Approved design, not yet implemented

## Problem

`ainfra install` converges the machine toward `ainfra.yaml`, but it only ever
adds and updates. A resource ainfra never recorded as its own is left alone by
design (`internal/provider/diff.go::DiffResources`). Machines accumulate config
that no manifest describes and no install removes, so a repo's real setup drifts
from the setup it claims to have and nothing converges it back.

Note: there is no `ainfra install --force`. `--force` exists only on `init` and
`adopt`, where it means "throw the existing `ainfra.yaml` away and re-scan"
(`cmd/ainfra/cmd_init.go`, `cmd/ainfra/cmd_adopt.go`) — a manifest-authoring
escape hatch, unrelated to machine state. This design adds a new mode; it does
not strengthen an existing one.

## The evidence that shapes this design

The obvious design is "untracked means cruft, so delete it". **On the only
machine available to inspect, that premise is false.** `ainfra inspect` in
`claude-config` on 2026-07-17 reported these untracked entries:

| Channel  | Untracked entries                                                     |
| -------- | --------------------------------------------------------------------- |
| commands | `dbaccess`, `document`, `monitor`, `review-wip`, `ship`, `spin`, `start`, `stop` |
| rules    | `claude-md` (CLAUDE.md), `agents-md` (AGENTS.md)                       |
| hooks    | `pretooluse-bash`                                                     |

The personal manifest at `~/.config/ainfra/personal.yaml` declares only three
things: the `pretooluse-bash` hook, commands `merge` and `pr`, and the
`video-edit` skill.

So a naive `--prune` would have deleted eight of that user's ten slash commands
— their daily workflow — plus their CLAUDE.md and AGENTS.md. Roughly ten of
eleven untracked entries were wanted.

**Untracked does not mean unwanted. It means "never got around to declaring
it."** A deleter built on the opposite assumption destroys real work. Every
constraint below follows from this.

## Goal

Make every machine's config fully declared — some entries team-declared, some
personal-declared, none undeclared — so "adhere completely to the ainfra setup"
becomes true without anyone losing work.

`--prune` reaches that end state via **declare-or-clear**, not delete-on-sight:

1. `ainfra install --prune` shows what is undeclared and clears nothing.
2. The user declares what they want to keep.
3. A second `ainfra install --prune` clears whatever is genuinely left over.

The user is never surprised. Deletion only ever happens to something the user
was shown and chose not to keep.

### Honest scope

`--prune` covers `skills`, `commands`, and `rules` in both the repo and the
user's `~/.claude`, plus `mcpServers` in the repo. It is **not** "nothing but
ainfra remains". These survive a prune regardless:

- **Untracked hooks**, always — structural; see "Why `hooks` is unpruneable".
- **Personal MCP servers in `~/.claude.json`**, always; see "Why personal MCP
  servers survive".
- **CLI tools, background services, plugins, marketplaces**, by choice; see
  "The `Pruner` interface".

Plan output and docs must not describe `--prune` as making a machine match its
manifest exactly. It clears undeclared *config files* — narrower, and honest.

## Non-goals

- **Auto-convergence.** `--prune` is opt-in per run. A manifest policy
  (`pruning: strict`) applying to everyone automatically was considered and
  rejected: given the evidence above, a policy that deletes undeclared config on
  a normal install would destroy teammates' workflows without them typing
  anything.
- **User-scope MCP support.** Reading/writing `~/.claude.json` was considered as
  a prerequisite and dropped: on the inspected machine the user-scope
  `mcpServers` key is `{}` and no project carries a local-scope entry, so the
  work would clear nothing that exists. Revisit only with evidence of real
  user-scope MCP servers.
- **Uninstalling CLI tools or tearing down background services.**
- **An `ainfra restore` command.** Backups make a mistaken prune recoverable;
  one-command undo is deferred.

## Keeping something: the personal layer

The escape hatch already exists. ainfra has a personal manifest layer —
`~/.config/ainfra/personal.yaml`, `manifest.LayerPersonal` — and personal entries
route to user scope automatically
(`cmd/ainfra/commands.go::partitionLockByLayer`).

The rule a user needs to understand is one sentence: **if you want to keep it,
declare it.** `ainfra add <channel> <id> --personal` for user-scope entries; for
repo-scope entries, `ainfra init --adopt --force` or a manifest edit, which is a
team decision and goes through a PR.

## The guard: the offered ledger

**This is the heart of the design.** Prune never deletes an entry the user has
not previously been shown.

A new per-scope ledger, `<scope root>/.ainfra/prune-offered.json` (user scope:
alongside the applied ledger under XDG, via `xdg.AppliedLedgerPath`'s
directory), records every untracked resource that has been reported to the user:

```json
{
  "version": 1,
  "offered": {
    "commands:ship":     { "firstOfferedAt": "2026-07-17T11:04:22Z" },
    "rules:claude-md":   { "firstOfferedAt": "2026-07-17T11:04:22Z" }
  }
}
```

Behavior on `ainfra install --prune`:

- Compute the untracked set, as `DiffResources` already does under `Prune`.
- **Not in the offered ledger** → record it, print it under a "not declared —
  will be removed on the next --prune unless you declare it" heading, and
  **drop the delete from the plan**.
- **Already in the offered ledger** → keep the delete. The user saw it on a
  previous run and did not declare it.
- Entries that become declared simply stop appearing in the untracked set; their
  ledger rows are pruned opportunistically on write.

`--dry-run` never writes the offered ledger. A preview must not arm a deletion:
if it did, `--dry-run --prune` followed by a real `--prune` would delete on what
the user experienced as the *first* real run.

### Why a ledger rather than a prompt

A confirm prompt is answered in the moment, under time pressure, by someone who
has not yet worked out whether `review-wip` matters to them. The ledger forces a
gap between "you were told" and "it is deleted", which is the only thing that
makes the choice real. It also survives non-interactive runs (`--yes`, CI),
where a prompt degrades to nothing.

### Ledger honesty

The offered ledger is the mechanism that lets prune be safe, so it must not be
quietly bypassable:

- `--yes` does **not** skip the guard. It skips the confirm prompt for deletes
  that are already armed.
- There is deliberately no `--prune-now` / `--force-prune` flag. The whole value
  is the enforced gap; a flag to skip it re-creates the footgun and would become
  the copy-pasted incantation.

## Prior art in the codebase

The delete-something-ainfra-never-installed mechanic already exists. A desired
resource flagged `Tombstone` (`enabled: false`) is an explicit "ensure absent":
`DiffResources` removes it whenever present, *even if ainfra never installed it*
— the observed-but-not-prior case (`internal/provider/diff.go`). Its comment
draws the axis this design extends:

> This is what makes "absent from the manifest" (leave alone) differ from
> "explicitly retired" (remove everywhere).

`--prune` adds a third policy on that axis: "absent from the manifest, pruning
is on, and previously offered" → remove.

## Design

### Control flow

1. `ainfra install --prune` sets `Prune: true` on both the repo-scope and
   user-scope orchestrators.
2. Each passes `DiffOpts{Prune: true}` to `DiffResources`, but only for
   providers implementing `Pruner`.
3. `DiffResources` synthesizes a `ChangeDelete` for each observed resource in
   neither `desired` nor `prior`, flagged `Prune: true`.
4. The orchestrator partitions those deletes against the offered ledger:
   unoffered ones are reported and dropped; offered ones stay.
5. For surviving deletes, `Pruner.Backup` runs before `Apply`. A failed backup
   cancels that delete.
6. `Apply` executes, unchanged.
7. On success (and not `--dry-run`), the offered ledger is written.

Everything downstream of `Change` is untouched: plan rendering, the confirm
prompt, `--dry-run`, `--strict`, and `Apply` all already speak `Change`.

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
			Detail:   "not declared in ainfra — will be removed",
			Resource: got,
			Prune:    true,
		})
	}
}
```

`Change` gains a `Prune bool` field, set only here. The orchestrator uses it to
find the deletes subject to the offered-ledger guard and to backup — a
structural flag rather than a `Detail` string match, which would be fragile.

The diff stays honest: it reports every untracked resource. The *guard* lives in
the orchestrator, because "has the user seen this before" is machine state, not
a property of the three-way diff.

### The user-scope orchestrator must be forced

**This trap would make the feature silently useless.** Today the user-scope
orchestrator is constructed only when the rendered set has personal-layer
entries or the user ledger is non-empty (`cmd/ainfra/commands.go`):

```go
if herr == nil && (anyResources(userRendered) || userLedgerNonEmpty) {
	userEnv := env
	userEnv.Root = home
	userEnv.UserScope = true
	userOrch = provider.NewOrchestratorScoped(home, provider.ScopeUser, userEnv, providers)
```

A person with no personal-layer entries and an empty user ledger gets **no
user-scope orchestrator at all** — exactly the person whose `~/.claude` has the
most undeclared config. The condition must gain `|| prune`.

Providers need no per-scope logic: `userEnv.Root = home` means `skillsDir(env)`
resolves to `$HOME/.claude/skills` and `commandsDir(env)` to
`$HOME/.claude/commands`. The same provider code serves both scopes.

### The `Pruner` interface

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

Implemented by `MCP`, `Skills`, `Commands`, `Rules` — the providers whose
`Observe` enumerates on-disk state and can therefore see untracked entries:

- `MCP.Observe` reads `env.Root/.mcp.json` (`mcp.go::mcpPath`).
- `Skills.Observe` lists `env.Root/.claude/skills/` (`skills.go::skillsDir`).
- `Commands.Observe` lists `env.Root/.claude/commands/` (`commands.go::commandsDir`).
- `Rules.Observe` lists `.claude/ainfra/*.md` under the repo and `$HOME` (`rules.go`).

Deliberately **not** implemented by:

| Channel              | Why not                                            |
| -------------------- | -------------------------------------------------- |
| `hooks`              | Cannot work — see below.                           |
| `tools`              | Pruning would uninstall CLI binaries (brew).       |
| `backgroundServices` | Pruning would tear down the prod-DB tunnels.       |
| `plugins`            | Installed artifacts; reinstall is not free.        |
| `marketplaces`       | Removing one silently breaks plugins that need it. |

Adding a channel is a deliberate act of implementing an interface, reviewable in
a PR.

#### A note on `Rules.Observe` and scope

`Rules.Observe` scans both `env.Root/.claude/ainfra` and `env.Home/.claude/ainfra`
in every scope, because a rule's fragment is co-located with its target and a
`~`-prefixed target is user-level (`rules.go::fragmentFor`).

Under this design that is intended — the user is reconciling their own machine.
But it means a repo-scope prune and a user-scope prune both observe the `$HOME`
fragments, so the two plans can name the same resource. The user-scope plan owns
`$HOME` fragments; the repo-scope plan must filter them out, or the resource is
double-deleted and double-backed-up. This is the one place prune needs genuinely
new scope logic.

#### Why `hooks` is unpruneable

`Hooks.Observe` does not read the machine: it sources from the applied ledger
(`internal/provider/claudecode/hooks.go`). Its comment explains why — Claude
Code's `settings.json` keys hooks by event, not by ainfra hook id, so the written
file cannot be mapped back to managed hooks, and the ledger is the only
authoritative record of what was applied.

The consequence is structural: for hooks, `observed` is by definition equal to
`prior`, so "observed but not in prior" — the exact set prune operates on — is
*always empty*. A hand-added hook in `settings.json` is invisible.

This makes hooks not merely unsupported but dangerous to include: a `--prune`
appearing to cover hooks would report success having done nothing, giving false
confidence. Supporting them needs an `Observe` that can attribute settings.json
entries back to ainfra — a separate problem.

#### Why personal MCP servers survive

`MCP` implements `Pruner`, but only reaches the repo's `.mcp.json`. Claude Code
reads user-level MCP servers from `~/.claude.json`, a different file with a
different format that ainfra does not read or write — see the live warning at
`cmd/ainfra/commands.go` (`provider.HasUserScopeMCP`).

In user scope, `MCP.Observe` would read `$HOME/.mcp.json`, which is not where
personal MCP servers live. **`MCP` must therefore be skipped entirely in the
user-scope prune**, not run against the wrong path.

As of 2026-07-17 this costs nothing: the inspected machine's user-scope
`mcpServers` is `{}`. The docs should say plainly that personal MCP servers are
not touched, rather than let users assume their MCP list was reset.

### Backup

**Why provider-side.** The obvious design — have the orchestrator serialize
`Change.Resource.Payload` — does not work. `Observe` does not populate
`Payload`: `Skills.Observe` returns only `ID` and `Channel`, `Rules.Observe` the
same, `MCP.Observe` returns `ID` and `ContentHash`. An orchestrator-level backup
would write empty files while reporting success, losing exactly the data it
claims to protect. Only the provider knows its own layout.

**Destination.** `<scope root>/.ainfra/pruned-<RFC3339-timestamp>/<channel>/`,
created once per run per scope. `.ainfra/` is git-ignored and already hosts the
applied ledger (`internal/provider/applied.go::appliedPath`), so this adds no new
location and nothing lands in git.

**Per-channel semantics:**

- `Skills`: copy the tree at `.claude/skills/<id>/` to `skills/<id>/`.
- `Commands`: copy `.claude/commands/<id>.md` to `commands/<id>.md`.
- `Rules`: copy the fragment `.claude/ainfra/<id>.md` to `rules/<id>.md`.
- `MCP`: re-read `.mcp.json` and write the entry's JSON fragment to
  `mcpServers/<id>.json`.

**Retention:** none. Backups accumulate under git-ignored `.ainfra/`; the user
can delete them. Deferred until someone complains.

**Restore:** manual, by copying the tree back. The backup makes a mistaken prune
*recoverable*, not one-command undoable.

### Error handling

- **Backup fails for a resource** → that delete is dropped and surfaced as a
  `ChangeFailure` via the existing aggregation
  (`internal/provider/provider.go::ApplyError`). Never delete what could not be
  backed up.
- **Backup directory cannot be created** → abort before any `Apply`, so a run
  cannot half-prune with no backups.
- **Offered ledger cannot be written** → the run must fail *before* deleting
  anything. A delete that happens without its ledger row means the next run
  re-offers an entry that is already gone.
- **Offered ledger is corrupt/unparseable** → treat as empty (offer everything
  afresh) and warn. Failing open here is safe: the worst case is one extra
  offer round, never an unannounced deletion.
- **`--dry-run`** → no backup dir, no ledger write, nothing copied. The plan
  still lists what would be offered or removed.
- **`os.UserHomeDir()` fails with `--prune`** → abort rather than silently
  pruning repo scope only.
- **Partial apply** → unchanged; existing `ChangeFailure` / `ChangeSkip` cover it.

### Applied ledger

No change. Pruned resources are by definition absent from the applied ledger
(that is what makes them untracked), so there is nothing to record or remove.

## User experience

First `--prune` run:

```
Not declared in ainfra (nothing removed yet):

  commands  dbaccess, document, monitor, review-wip, ship, spin, start, stop
  rules     claude-md, agents-md

To keep any of these, declare them:
  ainfra add commands ship --personal

Anything still undeclared will be removed by the next 'ainfra install --prune'.
Backups are written to .ainfra/pruned-<timestamp>/ regardless.
```

Second run removes what remains undeclared, listing each delete and its backup
path.

Prune must also report what it *cannot* clear (hooks, personal MCP servers) so
users are not left believing the machine is now exactly ainfra.

## Testing

Unit, `internal/provider/diff_test.go`:

- Untracked observed resource → `ChangeDelete` when `Prune: true`.
- Same resource → no change when `Prune: false`. Guards the historical contract;
  the most important regression test here.
- `DiffOpts{}` zero value behaves exactly as the old signature did.
- Tombstone still wins over prune for the same ID (no duplicate deletes).
- Prior-but-not-desired still produces exactly one delete under prune.
- Desired resources are unaffected by prune.

Unit, offered ledger (the guard — test hardest):

- First `--prune` with an untracked entry: entry reported, **not deleted**,
  ledger row written.
- Second `--prune`, same entry still undeclared: deleted.
- Entry declared between runs: not deleted, not reported, ledger row dropped.
- `--dry-run --prune` writes **no** ledger row, so a following real `--prune`
  still only offers. Direct regression test for "a preview must not arm a
  deletion".
- `--yes --prune` does not bypass the guard on a first encounter.
- Corrupt ledger → everything re-offered, nothing deleted, warning emitted.
- Ledger write failure → run fails before any delete.

Unit, orchestrator / `cmd`:

- **`--prune` constructs the user-scope orchestrator even when the rendered set
  has no personal entries and the user ledger is empty.** Without this the
  feature no-ops for exactly the messiest machines.
- A provider not implementing `Pruner` never receives `Prune: true`.
- `MCP` is not pruned in user scope.
- Backup is called before `Apply` for each armed delete.
- Backup failure cancels that delete and leaves the resource on disk.
- A `$HOME` rule fragment is deleted once, by the user-scope plan only.

Provider-level:

- `Skills.Backup` round-trips a hand-written skill directory: prune it, restore
  from backup, assert the tree matches byte-for-byte.
- `MCP.Backup` writes the untracked server's JSON fragment, not an empty file.
  Direct regression test for the `Payload`-is-empty trap.
- Table test over the provider set asserting `_, ok := p.(Pruner)` matches
  expectation, so quietly implementing `Backup` on an excluded provider fails
  the build rather than silently widening the blast radius.

E2E, `cmd/ainfra/`:

- **The scenario from "The evidence" above**: a machine with 8 undeclared
  commands and 2 undeclared rules. First `install --prune --yes` deletes
  nothing. After declaring 8 of them, a second run removes only the remaining 2.
  This is the acceptance test for the whole feature.
- `install --prune --dry-run` changes nothing and writes no ledger.
- `install` without `--prune` leaves every untracked entry alone. The headline
  safety property.
- Armed `install --prune --yes` leaves an untracked `tools` entry and an
  untracked hook alone.

## Open questions

1. Should `inspect`'s `nextStepHints` offer `install --prune` alongside
   `init --adopt --force`, so both remedies are discoverable? Probably yes.
2. Should an offered entry expire — e.g. re-offer if the user has not run prune
   for 90 days? Current design: no expiry; an offer is permanent. Revisit if
   someone is surprised by a delete armed long ago.

## Accepted tradeoffs

`--prune` puts destructive behavior behind a flag on the command everyone runs
reflexively. A separate `ainfra prune` command was considered — a scary verb
deserving its own name — but rejected because it would duplicate plan rendering,
confirmation, and `--dry-run`, and would drift from `install`. The offered
ledger, `Pruner` interface, confirm prompt, `--dry-run`, and backups are judged
sufficient containment.

The offered ledger means prune is never a one-command operation. That is
deliberate: the evidence says a single-command deleter would have removed eight
working slash commands and a CLAUDE.md from the first machine it touched.
