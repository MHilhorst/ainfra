# ainfra Quick Start

ainfra keeps a dev team's AI tooling in sync. You describe the setup once in `ainfra.yaml` — committed to your repo — and `ainfra apply` reconciles any developer's machine to match. A lockfile pins exact versions and content hashes so you can verify nothing has drifted.

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
ainfra plan     # preview what would change on your machine
ainfra apply    # reconcile your machine to the manifest
ainfra check    # verify nothing has drifted (safe to run anytime, incl. CI)
```

`plan` is always safe — it changes nothing. `apply` asks for confirmation before it touches anything (pass `--yes` to skip the prompt in CI). `check` exits non-zero on drift, so it works as a CI gate.

`ainfra plan` requires a committed `ainfra.lock`; run `ainfra lock` once after editing the manifest. Two features are not yet implemented: fetching sources from remote locations (git/npm) and gateway adapters. Local source files and inline MCP server definitions work today.

## Authoring a setup

```sh
ainfra init        # scaffold an ainfra.yaml
# edit ainfra.yaml — add cliTools, mcpServers, hooks, commands
ainfra validate    # static-check the manifest
ainfra lock        # resolve it and write ainfra.lock
git add ainfra.yaml ainfra.lock && git commit
```

`ainfra.lock` is the auto-generated lockfile that pins exact versions and content hashes. You commit it. Every teammate then resolves the manifest the same way.

### Editor autocomplete

`ainfra schema` prints the JSON Schema for `ainfra.yaml`. Generate it once, then point your editor's YAML language server at it for inline autocomplete and validation:

```sh
ainfra schema > ainfra.schema.json
```

Add this first line to `ainfra.yaml`:

```yaml
# yaml-language-server: $schema=./ainfra.schema.json
```

The schema always matches the version of ainfra you have installed. The schema checks structure; `ainfra validate` checks the semantic rules on top.

### Your personal layer

Anything that is just yours — a personal MCP server, a local override — goes in a personal layer that is never committed:

```sh
ainfra init --personal   # scaffold ainfra.personal.yaml (git-ignored)
```

## Worked example

`examples/multi-database/` is a complete manifest: four databases reached through SSH tunnels, defined with one template instantiated four times. Resolve it:

```sh
ainfra --chdir examples/multi-database lock
```

Each database server gets its own tunnel port, assigned by ainfra — no port is ever typed by hand. The example also carries a personal layer (`ainfra.personal.yaml`), so `ainfra lock` reports five MCP servers in total: four committed to `ainfra.lock`, and one resolved separately into `ainfra.personal.lock`.

## Command reference

| Command | What it does |
|---|---|
| `ainfra init` | Scaffold an `ainfra.yaml` (`--personal`, `--force`) |
| `ainfra validate` | Static-check the manifest without resolving it |
| `ainfra schema` | Print the JSON Schema for `ainfra.yaml` |
| `ainfra lock` | Resolve the manifest and write `ainfra.lock` |
| `ainfra plan` | Preview the diff between desired and observed state |
| `ainfra apply` | Reconcile the environment to the manifest |
| `ainfra check` | Verify the environment matches the lockfile |
| `ainfra publish` | Package the resolved lockfile into a subscriber artifact |
| `ainfra installer` | Generate a one-time macOS installer for subscriber machines |
| `ainfra version` | Print the ainfra version |

Global flags: `--chdir <dir>` runs ainfra as if it had started in `<dir>`; `--no-color` disables colored output. Run `ainfra <command> --help` for detail on a single command.
