# ainfra

**Keep your whole dev team's AI tooling in sync.**

A package manager for your team's AI development setup — a declarative manifest, a content-hashed lockfile that catches drift, and the same verbs you already know from `npm` and `brew`. Supports Claude Code and Codex.

**[Quick start](docs/quickstart.md)** · **[Manifest schema](spec/manifest-schema.md)** · **[Design](docs/reference/design.md)** · **[Worked example](examples/multi-database/)**

---

## Install

```sh
brew install MHilhorst/ainfra/ainfra
```

Or `go install github.com/MHilhorst/ainfra/cmd/ainfra@latest`.

## Try it

**Every repo starts the same way:** `ainfra init --adopt` captures whatever Claude Code config the repo already has (`.mcp.json`, `.claude/`, `CLAUDE.md` — or nothing) into `ainfra.yaml`. From there, `ainfra install` reconciles all three layers — team (via `extends:`), repo, and your global personal layer at `~/.config/ainfra/personal.yaml` — into the right places on disk.

```sh
ainfra init --adopt                  # bootstrap ainfra.yaml (works on empty repos too)
ainfra install                       # reconcile your machine to the manifest
ainfra install --dry-run --strict    # CI gate: exit non-zero on drift
```

Authoring a new setup from scratch — most days you work through `add`, never touching YAML by hand:

```sh
ainfra add mcp github                # add an MCP server (writes the entry + installs)
ainfra list                          # see what's installed
```

Bootstrapping a shared **team config repo** from a lead engineer's own `~/.claude/`:

```sh
ainfra init team ../claude-config           # scaffold + git init + emit manifest from ~/.claude/
ainfra init team ../claude-config --empty   # or scaffold a skeleton manifest
```

Once a team repo exists, downstream repos pull it in by adding `extends:` to their own `ainfra.yaml`:

```yaml
extends:
  - git+https://github.com/<org>/claude-config.git
```

Run `ainfra --help` for the full command list. The [quick start](docs/quickstart.md) is the full walkthrough; the [`ainfra.yaml`](ainfra.yaml) at the repo root is a worked example you can read in 30 seconds.

## What it looks like

```yaml
# ainfra.yaml — committed to your repo
version: 1

secrets:
  github-token:
    mode: direct
    ref: "op://Private/github/token"    # a reference, never a stored value

mcpServers:
  github:
    transport: stdio
    command: npx
    args: ["-y", "@modelcontextprotocol/server-github"]
    version: "0.6.2"
    secret:
      token: github-token

hooks:
  gofmt-after-edit:
    event: PostToolUse
    matcher: "Edit|Write"
    command: "gofmt -w ."
```

```bash
git clone <org/repo> && cd <repo>
ainfra install              # reconcile your machine to the manifest
ainfra install --dry-run    # preview without writing (CI-friendly with --strict)
```

ainfra borrows the package-manager vocabulary on purpose — once you have an `ainfra.yaml`, you mostly work through `install`, `add`, `remove`, `update`, `list`, and `outdated`. Same daily verbs as `npm`, `brew`, or `apt`.

ainfra writes the native config your AI tools already read — `.mcp.json`, the bundles under `.claude/`, `CLAUDE.md`. There is nothing to lock into: stop using ainfra tomorrow and every file it wrote still works.

## Why ainfra

AI coding agents need configuration to be useful — MCP servers, skills, hooks, rules files — but today every developer sets this up by hand, and the moment a teammate installs your setup, theirs starts to drift from yours.

ainfra fixes this with three promises:

- **Defined once.** One `ainfra.yaml` describes every channel your agents need — MCP servers, skills, plugins, rules, tool permissions, CLI tools, hooks, slash commands — across org/team, repo, and personal [layers](docs/reference/design.md#2-locked-architectural-decisions). Secrets are [references, not values](docs/reference/design.md#5-the-environment-primitive--three-credential-modes).
- **Reproduced everywhere.** `ainfra install` reconciles a machine to the manifest, [dependency-aware](docs/reference/design.md#7-the-dependency-graph--the-connective-layer) — installs CLI tools, verifies preconditions (VPN, SSH keys), starts services in the right order. `install --dry-run` previews first.
- **Verified in sync.** `ainfra.lock` pins resolved versions and content hashes; `ainfra install --dry-run --strict` reports drift with a clean CI exit code, and [catches silent upstream changes](docs/reference/validation.md#scenario-3--an-mcp-server-schema-silently-changes) — a package or advertised toolset shifting underneath you fails loudly. The lockfile also records a `toolsetHash` per MCP server — the fingerprint of the live `tools/list` description blob — so `ainfra check` catches the case where an upstream server changed its tool descriptions silently. A `SessionStart` hook installed by default warns once on Claude startup when a teammate's manifest pull is sitting un-installed; opt out per repo with `stalenessWarning: false`.

ainfra is *not* a runtime MCP gateway — it consumes gateways, secrets managers, and package managers as pluggable backends.

## Teach your AI agents how to use ainfra

This repo ships a Claude Code skill at [`skills/using-ainfra/`](skills/using-ainfra/SKILL.md). Reference it from any project's `ainfra.yaml` and every agent that lands in the repo learns the install/add/remove/list workflow, the eight channels, and the hard rules (never edit the lockfile, never commit personal config, secrets are references).

```yaml
skills:
  using-ainfra:
    source: "github:MHilhorst/ainfra/skills/using-ainfra"
    version: "0.1.0"
```

Or scaffold it at `init` time with `ainfra init --with-skill`.

## Commands

| Command | What it does |
|---------|--------------|
| `init` | Scaffold an `ainfra.yaml` (`--personal`, `--force`, `--with-skill`) |
| `adopt` | Draft an `ainfra.yaml` from an existing `.mcp.json` / `.claude/` / `CLAUDE.md` setup (`--merge`, `--force`) |
| `install` | Reconcile the environment to the manifest (`--dry-run`, `--strict`, `--print-schema`, `--from <url>`) |
| `add` | Add an entry to `ainfra.yaml` and reconcile (`ainfra add <channel> <id> [source]`) |
| `remove` | Remove an entry from `ainfra.yaml` and reconcile |
| `update` | Re-resolve the lockfile and reinstall (bare or `<channel> <id>`) |
| `list` | List installed entries (`--channel`, `--json`) |
| `outdated` | Show entries with newer resolvable versions (`--strict` for CI) |
| `version` | Print the ainfra version |

Global flags: `--chdir <dir>` runs as if started elsewhere; `--no-color` disables colored output; `AINFRA_QUIET=1` suppresses the `ainfraVersion:` mismatch warning.

<details>
<summary>Hidden verbs</summary>

Still callable but omitted from `ainfra --help`: `lock` (install auto-locks when the manifest is newer), `publish` / `installer` (subscriber-mode helpers).

</details>

## Status

Reconciles a Claude Code or Codex environment today. `init`, `adopt`, `install` (with all its modes), `add`, `remove`, `update`, `list`, `outdated`, `version`, plus the hidden subscriber-mode helpers (`publish`, `installer`) all work end to end across five completed build phases (see [design §10](docs/reference/design.md#10-build-phases)). Schemas were validated on paper against [five scenarios](docs/reference/validation.md) before any code was written.

Remote sources — `github:`, `npm:`, and `https:` — resolve at lock time and write through a content-addressed cache under `$XDG_CACHE_HOME/ainfra/sources/`, making subsequent fetches offline-capable. Gateway adapters are the remaining follow-up.

### Subscriber mode — non-engineers

Non-engineers (sales, support) need MCP servers in their Claude Desktop app, with no repo and no terminal. `ainfra publish` packages the resolved lockfile into a hash-pinned artifact; `ainfra install --from <url>` reconciles a machine against it; `ainfra installer` emits a one-time macOS installer that drops a launchd job to install on a schedule. A failed fetch is a safe no-op.

## Build

```sh
go build ./...
go test ./...
```

<details>
<summary>Repository layout</summary>

```
ainfra.yaml      Showcase manifest — a small team setup, the read-in-30s example
cmd/ainfra/      CLI entry point, command definitions, reconcile wiring
internal/        Engine — manifest, lockfile, providers, resolve, graph, schema, ui
spec/            Manifest and lockfile schema specifications
examples/        Worked manifests — multi-database is the hardest case
docs/            Quick start at the top, reference docs under docs/reference/
```

</details>
