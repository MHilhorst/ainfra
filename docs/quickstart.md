# ainfra Quick Start

ainfra defines a team's Claude Code setup as config-as-code and reconciles it,
with a lockfile, onto any developer's machine. This guide walks both paths:
joining a team that already uses ainfra, and authoring a setup from scratch.

## Install

```sh
go install github.com/MHilhorst/ainfra/cmd/ainfra@latest
# or, from a checkout of this repo:
go build -o ainfra ./cmd/ainfra   # then move ./ainfra onto your PATH
```

Check it works:

```sh
ainfra version
```

## Consuming a team setup

You cloned a repo that already contains an `ainfra.yaml`. There is no
"initialize" step — the manifest ships in the repo.

```sh
ainfra plan     # preview what would change on your machine
ainfra apply    # reconcile your machine to the manifest
ainfra check    # verify nothing has drifted (safe to run anytime, incl. CI)
```

`plan` is always safe — it changes nothing. `apply` asks for confirmation
before it touches anything. `check` exits non-zero when it finds drift, so it
works as a CI gate.

> `plan`, `apply`, and `check` are specified but not yet built — they depend on
> the channel provider layer, the next build phase. Running them today prints a
> short notice. `lock`, `init`, and `validate` work now.

## Authoring a setup

Starting a new team setup:

```sh
ainfra init        # scaffold an ainfra.yaml
# edit ainfra.yaml — add cliTools, mcpServers, hooks, commands
ainfra validate    # static-check the manifest
ainfra lock        # resolve it and write ainfra.lock
git add ainfra.yaml ainfra.lock && git commit
```

`ainfra.lock` is committed; it pins exact versions and content hashes so every
teammate resolves identically.

### Editor autocomplete

`ainfra schema` prints the JSON Schema for `ainfra.yaml`. Generate it once and
point your editor's YAML language server at it for autocomplete and inline
validation while you edit:

```sh
ainfra schema > ainfra.schema.json
```

Then add this first line to `ainfra.yaml`:

```yaml
# yaml-language-server: $schema=./ainfra.schema.json
```

The schema is reflected from ainfra's own types, so it always matches the
version of ainfra you have installed. It checks structure; `ainfra validate`
checks the semantic rules on top.

### Your personal layer

Anything that is just yours — a personal MCP server, a local override — goes in
a personal layer that is never committed:

```sh
ainfra init --personal   # scaffold ainfra.personal.yaml (git-ignored)
```

## Worked example

`examples/multi-database/` is a complete manifest: four databases reached
through SSH tunnels, expressed as one template instantiated four times. Resolve
it:

```sh
ainfra --chdir examples/multi-database lock
```

Each of the four database servers gets a distinct, tool-allocated tunnel port
— no port is ever typed by hand. The example also carries a personal layer
(`ainfra.personal.yaml`), so `ainfra lock` reports five MCP servers in total:
four committed to `ainfra.lock`, one resolved separately into
`ainfra.personal.lock`.

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
| `ainfra version` | Print the ainfra version |

Global flags: `--chdir <dir>` runs as if started elsewhere; `--no-color`
disables colored output. `ainfra <command> --help` prints per-command detail.
