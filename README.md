# ainfra

**Keep your whole dev team's AI tooling in sync.**

Your teammates' AI development setup drifts from yours the moment they install
it. Different MCP servers, different skills, different hooks, different rules
files — and no way to see the gap. ainfra makes a team's AI tooling
config-as-code: define it once in your repo, and every developer reproduces it
identically with one command.

Because ainfra pins and hashes every resolved version into a lockfile, "we're
in sync" is something you can *verify* — not hope for: a skill or server that
drifts on a teammate's machine, or changes silently upstream, gets caught.

## What this is — and is not

ainfra is declarative config-as-code for a team's AI tooling — a Terraform-style
CLI: a declarative manifest, `plan` before `apply`, a lockfile separating
desired from observed state. It is *not* a runtime MCP gateway — that category
is saturated and on the official MCP roadmap. ainfra *consumes* gateways,
secrets managers, and package managers as pluggable backends; it owns none of
their runtimes.

There is nothing to lock into. ainfra reconciles a machine by writing the
native config your AI tools already read — `.mcp.json`, the bundles under
`.claude/`, `CLAUDE.md`. It is what puts those files in place and keeps them
verified, not a runtime they depend on. Stop using ainfra tomorrow and every
file it wrote still works, untouched.

See [docs/design.md](docs/design.md) for the full, decided design.

## Quick start

```sh
go install github.com/MHilhorst/ainfra/cmd/ainfra@latest
# or, from a checkout of this repo:
go build -o ainfra ./cmd/ainfra

ainfra version
```

A developer joining a team runs `ainfra plan` then `ainfra apply`. Someone
authoring a setup runs `ainfra init`, edits `ainfra.yaml`, then `ainfra lock`.

The [`ainfra.yaml`](ainfra.yaml) at the repository root is a worked showcase —
a small team setup (MCP servers, a CLI tool, a hook) you can read in 30 seconds
and try with `ainfra validate`. See [docs/quickstart.md](docs/quickstart.md)
for the full walkthrough and [`examples/multi-database/`](examples/multi-database/)
for the hardest case.

Run `ainfra --help` for the command list, or `ainfra <command> --help` for
per-command detail.

## Commands

| Command | What it does |
|---------|--------------|
| `init` | Scaffold an `ainfra.yaml` in the current repo (`--personal`, `--force`) |
| `validate` | Static-check the manifest without resolving it |
| `schema` | Print the JSON Schema for `ainfra.yaml` — point an editor at it for autocomplete |
| `lock` | Resolve the manifest and write `ainfra.lock` |
| `plan` | Preview the diff between desired and observed state |
| `apply` | Reconcile the environment to the manifest — or to a published artifact with `--from` |
| `check` | Verify the environment matches the lockfile (or a `--from` artifact); report drift |
| `publish` | Package the resolved lockfile into a subscriber artifact (`--out`) |
| `installer` | Generate a one-time macOS installer for subscriber machines (`--out`) |
| `version` | Print the ainfra version |

Global flags: `--chdir <dir>` runs as if started elsewhere; `--no-color`
disables colored output.

## Status

The manifest and lockfile schemas, the resolution engine, the channel provider
layer, and the full CLI surface are built. `init`, `validate`, `schema`, `lock`,
`version`, `plan`, `apply`, and `check` all work end to end.

ainfra reconciles a Claude Code environment today. The manifest also carries an
`agent:` field (`claude-code`, `codex`, or `claude-desktop`) and capability-gates
each channel per agent — so the schema is already agent-agnostic. Remote (git/npm)
source fetching, the pluggable secret resolver, gateway adapters, and the Codex
renderer are follow-up phases.

### Subscriber mode — non-engineers

Engineers manage AI tooling as config-as-code in the repo. Non-engineers (sales,
support) need the MCP servers in their **Claude Desktop app**, with no repo and
no terminal. ainfra bridges this without owning a runtime:

- `ainfra publish` packages the resolved lockfile into a hash-pinned **artifact**
  (`ainfra.lock` + rendered resources + an `ainfra.sub.json` descriptor +
  `MANIFEST.sha256`). The team hosts that artifact at a URL.
- `ainfra apply --from <url>` reconciles a machine against the artifact —
  rendering `claude_desktop_config.json` — with no repo and no manifest. A
  failed fetch is a safe no-op: the machine stays on last-known-good config.
- `ainfra installer` emits a one-time macOS installer that drops a launchd job
  running `apply --from` at login and on a configurable interval.

The `publish:` block in `ainfra.yaml` configures the artifact URL, target agent,
and sync cadence — the team owns every knob; the subscriber configures nothing.
See [docs/superpowers/specs/2026-05-22-subscriber-mode-design.md](docs/superpowers/specs/2026-05-22-subscriber-mode-design.md).

| Phase | Deliverable | State |
|-------|-------------|-------|
| 0 | Repo, design doc, validation gate | done |
| 1 | Manifest schema (`ainfra.yaml`) — [spec](spec/manifest-schema.md) | done |
| 2 | Lockfile schema (`ainfra.lock`) — [spec](spec/lockfile-schema.md) | done |
| 3 | Channel provider interface — powers `plan` / `apply` / `check` | done |
| 4 | Resolution & precedence engine | done |
| 5 | CLI surface — all eight commands | done |

The schema is the product hypothesis; code is the proof. Phases 1 and 2 were
validated *on paper* against five scenarios — see
[docs/validation.md](docs/validation.md) — before implementation began.

## Repository layout

```
ainfra.yaml      Showcase manifest — a small team setup, the read-in-30s example
cmd/ainfra/      CLI entry point, command definitions, reconcile wiring
internal/
  agent/         registry of target AI agents and each one's channel capabilities
  cli/           command registry, dispatch, flags, help
  diag/          structured diagnostic error type
  graph/         dependency graph and topological sort
  lockfile/      ainfra.lock schema, content hashing, read/write
  manifest/      ainfra.yaml schema, strict layer loading, validation
  provider/      channel reconciliation — plan/apply/check, diff, environment
    agentset/    assembles the provider set for the resolved target agent
    claudecode/  Claude Code channel providers (mcp, hooks, commands, rules, …)
    shared/      agent-agnostic providers (the cliTools substrate)
    fetch/       retrieve channel-entry bundles from their declared sources
    fsmerge/     filesystem materialization and merge helpers
    pkg/         package-registry resolution for package-launched MCP servers
    precond/     precondition checks (DNS, TCP, file, command)
  resolve/       template instantiation, layer merge, port allocation, lock pipeline
  schema/        JSON Schema generation for ainfra.yaml (reflected from manifest)
  ui/            terminal rendering — color, plan diffs, errors, prompts
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
