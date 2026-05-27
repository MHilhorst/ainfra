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
ainfra install --dry-run             # preview without writing
ainfra install --dry-run --strict    # CI gate: exit non-zero on any drift
```

`install` asks for confirmation before it touches anything (pass `--yes` to skip the prompt in CI). `--dry-run` is always safe and changes nothing. `--dry-run --strict` exits non-zero when there's anything to do, so it works as a CI gate. Two features are not yet implemented: fetching sources from remote locations (git/npm) and gateway adapters. Local source files and inline MCP server definitions work today.

## Authoring a setup

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

Anything that is just yours — a personal MCP server, a local override — goes in a personal layer that is never committed:

```sh
ainfra init --personal   # scaffold ainfra.personal.yaml (git-ignored)
```

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
| `ainfra install` | Reconcile the environment to the manifest (`--dry-run`, `--strict`, `--print-schema`, `--from <url>`) |
| `ainfra add <ch> <id> [src]` | Add an entry to `ainfra.yaml` and reconcile |
| `ainfra remove <ch> <id>` | Remove an entry and reconcile |
| `ainfra update [<ch> <id>]` | Re-resolve the lockfile and reinstall |
| `ainfra list` | List installed entries (`--channel`, `--json`) |
| `ainfra outdated` | Show entries with newer resolvable versions (`--strict`) |
| `ainfra version` | Print the ainfra version |

Deprecated aliases (`apply`, `plan`, `check`, `validate`, `schema`, `sync`, `exec`, `history`) and rarely-used helpers (`lock`, `publish`, `installer`) keep working but are hidden from `ainfra --help` to keep the front page small. Set `AINFRA_QUIET=1` to suppress the deprecation warnings.

Global flags: `--chdir <dir>` runs ainfra as if it had started in `<dir>`; `--no-color` disables colored output. Run `ainfra <command> --help` for detail on a single command.
