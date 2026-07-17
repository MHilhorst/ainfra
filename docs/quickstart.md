# ainfra Quick Start

ainfra keeps a dev team's AI tooling in sync. You describe the setup once in `ainfra.yaml` — committed to your repo — and `ainfra install` reconciles any developer's machine to match. A lockfile pins exact versions and content hashes so you can verify nothing has drifted.

This guide walks both paths: joining a team that already uses ainfra, and authoring a setup from scratch.

## Install

```sh
brew install MHilhorst/ainfra/ainfra
# or: go install github.com/MHilhorst/ainfra/cmd/ainfra@latest
ainfra version
```

## Consuming a team setup

You cloned a repo that already contains an `ainfra.yaml`. There is no "initialize" step — the manifest already ships in the repo.

```sh
ainfra install                       # reconcile your machine to the manifest
ainfra install --agent codex         # render Codex config next to Claude Code
ainfra install --dry-run             # preview without writing
ainfra install --dry-run --strict    # CI gate: exit non-zero on any drift
```

`install` asks for confirmation before it touches anything (pass `--yes` to skip the prompt in CI). `--agent codex` applies the Codex-supported channels from the same manifest using a separate applied ledger, so Claude Code and Codex can be installed in the same repo without removing each other's resources. `--dry-run` is always safe and changes nothing. `--dry-run --strict` exits non-zero when there's anything to do, so it works as a CI gate. Remote sources (`github:`, `npm:`, `https:`) resolve at lock time and write through a content-addressed cache, so subsequent fetches are offline-capable. Gateway adapters remain the one follow-up.

`ainfra check` re-introspects every MCP server whose lockfile entry has a populated `toolsetHash`, compares the live `tools/list` against the locked per-tool description and input-schema hashes, and exits non-zero on drift with a per-tool diagnostic naming the changed tool.

## Adopting an existing repo

Your repo already has a Claude Code setup committed — `.mcp.json`, `.claude/`, `CLAUDE.md` — and you want to put it under ainfra without rewriting it by hand:

```sh
ainfra init --adopt              # draft an ainfra.yaml from the existing files
ainfra init --adopt --force      # throw the existing ainfra.yaml away and re-scan from scratch
```

`init --adopt` is the one-shot brownfield onramp. Once a manifest exists, the manifest is the source of truth — to reconcile on-disk drift back into matching it, run `ainfra install`. Adopt deliberately does not merge into an existing manifest.

Bootstrapping a shared team config repo from your own polished `~/.claude/`:

```sh
ainfra init team ../claude-config           # scaffold + git init + emit manifest from ~/.claude/
ainfra init team ../claude-config --empty   # …or scaffold a skeleton instead
```

`init --adopt` reads `.mcp.json`, `.claude/settings.json` hooks, `.claude/commands/*`, and `CLAUDE.md`, and emits a draft `ainfra.yaml`. Literal credentials it recognizes (`ghp_*`, `sk-*`, `xoxb-*`, and generic `token` / `key` / `password` keys) are stripped and replaced with `direct`-mode secret references plus a `TODO` marker for the vault path — nothing sensitive ends up in the manifest. Skills and tool permissions are skipped: skills arrive with `git clone`, and a clean permissions matcher is left for a later iteration.

## Clearing config you never declared

`ainfra install` only ever adds and updates, so a machine accumulates config no
manifest describes: an MCP server hand-added to `.mcp.json`, a skill dropped
into `.claude/skills/`, a slash command in `~/.claude/commands/`. `--prune`
removes it — but never the first time it sees it:

```sh
ainfra install --prune                                       # reports what is undeclared, removes nothing
ainfra add --global command ship ~/.claude/commands/ship.md  # keep the ones you want
ainfra install --prune                                       # removes what is still undeclared
```

Undeclared does not mean unwanted. Usually it means you never got around to
declaring it — the first machine this was tried on had eight undeclared slash
commands, every one of them in daily use. So the first run is always a report,
and only a later run removes what you saw and chose not to keep. Anything
removed is copied to `~/.config/ainfra/pruned/<timestamp>/` first.

The record of what you have been shown lives under `~/.config/ainfra/`, not in
the repo, and is keyed per machine and per agent. It is deliberately not
shareable: a ledger that travelled over git would arm deletions on a teammate's
first run, from a list they never saw.

**Use `--global` for anything in `~/.claude/`.** It applies in every repo, so a
declaration in one repo's `ainfra.personal.yaml` leaves it undeclared in all the
others — and a `--prune` run from a different repo would then remove it.
`--global` writes `~/.config/ainfra/personal.yaml`, which follows you
everywhere.

Flags go **before** the positional arguments (`ainfra add --global command ship
…`, not `ainfra add command ship … --global`). A flag placed after them is
silently ignored, and the entry lands in the team's `ainfra.yaml`.

There is deliberately no flag to skip the two-step. The gap between being shown
an entry and losing it is the point, and `--dry-run --prune` does not count as
having been shown: a preview never arms a removal.

`--prune` covers `mcpServers` (in the repo), `skills`, and `commands`, in both
the repo and your `~/.claude/`. It does **not** touch:

| Not pruned                                  | Why                                                        |
| ------------------------------------------- | ---------------------------------------------------------- |
| `hooks`                                     | ainfra cannot see hooks it did not install                  |
| `rules` (CLAUDE.md, AGENTS.md)              | not yet scoped; deferred                                    |
| personal MCP servers in `~/.claude.json`    | ainfra does not read that file yet                          |
| `tools`, `backgroundServices`               | would uninstall CLI binaries and tear down tunnels          |
| `plugins`, `marketplaces`                   | installed artifacts; removal is not free                    |

So `--prune` does not make your machine match your manifest exactly. It clears
undeclared config files, which is narrower.

## Authoring a setup from scratch

Starting a new team setup — most days you work through `add`, never touching `ainfra.yaml` by hand:

```sh
ainfra init                          # scaffold an ainfra.yaml
ainfra init --with-skill             # …or include the using-ainfra skill so AI agents
                                     #    in this repo learn ainfra's workflow
ainfra add mcp github                # add an MCP server (writes the entry + installs)
ainfra add command audit ./commands/audit.md   # add a slash command
ainfra list                          # see what's installed
git add ainfra.yaml ainfra.lock && git commit
```

Hand-editing still works and is the supported path for entries the CLI can't fully express (templates with complex `params:` blocks, hooks with embedded shell, secrets with referenced env vars).

`ainfra.lock` is the auto-generated lockfile that pins exact versions and content hashes. You commit it. Every teammate then resolves the manifest the same way.

`--with-skill` adds a `skills:` block sourcing [`using-ainfra`](../skills/using-ainfra/SKILL.md) from this repo. Once the skills channel resolver lands, every teammate's `ainfra install` materializes the skill into `.claude/skills/using-ainfra/`, and any AI agent working in the repo sees ainfra's install/add/remove/list workflow up front.

### Editor autocomplete

`ainfra install --print-schema` emits the JSON Schema for `ainfra.yaml`. Generate it once, then point your editor's YAML language server at it for inline autocomplete and validation:

```sh
ainfra install --print-schema > ainfra.schema.json
```

Add this first line to `ainfra.yaml`:

```yaml
# yaml-language-server: $schema=./ainfra.schema.json
```

The schema always matches the version of ainfra you have installed. The schema checks structure; `ainfra install --dry-run` checks the semantic rules on top.

### Your personal layer

Anything that is just yours — a personal MCP server, a local override — goes in a personal layer that is never committed. There are two flavors:

```sh
ainfra init --personal   # scaffold ainfra.personal.yaml in THIS repo (git-ignored)
```

```sh
# Or write once at the user level — applies to every ainfra-managed repo
# on this machine. Lives at $XDG_CONFIG_HOME/ainfra/personal.yaml
# (defaults to ~/.config/ainfra/personal.yaml).
mkdir -p ~/.config/ainfra
$EDITOR ~/.config/ainfra/personal.yaml
```

The repo-level `ainfra.personal.yaml` wins on conflict; the global one provides cross-repo personal tooling that follows you. Use the global layer for skills or MCP servers you want everywhere — your note-taking helper, a favourite filesystem MCP. Use the repo-level layer for one-off overrides scoped to that repo.

### Pin which ainfra version your repo expects

When teammates run different ainfra binary versions, they can produce slightly different lockfiles. Pin the version in `ainfra.yaml`:

```yaml
version: 1
ainfraVersion: "0.2.0"   # exact match; running a different ainfra prints a one-line warning
```

Missing field = no check (backward compatible). `AINFRA_QUIET=1` suppresses the warning if you genuinely want to run with a mismatched binary.

### Staleness warning on every Claude session

`ainfra install` writes a `SessionStart` hook into `.claude/settings.json` by default. The hook runs every time you open Claude Code in the repo and stays silent unless the manifest has changed since the last install — at which point it prints one stderr line suggesting you run `ainfra install` to refresh. No "in sync" chatter; the absence of a warning is the confidence signal.

Opt out by setting `stalenessWarning: false` at the manifest root:

```yaml
version: 1
stalenessWarning: false
```

The hook never blocks Claude (exit code is always 0) and runs the equivalent of a `git status` on the manifest hash, so the per-session cost is a few milliseconds.

## Worked example

`examples/multi-database/` is a complete manifest: four databases reached through SSH tunnels, defined with one template instantiated four times. Resolve it:

```sh
ainfra --chdir examples/multi-database install --dry-run
```

Each database server gets its own tunnel port, assigned by ainfra — no port is ever typed by hand. The example also carries a personal layer (`ainfra.personal.yaml`), so `ainfra install --dry-run` reports five MCP servers in total: four committed to `ainfra.lock`, and one resolved separately into `ainfra.personal.lock`.

## Command reference

| Command | What it does |
|---|---|
| `ainfra init` | Scaffold an `ainfra.yaml` (`--personal`, `--force`, `--with-skill`) |
| `ainfra init --adopt` | One-shot bootstrap: draft an `ainfra.yaml` from an existing `.mcp.json` / `.claude/` / `CLAUDE.md` setup (`--force` to re-scan). Use `install` for drift after that. |
| `ainfra init team <path>` | Scaffold a team config repo at `<path>`, scanning `~/.claude/` by default (`--empty` for a skeleton) |
| `ainfra install` | Reconcile the environment to the manifest (`--agent`, `--dry-run`, `--strict`, `--prune`, `--print-schema`, `--from <url>`) |
| `ainfra install --prune` | Also remove undeclared config. Reports on the first run, removes on a later one — see [Clearing config you never declared](#clearing-config-you-never-declared) |
| `ainfra add <ch> <id> [src]` | Add an entry to `ainfra.yaml` and reconcile (`--personal` for this repo only, `--global` for every repo on this machine) |
| `ainfra remove <ch> <id>` | Remove an entry and reconcile |
| `ainfra update [<ch> <id>]` | Re-resolve the lockfile and reinstall |
| `ainfra list` | List installed entries (`--channel`, `--json`) |
| `ainfra outdated` | Show entries with newer resolvable versions (`--strict`) |
| `ainfra version` | Print the ainfra version |

Rarely-used helpers (`lock`, `publish`, `installer`) keep working but are hidden from `ainfra --help` to keep the front page small.

Global flags: `--chdir <dir>` runs ainfra as if it had started in `<dir>`; `--no-color` disables colored output. Run `ainfra <command> --help` for detail on a single command.
