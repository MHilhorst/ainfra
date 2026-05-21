# ainfra

A Terraform-style CLI that defines a team's Claude Code setup as layered
config-as-code and reconciles it — with a lockfile — onto any developer's machine.

## The problem

A team has no way to define its Claude Code setup once and guarantee every
developer reproduces it identically. Config is scattered across separate
mechanisms with separate scopes: MCP servers, skills, plugins, `CLAUDE.md`,
tool permissions, and the CLI binaries they all depend on. There is no single
source of truth and no lockfile. "Works on my machine" is unverifiable, drift
goes unnoticed, and a server or skill that was safe yesterday can change
silently.

## What this is — and is not

`ainfra` owns **declarative, cross-channel reconciliation with a lockfile**.
That is the unowned cell in the market. It is *not* a runtime MCP gateway —
that category is saturated and on the official MCP roadmap. `ainfra` *consumes*
gateways, secrets managers, and package managers as pluggable backends; it owns
none of their runtimes.

See [docs/design.md](docs/design.md) for the full, decided design.

## Quick start

```sh
go build -o ainfra ./cmd/ainfra
ainfra version
```

A developer joining a team runs `ainfra plan` then `ainfra apply`. Someone
authoring a setup runs `ainfra init`, edits `ainfra.yaml`, then `ainfra lock`.
See [docs/quickstart.md](docs/quickstart.md) for the full walkthrough and the
`examples/multi-database/` worked example.

Run `ainfra --help` for the command list, or `ainfra <command> --help` for
per-command detail.

## Commands

| Command | What it does |
|---------|--------------|
| `init` | Scaffold an `ainfra.yaml` in the current repo (`--personal`, `--force`) |
| `validate` | Static-check the manifest without resolving it |
| `lock` | Resolve the manifest and write `ainfra.lock` |
| `plan` | Preview the diff between desired and observed state *(pending the provider layer)* |
| `apply` | Reconcile the environment to the manifest *(pending the provider layer)* |
| `check` | Verify the environment matches the lockfile; report drift *(pending the provider layer)* |
| `version` | Print the ainfra version |

Global flags: `--chdir <dir>` runs as if started elsewhere; `--no-color`
disables colored output.

## Status

The manifest and lockfile schemas, the resolution engine, and the CLI surface
are built. `init`, `validate`, `lock`, and `version` work end to end. `plan`,
`apply`, and `check` are specified and stubbed — their real behaviour depends on
the channel provider layer, which is the next build phase.

| Phase | Deliverable | State |
|-------|-------------|-------|
| 0 | Repo, design doc, validation gate | done |
| 1 | Manifest schema (`ainfra.yaml`) — [spec](spec/manifest-schema.md) | implemented |
| 2 | Lockfile schema (`ainfra.lock`) — [spec](spec/lockfile-schema.md) | implemented |
| 3 | Channel provider interface — powers `plan` / `apply` / `check` | next |
| 4 | Resolution & precedence engine | done |
| 5 | CLI surface (`init` / `validate` / `lock` / `version`) | done |

The schema is the product hypothesis; code is the proof. Phases 1 and 2 were
validated *on paper* against five scenarios — see
[docs/validation.md](docs/validation.md) — before implementation began.

## Repository layout

```
cmd/ainfra/      CLI entry point and command definitions
internal/
  cli/           command registry, dispatch, flags, help
  ui/            terminal rendering — color, plan diffs, errors, prompts
  diag/          structured diagnostic error type
  manifest/      ainfra.yaml schema, layer loading, validation
  resolve/       template instantiation, layer merge, port allocation, lock pipeline
  graph/         dependency graph and topological sort
  lockfile/      ainfra.lock schema, content hashing, read/write
  version/       build version
spec/            Manifest and lockfile schema specifications
examples/        Worked manifests — multi-database is the hardest case
docs/            Design, validation gate, quick start, specs and plans
```

## Build

```sh
go build ./...
go test ./...
```
