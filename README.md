# ainfra

**Keep your whole dev team's AI tooling in sync.**

Think Terraform — a declarative manifest, `plan` before `apply`, a lockfile — but for your team's AI development setup.

Claude Code · Codex

**[Quick start](docs/quickstart.md)** · **[Design](docs/design.md)** · **[Manifest schema](spec/manifest-schema.md)** · **[Worked example](examples/multi-database/)**

---

> **Defined once. Reproduced everywhere. Verified in sync.**
> One manifest describes every developer's AI tooling; one command reproduces it on any machine; a lockfile proves it stayed that way.

## Why ainfra

AI coding agents need configuration to be useful — MCP servers, skills, hooks, rules files — but today every developer sets this up by hand. The moment a teammate installs your setup, theirs starts to drift from yours, and nobody can see the gap.

**ainfra fixes this.** Describe your team's AI tooling once in `ainfra.yaml`, commit it, and every developer who clones the repo reproduces the exact same setup with one command — versions pinned, content hashed, drift caught.

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
    version: "0.6.2"                     # package-launched servers pin a version
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
ainfra plan     # preview what would change on your machine
ainfra apply    # reconcile your machine to the manifest
```

ainfra writes the native config your AI tools already read — `.mcp.json`, the bundles under `.claude/`, `CLAUDE.md`. There is nothing to lock into: stop using ainfra tomorrow and every file it wrote still works.

## The three promises

### 1. Defined once — config-as-code

One `ainfra.yaml` describes every channel your agents need, in a file you commit instead of steps you run by hand. It is layered and templated, so an org defines a shape once and every repo reuses it.

- **[Eight channels, one file](spec/manifest-schema.md)** — MCP servers, skills, plugins, rules, tool permissions, CLI tools, hooks, and slash commands
- **[Layered](docs/design.md#2-locked-architectural-decisions)** — org/team, repo, and personal layers merge under one explicit precedence rule
- **[Templates](docs/design.md#8-modularity--templates)** — define an MCP-server shape once, instantiate it many times; the multi-database example does exactly this
- **[Secrets are references](docs/design.md#5-the-environment-primitive--three-credential-modes)** — the manifest holds a pointer (`op://`, Vault, Doppler, …), never a credential value

### 2. Reproduced everywhere — one command

`ainfra apply` reconciles a machine to the manifest: it installs what is missing, writes each channel's config, and works through the dependency graph in order. `plan` previews every change first — `apply` is the only command that writes.

- **[plan before apply](docs/quickstart.md)** — every change is previewed; nothing is reconciled unseen
- **[Dependency-aware](docs/design.md#7-the-dependency-graph--the-connective-layer)** — installs CLI tools, verifies preconditions (VPN, SSH keys), and starts background services in the right order
- **[No runtime lock-in](docs/design.md#0-what-this-is)** — writes the native files your tools already read; remove ainfra and they keep working
- **[Agent-aware](docs/design.md#14-target-agent--a-chooseable-axis)** — the engine is agent-agnostic; Claude Code and Codex are both supported targets

### 3. Verified in sync — the lockfile

`ainfra.lock` pins every resolved version and records a content hash of each one. That turns "we're in sync" into something you can verify, not just hope for. `ainfra check` recomputes the hashes against the live environment and reports any drift.

- **[Drift detection](spec/lockfile-schema.md)** — `ainfra check` flags anything that changed, with a clean exit code for CI
- **[Catches silent upstream changes](docs/validation.md#scenario-3--an-mcp-server-schema-silently-changes)** — a package or advertised toolset that changes underneath you fails loudly
- **[Reproducible ports](spec/lockfile-schema.md#4-allocated-ports-are-sticky)** — allocated once, recorded, and reused, so every teammate's tunnels land on the same ports
- **[Personal config stays private](spec/lockfile-schema.md#7-the-lockfile-is-layered)** — the lockfile is layered; personal entries never land in a committed file

## What this is — and is not

ainfra is declarative config-as-code for a team's AI tooling — a Terraform-style CLI with a declarative manifest, `plan` before `apply`, and a lockfile that separates what you want from what is actually on the machine.

It is *not* a runtime MCP gateway — that category is crowded and already on the official MCP roadmap. ainfra *consumes* gateways, secrets managers, and package managers as pluggable backends; it runs none of their runtimes itself.

See [docs/design.md](docs/design.md) for the full, decided design.

## Get Started

Install with Go:

```sh
go install github.com/MHilhorst/ainfra/cmd/ainfra@latest
ainfra version
```

<details>
<summary>Build from a checkout</summary>

```sh
git clone https://github.com/MHilhorst/ainfra.git && cd ainfra
go build -o ainfra ./cmd/ainfra   # then move ./ainfra onto your PATH
```

</details>

**Joining a team** whose repo already has an `ainfra.yaml`:

```sh
ainfra plan     # preview what would change on your machine
ainfra apply    # reconcile your machine to the manifest
ainfra check    # verify nothing has drifted (safe to run anytime, incl. CI)
```

**Authoring a setup** from scratch:

```sh
ainfra init        # scaffold an ainfra.yaml
ainfra validate    # static-check the manifest
ainfra lock        # resolve it and write ainfra.lock
```

The [`ainfra.yaml`](ainfra.yaml) at the repository root is a worked example — a small team setup you can read in 30 seconds and try with `ainfra validate`. See [docs/quickstart.md](docs/quickstart.md) for the full walkthrough and [`examples/multi-database/`](examples/multi-database/) for the hardest case.

Run `ainfra --help` for the command list, or `ainfra <command> --help` for per-command detail.

## Commands

| Command | What it does |
|---------|--------------|
| `init` | Scaffold an `ainfra.yaml` in the current repo (`--personal`, `--force`) |
| `validate` | Static-check the manifest without resolving it (`--print-schema` to emit the JSON Schema) |
| `lock` | Resolve the manifest and write `ainfra.lock` |
| `plan` | Preview the diff between desired and observed state |
| `apply` | Reconcile the environment to the manifest — or to a published artifact with `--from` |
| `check` | Verify the environment matches the lockfile (or a `--from` artifact); report drift |
| `version` | Print the ainfra version |

Global flags: `--chdir <dir>` runs as if started elsewhere; `--no-color` disables colored output.

<details>
<summary>Hidden / advanced verbs</summary>

These keep working but are omitted from `ainfra --help` so the front page stays small.

| Command | What it does |
|---------|--------------|
| `schema` | Folded into `validate --print-schema`; the standalone verb is still wired |
| `publish` | Package the resolved lockfile into a subscriber artifact (`--out`) |
| `installer` | Generate a one-time macOS installer for subscriber machines (`--out`) |
| `exec` | Resolve secrets and run a command with them in its environment |
| `sync` | Resolve secrets and write them to the Claude Code settings env block |
| `history` | Show recent apply events (who / what / when) |

</details>

## Status

ainfra reconciles a Claude Code or Codex environment today. `init`, `validate`, `schema`, `lock`, `plan`, `apply`, `check`, and `version` all work end to end. The manifest and lockfile schemas, the resolution engine, the channel provider layer, and the full CLI are built and tested across five completed build phases (see [docs/design.md §10](docs/design.md#10-build-phases)).

The schemas were validated *on paper* against five scenarios — see [docs/validation.md](docs/validation.md) — before any implementation code was written.

Both Claude Code and Codex are supported targets, and the pluggable secret resolver (`op://` and `env://`) is built. Local source files and inline or templated MCP servers work today; fetching sources from remote locations (git/npm) and gateway adapters are the remaining follow-ups.

### Subscriber mode — non-engineers

Engineers manage AI tooling as config-as-code in the repo. Non-engineers (sales, support) need the MCP servers in their **Claude Desktop app**, with no repo and no terminal. ainfra bridges this without owning a runtime:

- `ainfra publish` packages the resolved lockfile into a hash-pinned **artifact** (`ainfra.lock` + rendered resources + an `ainfra.sub.json` descriptor + `MANIFEST.sha256`). The team hosts that artifact at a URL.
- `ainfra apply --from <url>` reconciles a machine against the artifact — rendering `claude_desktop_config.json` — with no repo and no manifest. A failed fetch is a safe no-op: the machine stays on last-known-good config.
- `ainfra installer` emits a one-time macOS installer that drops a launchd job running `apply --from` at login and on a configurable interval.

The `publish:` block in `ainfra.yaml` configures the artifact URL, target agent, and sync cadence — the team owns every knob; the subscriber configures nothing. See [docs/superpowers/specs/2026-05-22-subscriber-mode-design.md](docs/superpowers/specs/2026-05-22-subscriber-mode-design.md).

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
    shared/      agent-agnostic providers (the cliTools layer)
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

</details>
