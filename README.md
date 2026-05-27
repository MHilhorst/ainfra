# ainfra

**Keep your whole dev team's AI tooling in sync.**

Terraform for your team's AI development setup — a declarative manifest, `plan` before `apply`, and a lockfile that catches drift. Supports Claude Code and Codex.

**[Quick start](docs/quickstart.md)** · **[Manifest schema](spec/manifest-schema.md)** · **[Design](docs/reference/design.md)** · **[Worked example](examples/multi-database/)**

---

## Install

```sh
brew install MHilhorst/ainfra/ainfra
```

Or `go install github.com/MHilhorst/ainfra/cmd/ainfra@latest`.

## Try it

Joining a team whose repo already has an `ainfra.yaml`:

```sh
ainfra plan     # preview what would change on your machine
ainfra apply    # reconcile your machine to the manifest
ainfra check    # verify nothing has drifted (safe in CI)
```

Authoring a new setup:

```sh
ainfra init        # scaffold an ainfra.yaml
ainfra validate    # static-check the manifest
ainfra lock        # resolve it and write ainfra.lock
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
ainfra plan && ainfra apply
```

ainfra writes the native config your AI tools already read — `.mcp.json`, the bundles under `.claude/`, `CLAUDE.md`. There is nothing to lock into: stop using ainfra tomorrow and every file it wrote still works.

## Why ainfra

AI coding agents need configuration to be useful — MCP servers, skills, hooks, rules files — but today every developer sets this up by hand, and the moment a teammate installs your setup, theirs starts to drift from yours.

ainfra fixes this with three promises:

- **Defined once.** One `ainfra.yaml` describes every channel your agents need — MCP servers, skills, plugins, rules, tool permissions, CLI tools, hooks, slash commands — across org/team, repo, and personal [layers](docs/reference/design.md#2-locked-architectural-decisions). Secrets are [references, not values](docs/reference/design.md#5-the-environment-primitive--three-credential-modes).
- **Reproduced everywhere.** `ainfra apply` reconciles a machine to the manifest, [dependency-aware](docs/reference/design.md#7-the-dependency-graph--the-connective-layer) — installs CLI tools, verifies preconditions (VPN, SSH keys), starts services in the right order. `plan` previews first.
- **Verified in sync.** `ainfra.lock` pins resolved versions and content hashes; `ainfra check` reports drift with a clean CI exit code, and [catches silent upstream changes](docs/reference/validation.md#scenario-3--an-mcp-server-schema-silently-changes) — a package or advertised toolset shifting underneath you fails loudly.

ainfra is *not* a runtime MCP gateway — it consumes gateways, secrets managers, and package managers as pluggable backends.

## Status

Reconciles a Claude Code or Codex environment today. `init`, `validate`, `schema`, `lock`, `plan`, `apply`, `check`, `publish`, `installer`, and `version` all work end to end across five completed build phases (see [design §10](docs/reference/design.md#10-build-phases)). Schemas were validated on paper against [five scenarios](docs/reference/validation.md) before any code was written.

Local source files and inline or templated MCP servers work today; fetching sources from remote locations (git/npm) and gateway adapters are the remaining follow-ups.

### Subscriber mode — non-engineers

Non-engineers (sales, support) need MCP servers in their Claude Desktop app, with no repo and no terminal. `ainfra publish` packages the resolved lockfile into a hash-pinned artifact; `ainfra apply --from <url>` reconciles a machine against it; `ainfra installer` emits a one-time macOS installer that drops a launchd job to apply on a schedule. A failed fetch is a safe no-op.

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
