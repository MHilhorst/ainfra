# Phase 1 — Manifest Schema (`ainfra.yaml`)

Status: **drafted, under validation.** This spec is proven by
[docs/validation.md](../docs/validation.md). If a validation scenario cannot be
expressed cleanly, this spec is iterated — not worked around in code.

The manifest is YAML. `version: 1` is the only stable promise; everything else
may change until the validation gate passes.

---

## 1. Layers and files

Three layers merge into one resolved state:

| Layer | File | Committed? | Authority |
|-------|------|-----------|-----------|
| Team | sourced via `extends:` | yes (in its own repo/package) | highest |
| Repo | `ainfra.yaml` (repo root) | yes | middle |
| Personal | `ainfra.personal.yaml` (repo root) | **no** — gitignored | lowest |

The repo manifest names the team layer:

```yaml
version: 1
extends:
  - source: git+https://github.com/acme/ainfra-team.git@v3.1.0
```

`source` schemes: a local path (`./team/ainfra.team.yaml`), `git+https://…@<ref>`,
or `npm:<pkg>@<version>`. A team layer may itself `extends:` an org layer; the
chain is resolved depth-first, org-most first.

### Precedence (Option C)

Entries are keyed by `channel + id`. When the same key appears in multiple
layers:

- The **higher-authority layer wins** (team > repo > personal).
- A winning entry may set `overridable: true` to *sanction* a lower layer
  overriding it. Default is `overridable: false`.
- Personal entries cannot be overridden (nothing sits below them).
- An id present in only one layer is used as-is — any layer may *add* entries.

This is Anthropic's enterprise > personal > project ordering as the default,
plus a deliberate opt-in departure. The precedence engine (Phase 4) is the
mechanical expression of this table.

---

## 2. Top-level structure

```yaml
version: 1
extends:            []      # team/org layer sources
preconditions:      {}      # things the tool can only verify (§6)
cliTools:           {}      # installable substrate binaries (§7)
backgroundServices: {}      # persistent processes (§8) — usually template-emitted
secrets:            {}      # named, reusable secret references (§5)
templates:          {}      # reusable channel-entry shapes (§4)
mcpServers:         {}      # channel 1
skills:             {}      # channel 2
plugins:            {}      # channel 3
rules:              {}      # channel 4 — CLAUDE.md / context files
tools:              {}      # channel 5 — built-in toggles + permissions
hooks:              {}      # channel 7 — lifecycle automation (§11)
commands:           {}      # channel 8 — slash commands (§12)
```

(Channel 6, CLI tooling, has no manifest key of its own — it is the `cliTools`
substrate of §7.)

Every channel entry (`mcpServers`, `skills`, `plugins`, `rules`, `hooks`,
`commands`) accepts these common fields:

| Field | Type | Default | Meaning |
|-------|------|---------|---------|
| `enabled` | bool | `true` | A lower layer may set `false` to switch an entry off (subject to `overridable`). |
| `overridable` | bool | `false` | Sanctions a lower-authority layer overriding this entry. |
| `requires` | list | `[]` | Dependency edges (§9). |

---

## 3. The environment primitive — secrets

A secret is a *reference*, never a value the tool stores. Three modes:

```yaml
# mode: direct + literal — the always-works baseline
mode: direct
value: "literal-string"

# mode: direct + reference — resolved at apply/session time
mode: direct
ref: "op://Engineering/analytics-db/password"

# mode: brokered — a gateway holds the credential; no per-dev secret exists
mode: brokered
gateway: corp-mcp-gateway
```

`ref` scheme selects the resolver adapter: `op://` (1Password), `doppler://`,
`vault://`, `sops://<file>#<key>`, `env://<VARNAME>` (read an already-set env
var). `mode` defaults to `direct`.

`scope:` declares intent and controls interpolation:

- `scope: shared` (default) — one vault item the whole team reads.
- `scope: personal` — each dev's own item at the same logical path; `ref` may
  contain `${user}`, resolved per-developer.

Named, reusable secrets live at the top level and are referenced by id:

```yaml
secrets:
  bastion-ssh:
    mode: direct
    ref: "op://Engineering/bastion/ssh-key"
```

An instance references one with `secret:` (see §4) or declares an inline secret.

---

## 4. Templates, instances, and resolved fields

The three things the manifest keeps strictly separate (design §8).

### 4.1 Template — the shared shape

```yaml
templates:
  <template-id>:
    description: <string>
    params:                       # typed inputs an instance must/may supply
      <name>:
        type: string|bool|int
        required: true|false
        default: <value>
    secrets:                      # secret names the body consumes
      <name>: { required: true|false }
    resolved:                     # tool-owned fields the body consumes (§4.3)
      <name>:
        kind: allocated-port|generated-script-path|rendered-hook
    produces:                     # what instantiating this template emits
      mcpServer:        { ... }   # a channel entry
      backgroundService:{ ... }   # an auxiliary node
```

`produces` may emit a channel entry of exactly one channel type plus any number
of auxiliary `backgroundService` nodes. Auxiliary nodes are namespaced by the
instance id so two instances never collide.

### 4.2 Instance — the per-use differences

An instance lives under a channel and names a template:

```yaml
mcpServers:
  analytics-db:
    template: mysql-over-ssh-tunnel
    params:    { host: analytics-db.tvt.internal, database: analytics, sshUser: deploy }
    secret:    { dbPassword: bastion-db-analytics }   # maps body secret -> top-level secret id
```

`secret:` maps each secret name the template declares to a top-level secret id,
or to an inline secret object. The instance supplies *only* what differs:
params and secret bindings. Nothing else.

### 4.3 Resolved fields — tool-owned

Fields declared by no human and computed by the tool. This boundary is what
makes port collision structurally impossible: no human types a port.

| `kind` | Tool behaviour |
|--------|----------------|
| `allocated-port` | Allocates a free local port, **stable across runs** — the chosen value is recorded in `ainfra.lock` and reused. |
| `generated-script-path` | Computes the path of a script the tool writes (e.g. a tunnel launcher). |
| `rendered-hook` | Computes the Claude Code hook block the tool injects. |

### 4.4 Interpolation

Inside a template body, `${...}` expressions resolve against four namespaces:

- `${params.<name>}` — instance-supplied param
- `${instance.id}` — the instance's own id
- `${secret.<name>}` — a secret the instance bound (resolved at apply/session time)
- `${resolved.<name>}` — a tool-owned field (§4.3)

Outside templates (a direct, non-templated entry) only `${secrets.<id>}`,
`${secret.<name>}`, and `${resolved.<name>}` are available.

---

## 5. Channel 1 — MCP servers

A non-templated MCP server:

```yaml
mcpServers:
  github:
    transport: stdio              # stdio | http
    command: npx
    args: ["-y", "@modelcontextprotocol/server-github"]
    env:
      GITHUB_TOKEN: "${secret.token}"
    secret:
      token: { mode: direct, ref: "op://Engineering/github/pat" }
    capabilities:                 # generic toggles, not hardcoded server knowledge
      allow: ["ALLOW_WRITE"]
    requires:
      - precondition: internet
```

A templated MCP server is an instance (§4.2). When a gateway is configured, an
MCP entry may set `via: <gateway-id>` to route through it; absent that, the
entry lands directly in the dev's `.mcp.json` (direct mode is the baseline).

### 5.1 Pinned versions are mandatory for package-launched servers

> Added by Iteration 1 of the [validation gate](../docs/validation.md#scenario-3--an-mcp-server-schema-silently-changes).

If an MCP server's `command`/`args` launch it from a package registry (`npx`,
`uvx`, `pipx`, …), the entry **must** pin an exact `version:`. A floating range
or `@latest` is a validation error — it would let the server's code change with
no change to the launch config, defeating drift detection.

```yaml
mcpServers:
  github:
    command: npx
    args: ["-y", "@modelcontextprotocol/server-github"]
    version: "2025.4.0"           # required — exact, no range, no @latest
```

The lockfile records an `integrity` hash of the resolved package content (and,
when the server is reachable at `lock` time, a `toolsetHash` of its advertised
tool list) so that a tampered package or a changed toolset fails `check` loudly.
See [lockfile-schema.md §3](lockfile-schema.md#3-entry-shape).

---

## 6. Preconditions — verify-only

Something the tool can only verify, never provision.

```yaml
preconditions:
  vpn-tvt-internal:
    description: Team VPN must be connected to reach *.tvt.internal hosts.
    check:
      type: dns-resolves          # dns-resolves | tcp-reachable | file-exists | command-succeeds
      host: bastion.tvt.internal
    remediation: "Connect the team VPN, then re-run ainfra check."
```

`check` fails loudly with `remediation` text. The tool never tries to satisfy a
precondition.

---

## 7. CLI tooling — installable substrate

```yaml
cliTools:
  mysql-client:
    versionConstraint: ">=8.0"
    install:                      # platform adapters; first match wins
      brew: { formula: mysql-client }
      apt:  { package: mysql-client }
    check:
      command: "mysql --version"
      versionRegex: 'Ver (\d+\.\d+\.\d+)'
```

If no `install` adapter matches the host OS, the tool falls back to
declare-and-check: it verifies presence and version and, on failure, prints an
actionable error naming the tool and constraint. CLI reproducibility is
best-effort (design §6): the lock records the resolved version but cannot
guarantee a byte-identical binary.

---

## 8. Background services — declare, check, generate

A persistent process a channel needs running for its lifetime.

```yaml
backgroundServices:
  <id>:
    kind: ssh-tunnel              # ssh-tunnel | command
    spec:                         # kind-specific
      localPort:  "${resolved.localPort}"
      remoteHost: "${params.host}"
      remotePort: 3306
      sshUser:    "${params.sshUser}"
      sshHost:    "${params.sshHost}"
      identity:   "${secret.sshKey}"
    requires:
      - cliTool: ssh
      - precondition: vpn-tvt-internal
    lifecycle:
      generateHook: SessionStart  # tool wires a Claude Code hook to start it
    check:
      type: port-listening
      port: "${resolved.localPort}"
```

The tool **generates** the start/stop scripts and the hook wiring, and **checks**
the service is up. It does **not** supervise, restart, or own the daemon — that
is the OS / Claude Code hooks (design §7, §12.1).

---

## 9. The dependency graph — `requires`

Any channel entry, template body, or background service may declare edges:

```yaml
requires:
  - service: <backgroundService-id>
  - cliTool: <cliTool-id>
  - precondition: <precondition-id>
```

The tool builds one graph across all layers, topologically orders it, and
`apply` walks it leaves-first: install CLI tools, verify preconditions, start
background services, then write channel config. A cycle is a hard error.

---

## 10. Channels 2–5

```yaml
skills:
  disruption-debugging:
    source: "git+https://github.com/acme/skills.git@v1.4.0#disruption-debugging"
    version: 1.4.0                # pinned; lock adds the content hash

plugins:
  tvt-config:
    source: "npm:@acme/tvt-config-plugin@2.0.1"
    version: 2.0.1

rules:
  team-claude-md:
    target: CLAUDE.md             # where the file lands
    source: ./rules/team-claude.md
    version: 1                    # lock adds the content hash

tools:
  builtins:
    disabled: [WebFetch]          # built-ins switched off team-wide
  permissions:
    allow: ["Bash(go build:*)", "Bash(go test:*)"]
    deny:  ["Bash(rm -rf:*)"]
```

`skills`, `plugins`, and `rules` pin an exact version; the lockfile adds a
content hash for strong reproducibility and drift detection (Phase 2).

---

## 11. Channel 7 — Hooks

> Added by Iteration 3 of the validation work — assessing the schema against a
> real team config repo showed the original six channels could not express
> standalone hooks as managed config (see `docs/assessment-vs-real-config.md`).

A hook binds automation to a Claude Code lifecycle event. It is a first-class,
layerable, lockable channel — not merely a side-effect of a background service.

```yaml
hooks:
  guard-destructive-sql:
    event: PreToolUse           # required (§11.1)
    matcher: Bash               # optional — tool-name matcher for *ToolUse events
    command: node .ainfra/run/guard.js   # required — what Claude Code runs
    source: ./hooks/guard.js    # optional — a script the tool installs alongside
    timeout: 5000               # optional — milliseconds
    requires:
      - cliTool: node
    enabled: true               # common field
    overridable: false          # common field
```

### 11.1 Events

`event` must be one of the Claude Code lifecycle events: `SessionStart`,
`SessionEnd`, `UserPromptSubmit`, `PreToolUse`, `PostToolUse`, `Notification`,
`Stop`, `SubagentStop`, `PreCompact`. An unknown event is a validation error.

`matcher` is meaningful for `PreToolUse` / `PostToolUse` (it scopes the hook to
matching tool names); it is ignored for other events.

A hook with a `source` script is installed by the tool; `command` references
the installed path. The lockfile records a content hash of the hook's declared
config so a silent change to a hook fails `check`.

This channel is distinct from the `generateHook` lifecycle field a background
service uses (§8) — that field generates *one specific* SessionStart hook to
launch a service. The `hooks` channel manages *arbitrary, standalone* hooks.

---

## 12. Channel 8 — Commands

A command is a Claude Code slash command — a sourced markdown file. It is
modelled like `skills`: a `source` plus optional `version`.

```yaml
commands:
  db-console:
    source: ./commands/db-console.md   # required — local path, git, or npm ref
    description: Open a read-only MySQL console.   # optional
    version: 1                                     # optional — for git/npm sources
    requires:
      - cliTool: mysql-client          # a command may depend on a CLI tool
    enabled: true
    overridable: false
```

`source` accepts the same schemes as `extends` (§1): a local path,
`git+https://…@<ref>`, or `npm:<pkg>@<version>`. The lockfile records a content
hash; for git/npm sources the pinned `version` is recorded too.
