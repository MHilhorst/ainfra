# ainfra

**Keep your whole dev team's AI tooling in sync.**

A package manager for your team's AI setup. You write one `ainfra.yaml`; `ainfra install` reconciles every teammate's machine to it — MCP servers, skills, hooks, rules, permissions. Same verbs you know from `npm` and `brew`. Works with Claude Code and Codex.

**[Quick start](docs/quickstart.md)** · **[Manifest schema](spec/manifest-schema.md)** · **[Design](docs/reference/design.md)**

## Install

```sh
brew install MHilhorst/ainfra/ainfra
```

Or `go install github.com/MHilhorst/ainfra/cmd/ainfra@latest`.

## The three commands you'll actually use

```sh
ainfra init --adopt    # capture whatever this repo already has into ainfra.yaml
ainfra install         # reconcile your machine to ainfra.yaml
ainfra add mcp github  # add something (writes the entry + installs it)
```

That's the whole daily loop. `init --adopt` works on empty repos too — it just gives you a starting manifest. After that, you mostly run `install` and `add`.

In CI, gate on drift:

```sh
ainfra install --dry-run --strict   # exits non-zero if a machine has drifted
```

## What `ainfra.yaml` looks like

```yaml
version: 1

mcpServers:
  github:
    transport: stdio
    command: npx
    args: ["-y", "@modelcontextprotocol/server-github"]
    version: "0.6.2"
    secret:
      token: github-token

secrets:
  github-token:
    mode: direct
    ref: "op://Private/github/token"   # a reference, never a stored value

hooks:
  gofmt-after-edit:
    event: PostToolUse
    matcher: "Edit|Write"
    command: "gofmt -w ."
```

ainfra writes the native config your tools already read — `.mcp.json`, the bundles under `.claude/`, `CLAUDE.md`. Nothing to lock into: stop using ainfra tomorrow and every file it wrote still works.

## Sharing a setup across repos

A team config repo holds the shared baseline:

```sh
ainfra init team ../claude-config   # scaffold from a lead's ~/.claude/
```

Every other repo pulls it in with one line:

```yaml
extends:
  - git+https://github.com/<org>/claude-config.git
```

`ainfra install` then merges three layers — team, repo, and your personal layer at `~/.config/ainfra/personal.yaml` — into the right places on disk.

## Commands

| Command | What it does |
|---------|--------------|
| `init` | Scaffold an `ainfra.yaml` (`--adopt`, `--personal`, `--with-skill`, `--force`) |
| `install` | Reconcile your machine to the manifest (`--dry-run`, `--strict`, `--from <url>`) |
| `add` / `remove` | Add or remove an entry and reconcile |
| `update` | Re-resolve the lockfile and reinstall |
| `list` / `inspect` | See what's installed |
| `outdated` | Show entries with newer versions (`--strict` for CI) |
| `version` | Print the ainfra version |

Run `ainfra --help` for everything.

## Why it stays in sync

`ainfra.lock` pins resolved versions and content hashes, so `install --dry-run --strict` catches drift — including silent upstream changes, like an MCP server quietly changing its tool descriptions. A `SessionStart` hook warns once when a teammate's manifest pull is sitting un-installed.

ainfra is *not* a runtime MCP gateway — it consumes gateways, secrets managers, and package managers as pluggable backends.

## Teach your agents to use ainfra

This repo ships a Claude Code skill. Reference it and every agent that lands in the repo learns the workflow:

```yaml
skills:
  using-ainfra:
    source: "github:MHilhorst/ainfra/skills/using-ainfra"
    version: "0.1.0"
```

Or scaffold it with `ainfra init --with-skill`.

## Build

```sh
go build ./...
go test ./...
```

See [docs/](docs/) for the quick start, design notes, and worked examples.
