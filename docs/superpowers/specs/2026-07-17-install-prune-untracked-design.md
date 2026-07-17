# `ainfra install --prune`: resetting a machine to the ainfra setup

Date: 2026-07-17
Status: Approved design, not yet implemented

## Problem

`ainfra install` converges the machine toward `ainfra.yaml`, but it only ever
adds and updates. A resource ainfra never recorded as its own is left alone by
design (`internal/provider/diff.go::DiffResources`). Over time a machine
accumulates config that no manifest describes and no install removes — an MCP
server hand-added to `.mcp.json`, a skill dropped into `.claude/skills/`, a
rule fragment left behind by an experiment. There is no way to get back to a
known state short of deleting files by hand.

`ainfra inspect` already detects this: it classifies every entry as tracked,
untracked, or missing (`cmd/ainfra/cmd_inspect.go::summarize`). Its only remedy
is `ainfra init --adopt --force`, which folds the local entries *into* the
manifest. That is the right answer when the local additions are wanted. It is
the wrong answer when they are cruft.

Note: there is no `ainfra install --force`. `--force` exists only on `init` and
`adopt`, where it means "throw the existing `ainfra.yaml` away and re-scan"
(`cmd/ainfra/cmd_init.go`, `cmd/ainfra/cmd_adopt.go`). That is a
manifest-authoring escape hatch, unrelated to machine state. This design adds a
new mode; it does not strengthen an existing one.

## Goal

Let a person run one command on their own machine and end up with the ainfra
setup and nothing else: **anything declared in ainfra — team, repo, or their own
personal layer — stays; anything they never declared is cleared.**

This is self-service, not enforcement. The person opts in by typing `--prune` on
their own machine. Nothing prunes anyone else's config, and a normal
`ainfra install` never prunes.

### Honest scope

`--prune` covers `skills`, `commands`, and `rules` in both the repo and the
person's `~/.claude`, plus `mcpServers` in the repo. It is **not** "nothing but
ainfra remains". Specifically, these survive a prune:

- **Untracked hooks**, always. Structural, not incidental — see
  "Why `hooks` is unpruneable".
- **Personal MCP servers in `~/.claude.json`**, always. ainfra cannot see them
  yet — see "Why personal MCP servers survive".
- **CLI tools, background services, plugins, marketplaces**, by choice — see
  "The `Pruner` interface".

The plan output and docs must not describe `--prune` as making a machine match
its manifest exactly. It clears untracked *config files*, which is a narrower
and honest claim.

## Non-goals

- **Auto-convergence.** `--prune` is opt-in per run. A manifest policy
  (`pruning: strict`) applying to everyone automatically was considered and
  rejected as too destructive for the default path. Revisit only with evidence.
- **Uninstalling CLI tools or tearing down background services.** A prune should
  not cost someone their brew packages or their prod-DB tunnels.
- **An `ainfra restore` command.** Backups make a mistaken prune recoverable;
  one-command undo is deferred.

## Keeping something: the personal layer

The escape hatch already exists. ainfra has a personal manifest layer —
`ainfra.personal.yaml` / `ainfra.personal.lock`, `manifest.LayerPersonal` — and
personal entries route to user scope automatically
(`cmd/ainfra/commands.go::partitionLockByLayer`).

So the rule a user needs to understand is one sentence: **if you want to keep
it, declare it** — `ainfra add <channel> <id> --personal`. A declared personal
skill is tracked, and prune leaves it alone. An ad-hoc one is cruft, and prune
clears it.

This is what makes user-scope pruning defensible rather than hostile: the person
has a supported way to say "this is mine, keep it", and they chose to run the
command.

## Prior art in the codebase

The delete-something-ainfra-never-installed mechanic already exists. A desired
resource flagged `Tombstone` (`enabled: false` in the manifest) is an explicit
"ensure absent": `DiffResources` removes it whenever present on the machine,
*even if ainfra never installed it* — the observed-but-not-prior case
(`internal/provider/diff.go`). The existing comment draws the exact axis this
design extends:

> This is what makes "absent from the manifest" (leave alone) differ from
> "explicitly retired" (remove everywhere).

`--prune` adds a third policy on that axis: "absent from the manifest and
pruning is on" → remove. It reuses the tombstone semantics, changing only which
resources receive the flag.

## Design

### Control flow

1. `ainfra install --prune` sets `Prune: true` on **both** the repo-scope and
   user-scope orchestrators.
2. Each orchestrator passes `DiffOpts{Prune: true}` to `DiffResources`, but only
   for providers implementing `Pruner`.
3. `DiffResources` synthesizes a `ChangeDelete` for each observed resource in
   neither `desired` nor `prior`.
4. Before `Apply`, the orchestrator calls `Pruner.Backup` for each synthesized
   delete. A backup that fails cancels that delete.
5. `Apply` executes, unchanged.

Everything downstream of `Change` is untouched: plan rendering, the confirm
prompt, `--dry-run`, `--strict`, and `Apply` all already speak `Change`.

`--dry-run --prune` is an honest preview because it runs the identical code
path rather than a simulation of it. Users should be told to run it first.

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
decide what to back up — a structural flag rather than a `Detail` string match,
which would be fragile.

### The user-scope orchestrator must be forced

**This is the trap that would make the feature silently useless.** Today the
user-scope orchestrator is constructed only when the rendered set has
personal-layer entries or the user ledger is non-empty
(`cmd/ainfra/commands.go`):

```go
if herr == nil && (anyResources(userRendered) || userLedgerNonEmpty) {
	userEnv := env
	userEnv.Root = home
	userEnv.UserScope = true
	userOrch = provider.NewOrchestratorScoped(home, provider.ScopeUser, userEnv, providers)
```

A person with no personal-layer entries and an empty user ledger gets **no
user-scope orchestrator at all** — and that is exactly the person whose
`~/.claude` has the most untracked cruft. Prune would appear to run, report
success, and skip their personal config entirely.

The condition must therefore gain `|| prune`: when pruning, the user-scope
orchestrator is always constructed, because the whole point is to reconcile a
scope that may have nothing declared in it.

Providers need no per-scope logic: `userEnv.Root = home` means
`skillsDir(env)` resolves to `$HOME/.claude/skills` and `commandsDir(env)` to
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

Implemented by: `MCP`, `Skills`, `Commands`, `Rules`. These are the providers
whose `Observe` enumerates on-disk state and can therefore see untracked
entries:

- `MCP.Observe` reads `env.Root/.mcp.json` (`mcp.go::mcpPath`).
- `Skills.Observe` lists `env.Root/.claude/skills/` (`skills.go::skillsDir`).
- `Commands.Observe` lists `env.Root/.claude/commands/` (`commands.go::commandsDir`).
- `Rules.Observe` lists `.claude/ainfra/*.md` under the repo and `$HOME`
  (`rules.go`).

Deliberately **not** implemented by:

| Channel              | Why not                                            |
| -------------------- | -------------------------------------------------- |
| `hooks`              | Cannot work — see below.                           |
| `tools`              | Pruning would uninstall CLI binaries (brew).       |
| `backgroundServices` | Pruning would tear down the prod-DB tunnels.       |
| `plugins`            | Installed artifacts; reinstall is not free.        |
| `marketplaces`       | Removing one silently breaks plugins that need it. |

Adding a channel to the pruneable set is a deliberate act of implementing an
interface, reviewable in a PR.

#### A note on `Rules.Observe` and scope

`Rules.Observe` scans both `env.Root/.claude/ainfra` and
`env.Home/.claude/ainfra` in every scope, because a rule's fragment is
co-located with its target and a `~`-prefixed target is user-level
(`rules.go::fragmentFor`).

Under this design that is correct rather than a leak: the user is resetting
their own machine, so reaching `$HOME` is the intent. It does mean a repo-scope
prune and a user-scope prune both observe the `$HOME` fragments, so the two
plans can name the same resource. The orchestrator must not double-delete or
double-back-up; the user-scope plan owns `$HOME` fragments and the repo-scope
plan must filter them out. This is the one place where prune needs genuinely new
scope logic.

#### Why `hooks` is unpruneable

`Hooks.Observe` does not read the machine: it sources from the applied ledger
(`internal/provider/claudecode/hooks.go`). Its own comment explains why —
Claude Code's `settings.json` keys hooks by event, not by ainfra hook id, so the
written file cannot be mapped back to managed hooks, and the ledger is the only
authoritative record of what was applied.

The consequence is structural: for hooks, `observed` is by definition equal to
`prior`, so the set "observed but not in prior" — the exact set prune operates
on — is *always empty*. A hand-added hook in `settings.json` is invisible to
`Observe` and cannot be detected, let alone removed.

This makes hooks not merely unsupported but actively dangerous to include: a
`--prune` that appeared to cover hooks would report success having done nothing,
giving false confidence. Supporting them requires an `Observe` that can
attribute settings.json entries back to ainfra — a separate problem.

#### Why personal MCP servers survive

`MCP` implements `Pruner`, but it only reaches the repo's `.mcp.json`. Claude
Code reads user-level MCP servers from `~/.claude.json`, a different file with a
different format, and ainfra does not write or read it yet — there is a live
warning to this effect at `cmd/ainfra/commands.go::runInstall`
(`provider.HasUserScopeMCP`).

In user scope, `MCP.Observe` would read `$HOME/.mcp.json`, which is not where
personal MCP servers live. It will typically find nothing; if that file does
exist it is not the user's real MCP config.

Therefore: **`MCP` must be skipped entirely in the user-scope prune**, not
allowed to run against the wrong path. A person's hand-added personal MCP
servers cannot be cleared by this feature. When user-scope MCP lands, this
restriction should be revisited; until then the docs must say so plainly rather
than let users assume their personal MCP list was reset.

### Backup

**Why provider-side.** The obvious design — have the orchestrator serialize
`Change.Resource.Payload` — does not work. `Observe` does not populate
`Payload`: `Skills.Observe` returns only `ID` and `Channel`, `Rules.Observe` the
same, and `MCP.Observe` returns `ID` and `ContentHash`. An orchestrator-level
backup would write empty files while reporting success, losing exactly the data
it claims to protect. Only the provider knows its own on-disk layout.

**Destination.** `<scope root>/.ainfra/pruned-<RFC3339-timestamp>/<channel>/`,
created once per run per scope. `.ainfra/` is already git-ignored and already
hosts the applied ledger (`internal/provider/applied.go::appliedPath`), so this
introduces no new location and nothing lands in git.

**Per-channel semantics:**

- `Skills`: copy the tree at `.claude/skills/<id>/` to `skills/<id>/`.
- `Commands`: copy `.claude/commands/<id>.md` to `commands/<id>.md`.
- `Rules`: copy the fragment `.claude/ainfra/<id>.md` to `rules/<id>.md`.
- `MCP`: re-read `.mcp.json` and write the entry's JSON fragment to
  `mcpServers/<id>.json`.

**Retention:** none. Backups accumulate under `.ainfra/`; the directory is
git-ignored and the user can delete it. A retention policy is deferred until
someone complains.

**Restore:** manual, by copying the tree back. The backup exists so a mistaken
prune is *recoverable*, not so it is a one-command undo.

### Error handling

- **Backup fails for a resource** → that resource's delete is dropped from the
  plan and surfaced as a `ChangeFailure` via the existing aggregation
  (`internal/provider/provider.go::ApplyError`). Never delete what could not be
  backed up.
- **Backup directory cannot be created** → abort the run before any `Apply`, so
  a run cannot half-prune with no backups.
- **`--dry-run`** → no backup directory is created and nothing is copied. The
  plan still lists the deletes.
- **`os.UserHomeDir()` fails with `--prune`** → abort with an error rather than
  silently pruning repo scope only. A partial prune the user did not ask for is
  worse than no prune.
- **Partial apply** → unchanged; the existing `ChangeFailure` / `ChangeSkip`
  machinery covers it.

### Applied ledger

No change. Pruned resources are by definition absent from the ledger (that is
what makes them untracked), so there is nothing to record or remove. The
post-apply `WriteApplied` snapshot of desired state is already correct.

## Testing

Unit, `internal/provider/diff_test.go`:

- Untracked observed resource → `ChangeDelete` when `Prune: true`.
- Same resource → no change when `Prune: false`. This guards the historical
  contract and is the regression test that matters most.
- `DiffOpts{}` zero value behaves exactly as the old signature did.
- Tombstone still wins over prune for the same ID (no duplicate deletes).
- Prior-but-not-desired still produces exactly one delete under prune.
- Desired resources are unaffected by prune.

Unit, orchestrator / `cmd`:

- **`--prune` constructs the user-scope orchestrator even when the rendered set
  has no personal entries and the user ledger is empty.** Direct regression test
  for the trap above; without it the feature no-ops for the people who need it.
- A provider not implementing `Pruner` never receives `Prune: true`.
- `MCP` is not pruned in user scope.
- Backup is called before `Apply` for each pruned delete.
- Backup failure cancels that delete and leaves the resource on disk.
- Backup-dir creation failure aborts before any `Apply`.
- A `$HOME` rule fragment is deleted once, by the user-scope plan only.

Provider-level:

- `Skills.Backup` round-trips a hand-written skill directory: prune it, restore
  from the backup, assert the tree matches byte-for-byte.
- `MCP.Backup` writes the untracked server's JSON fragment, not an empty file.
  Direct regression test for the `Payload`-is-empty trap.
- Table test over the provider set asserting `_, ok := p.(Pruner)` matches
  expectation, so quietly implementing `Backup` on an excluded provider fails
  the build rather than silently widening the blast radius.

E2E, `cmd/ainfra/`:

- `install --prune --dry-run` lists untracked deletes and changes nothing.
- `install --prune --yes` removes an untracked repo MCP server, skill, and
  command; leaves tracked ones, an untracked `tools` entry, and an untracked
  hook alone.
- `install --prune --yes` removes an untracked skill from `$HOME/.claude/skills`
  and leaves a personal-layer-declared skill alone. This is the test that proves
  "declare it to keep it" works.
- `install` without `--prune` leaves every untracked entry alone. The headline
  safety property.

## Open questions

1. Should `inspect`'s `nextStepHints` offer `install --prune` alongside
   `init --adopt --force`, so both remedies for untracked entries are
   discoverable? Probably yes; low cost.
2. Should `--prune` print a summary of what it *cannot* clear (hooks, personal
   MCP servers) after a run, so users are not left believing their machine is
   now exactly ainfra? Leaning yes — the honest-scope problem is the main way
   this feature could mislead.

## Accepted tradeoff

`--prune` puts destructive behavior behind a flag on the command everyone runs
reflexively. A separate `ainfra prune` command was considered — a scary verb
deserving its own name — but rejected because it would duplicate plan
rendering, confirmation, and `--dry-run`, and would drift from `install`. The
`Pruner` interface, the confirm prompt, `--dry-run`, backups, and the fact that
a person only ever prunes their own machine are judged sufficient containment.
