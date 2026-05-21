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

## Status

Phase 0 (foundation) is in progress. The build sequence is:

| Phase | Deliverable | State |
|-------|-------------|-------|
| 0 | This repo, design doc, validation gate | in progress |
| 1 | Manifest schema (`ainfra.yaml`) — [spec](spec/manifest-schema.md) | drafted, under validation |
| 2 | Lockfile schema (`ainfra.lock`) — [spec](spec/lockfile-schema.md) | drafted |
| 3 | Channel provider interface | planned |
| 4 | Resolution & precedence engine | planned |
| 5 | CLI surface (`init` / `plan` / `apply` / `check` / `lock`) | planned |

The schema is the product hypothesis; code is the proof. The schema is
validated *on paper* against five scenarios — see
[docs/validation.md](docs/validation.md) — before implementation code is written.

## Repository layout

```
cmd/ainfra/        CLI entry point
internal/           Implementation packages (filled in per the plan)
spec/               Schema specifications (Phase 1 & 2)
examples/           Worked manifests — multi-database is the hardest case
docs/               Design, validation gate, implementation plan
```

## Build

```sh
go build ./...
go run ./cmd/ainfra version
```
