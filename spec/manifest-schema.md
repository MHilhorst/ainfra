# Phase 1 — Manifest Schema (`ainfra.yaml`)

Status: **implemented.** This spec was checked on paper against
[docs/reference/validation.md](../docs/reference/validation.md). Whenever a test scenario exposed a
gap, the schema itself was changed rather than patched around in code. It is now
enforced by the loader, `ainfra install --dry-run`, and the reflected JSON
Schema (`ainfra install --print-schema`).

The manifest — `ainfra.yaml`, the file describing the team's setup — is written
in YAML. `version: 1` is the stable wire promise: a fixed format other tools can
rely on. Later schema changes are recorded inline below and in the validation
gate.

---

## 1. Layers and files

Three layers (separate sources of config, stacked by authority) merge into one
resolved state:

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

A `source` can take three forms: a local path (`./team/ainfra.team.yaml`), a Git
reference (`git+https://…@<ref>`), or an npm package (`npm:<pkg>@<version>`). A
team layer may itself `extends:` an org layer. The chain is resolved depth-first,
with the org-most layer applied first.

### Precedence (Option C)

Each entry belongs to a channel (one category of AI-tooling config — MCP
servers, hooks, and so on) and has an id. Entries are keyed by `channel + id`.
When the same key appears in more than one layer:

- The **higher-authority layer wins** (team > repo > personal).
- A winning entry may set `overridable: true` to *sanction* — explicitly permit
  — a lower layer overriding it. The default is `overridable: false`.
- Personal entries cannot be overridden (nothing sits below them).
- An id that appears in only one layer is used as-is — any layer may *add*
  entries.

This is Anthropic's enterprise > personal > project ordering as the default,
plus a deliberate opt-in departure from it. The precedence engine (Phase 4) is
the code that carries out this table.

### 1.1 Singleton channels — union merge

The table above resolves id-keyed channels one entry at a time. The `tools`
channel (§10) works differently. It is a *singleton* — a single entry, not a map
of ids — and its fields are lists (`permissions.allow` / `ask` / `deny`,
`builtins.disabled`). A last-writer rule fits it badly: a personal layer that
adds one `allow` pattern would replace the team's entire list.

`tools` therefore **union-merges** across layers (combines the lists rather than
replacing them):

- Each list is the union of that list across the team, repo, and personal
  layers. The merge does not depend on order, and the result is sorted.
- A lower-authority layer can only *extend* a list, never shrink it. A
  developer may add permissions for their own tooling, but cannot delete a
  team `deny` or re-enable a built-in the team disabled.
- When a pattern ends up in more than one permission tier after the union, the
  strictest tier wins: **`deny` beats `ask` beats `allow`**.

This is the Option-C freedom/guardrail balance applied to a list-valued
channel: additive by default, with team guardrails a lower layer cannot lift.

### 1.2 The target agent

`agent` names the AI coding agent ainfra renders for: `claude-code` (the
default) or `codex`. It is a scalar (a single value, not a list or map), so the
`overridable` mechanism — which settles conflicts between id-keyed entries —
does not apply. Instead, the highest-authority layer that declares a non-empty
`agent` wins (team, then repo, then personal). A repo that sets `agent`
standardizes the team on it; a repo that omits it leaves the choice to each
developer's personal layer.

Not every channel exists for every agent — Codex has no skills, plugins,
hooks, built-in toggles, or slash commands. Any channel entry may carry an
`agents:` list to scope it (this is capability-gating — marking which entries
apply to which AI agent):

```yaml
hooks:
  gofmt-after-edit:
    event: PostToolUse
    command: gofmt -w .
    agents: [claude-code]   # this hook applies only when agent is claude-code
```

Once `agent` is resolved, an entry in a channel that agent cannot render is a
hard validation error — unless its `agents:` list leaves out that agent, which
cleanly scopes the entry away. An entry with no `agents:` list (ungated) never
silently disappears.

---

## 2. Top-level structure

```yaml
version: 1
agent:              claude-code  # which AI agent to render for (§1.2)
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
block of §7, the underlying command-line tools the other channels rest on.)

Every channel entry (`mcpServers`, `skills`, `plugins`, `rules`, `hooks`,
`commands`) accepts these common fields:

| Field | Type | Default | Meaning |
|-------|------|---------|---------|
| `enabled` | bool | `true` | A lower layer may set `false` to switch an entry off (subject to `overridable`). |
| `overridable` | bool | `false` | Sanctions a lower-authority layer overriding this entry. |
| `requires` | list | `[]` | Dependency edges (§9). |

---

## 3. The environment primitive — secrets

A secret is a *reference* — a pointer to a credential — never a value the tool
stores. There are three modes:

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

The scheme in `ref` picks which resolver adapter fetches the credential:
`op://` (1Password), `doppler://`, `vault://`, `sops://<file>#<key>`, or
`env://<VARNAME>` (read an already-set environment variable). `mode` defaults to
`direct`.

`scope:` states intent and controls interpolation (substituting values into
`${...}` placeholders):

- `scope: shared` (default) — one vault item the whole team reads.
- `scope: personal` — each developer has their own item at the same logical
  path; `ref` may contain `${user}`, resolved separately per developer.

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

The manifest keeps these three things strictly separate (design §8). A
*template* is a reusable shape; an *instance* is one concrete use of a template;
a *resolved field* is a value the tool computes itself.

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

`produces` may emit a channel entry of exactly one channel type, plus any number
of supporting `backgroundService` nodes. Those supporting nodes are namespaced
by the instance id, so two instances never collide.

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
or to an inline secret object. The instance supplies *only* what differs from
the template: params and secret bindings. Nothing else.

### 4.3 Resolved fields — tool-owned

Fields no human declares — the tool computes them. This boundary is what makes
port collisions structurally impossible: no human ever types a port number.

| `kind` | Tool behaviour |
|--------|----------------|
| `allocated-port` | Allocates a free local port, **stable across runs** — the chosen value is recorded in the lockfile (`ainfra.lock`, the auto-generated file that pins exact versions) and reused. |
| `generated-script-path` | Computes the path of a script the tool writes (e.g. a tunnel launcher). |
| `rendered-hook` | Computes the Claude Code hook block the tool injects. |

### 4.4 Interpolation

Interpolation replaces `${...}` placeholders with real values. Inside a template
body, those expressions resolve against four namespaces:

- `${params.<name>}` — a param the instance supplied
- `${instance.id}` — the instance's own id
- `${secret.<name>}` — a secret the instance bound (resolved at apply/session time)
- `${resolved.<name>}` — a tool-owned field (§4.3)

Outside templates (in a direct, non-templated entry) only `${secrets.<id>}`,
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
MCP entry may set `via: <gateway-id>` to route through it. Without that, the
entry lands directly in the developer's `.mcp.json` — direct mode is the
baseline.

### 5.1 Pinned versions are mandatory for package-launched servers

> Added by Iteration 1 of the [validation gate](../docs/reference/validation.md#scenario-3--an-mcp-server-schema-silently-changes).

If an MCP server's `command`/`args` launch it from a package registry (`npx`,
`uvx`, `pipx`, …), the entry **must** pin an exact `version:`. A floating range
or `@latest` is a validation error: it would let the server's code change while
the launch config stays the same, defeating drift detection (drift is config
quietly falling out of sync with what was declared).

```yaml
mcpServers:
  github:
    command: npx
    args: ["-y", "@modelcontextprotocol/server-github"]
    version: "2025.4.0"           # required — exact, no range, no @latest
```

The lockfile records an `integrity` hash of the resolved package content. When
the server is reachable at `lock` time, it also records a `toolsetHash` of the
server's advertised tool list. Together these make a tampered package or a
changed toolset fail `check` loudly. See
[lockfile-schema.md §3](lockfile-schema.md#3-entry-shape).

### 5.2 HTTP transport — `url` and `headers`

> Added by Iteration 5 — closes assessment gap #2.

A `transport: http` server is reached over HTTP rather than launched as a
subprocess. It declares a `url` (required) and optional request `headers`:

```yaml
mcpServers:
  linear:
    transport: http
    url: https://mcp.linear.app/sse
    headers:
      Authorization: "Bearer ${secret.token}"
    secret:
      token: { mode: direct, ref: "op://Engineering/linear/mcp" }
```

The two transports use separate, non-overlapping field sets, enforced at
validation:

- `transport: http` requires `url`; `command` / `args` / `version` are rejected.
- `transport: stdio` (the default) requires none of those; `url` / `headers`
  are rejected.

Header values interpolate exactly like `env` (§4.4). A header whose value comes
from a secret follows the same rule as a secret-bearing `env` value: it may be
written only to gitignored client config, never a committed file (see the
design doc's failure-modes table).

---

## 6. Preconditions — verify-only

A precondition is something the tool can only check, never set up itself.

```yaml
preconditions:
  vpn-tvt-internal:
    description: Team VPN must be connected to reach *.tvt.internal hosts.
    check:
      type: dns-resolves          # dns-resolves | tcp-reachable | file-exists | command-succeeds
      host: bastion.tvt.internal
    remediation: "Connect the team VPN, then re-run ainfra install."
```

When `check` fails, it fails loudly and prints the `remediation` text. The tool
never tries to satisfy a precondition itself.

The `file-exists` check takes a `path` and an optional `mode` (an octal string
such as `"0600"`). When `mode` is set, the check also verifies the file's
permission bits, flagging a credential file that is readable too widely. This is
how a CLI tool expresses its dependency on a credential file: ainfra checks the
file but deliberately never writes it (the environment primitive stays
reference-only, §3). A `cliTool` points at such a precondition with `requires`
(§7).

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

> Added by Iteration 5: a `cliTool` also accepts `env`, `secret`, and
> `requires`.

```yaml
cliTools:
  aws-cli:
    versionConstraint: ">=2.0"
    install:
      brew: { formula: awscli }
    env:                                # written to the Claude Code settings.json env block
      AWS_REGION: "eu-west-1"
    secret:                             # inline secret bindings, as on an mcpServer
      ssoToken: { mode: direct, ref: "op://Engineering/aws/sso" }
    requires:
      - precondition: aws-credentials   # a credential file ainfra checks, never writes
```

A `cliTool`'s `env` is delivered through a Claude Code `settings.json` env
block, so it reaches every Bash tool call in a session — which is where the
credential-needing CLIs run. `secret` declares inline secret bindings,
referenced from `env` as `${secret.<name>}`. `requires` declares dependency
edges (§9), typically a `file-exists` precondition for a credential file.

> Resolving CLI tools and non-templated MCP servers at lock time (env/headers
> interpolation, graph edges, lock entries) is owned by the follow-up plan for
> non-templated entries. Iteration 5 only adds and validates the fields.

If no `install` adapter matches the host OS, the tool falls back to
declare-and-check: it verifies the tool is present and the right version, and on
failure prints a clear error naming the tool and the constraint. CLI
reproducibility is best-effort (design §6): the lock records the resolved
version but cannot guarantee a byte-identical binary.

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

The tool **generates** the start/stop scripts and the hook wiring, and
**checks** that the service is up. It does **not** supervise, restart, or own
the daemon — that is left to the OS and Claude Code hooks (design §7, §12.1).

---

## 9. The dependency graph — `requires`

Any channel entry, template body, or background service may declare dependency
edges — links saying it needs another node ready first:

```yaml
requires:
  - service: <backgroundService-id>
  - cliTool: <cliTool-id>
  - precondition: <precondition-id>
```

The tool builds one graph across all layers and sorts it topologically (so each
node comes after everything it depends on). `apply` then walks it leaves-first:
install CLI tools, verify preconditions, start background services, then write
channel config. A cycle in the graph is a hard error.

---

## 10. Channels 2–5

```yaml
skills:
  disruption-debugging:
    source: "git+https://github.com/acme/skills.git@v1.4.0#disruption-debugging"
    version: "1.4.0"              # pinned; lock adds the content hash

plugins:
  tvt-config:
    source: "npm:@acme/tvt-config-plugin@2.0.1"
    version: "2.0.1"

rules:
  team-claude-md:
    target: CLAUDE.md             # where the file lands
    source: ./rules/team-claude.md
    version: "1"                  # lock adds the content hash

tools:
  builtins:
    disabled: [WebFetch]          # built-ins switched off team-wide
  permissions:
    allow: ["Bash(go build:*)", "Bash(go test:*)"]
    ask:   ["Bash(git push:*)"]   # three-tier policy: allow / ask / deny
    deny:  ["Bash(rm -rf:*)"]
```

`skills`, `plugins`, and `rules` each pin an exact version; the lockfile adds a
content hash for strong reproducibility and drift detection (Phase 2). Version
values are strings — always quote them, so `version: "1"` is never misread as a
number.

For `plugins` specifically, the `version:` field is an *expected resolved
version*, not the cache key itself. Claude Code's plugin cache is keyed on the
plugin's own `plugin.json` (or its marketplace entry, or the git commit SHA —
see the plugins reference). After install, `ainfra` reads the resolved
version from `~/.claude/plugins/cache/<name>@<marketplace>/<version>/.claude-plugin/plugin.json`
and emits a warning when it diverges from the pin. The pin itself is never
passed to `claude plugin install`. To change the *actual* cache key, bump the
upstream `plugin.json`/marketplace entry. The plugin's content hash in
`ainfra.lock` is a recursive sha256 of the resolved version directory, so
in-place edits to a cached plugin are caught by `ainfra check`.

`tools` is a singleton, not an id-keyed map. Its `permissions` block is the
three-tier Claude Code policy (`allow` / `ask` / `deny`). Across layers it
union-merges under the rule in §1.1: the lists are additive, and `deny` beats
`ask` beats `allow` for any pattern that appears in more than one tier.

---

## 11. Channel 7 — Hooks

> Added by Iteration 3 of the validation work — assessing the schema against a
> real team config repo showed the original six channels could not express
> standalone hooks as managed config (see `docs/reference/assessment-vs-real-config.md`).

A hook attaches automation to a Claude Code lifecycle event. It is a
first-class channel in its own right — one that layers and locks like the
others — not just a side-effect of a background service.

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

`matcher` only matters for `PreToolUse` / `PostToolUse`, where it scopes the
hook to matching tool names. For other events it is ignored.

If a hook has a `source` script, the tool installs it, and `command` then
references the installed path. The lockfile records a content hash of the hook's
*declared config* (event, matcher, command, source path, timeout), so an edit to
that config is caught by `check`. Hashing the bundled `source` script's
*contents* is a fast-follow — the same drift-coverage gap the lockfile spec
notes for `skills` and `plugins`.

This channel is separate from the `generateHook` lifecycle field a background
service uses (§8). That field generates *one specific* SessionStart hook to
launch a service; the `hooks` channel manages *arbitrary, standalone* hooks.

---

## 12. Channel 8 — Commands

A command is a Claude Code slash command, supplied as a markdown file. It is
modelled like `skills`: a `source` plus an optional `version`.

```yaml
commands:
  db-console:
    source: ./commands/db-console.md   # required — local path, git, or npm ref
    description: Open a read-only MySQL console.   # optional
    version: "1"                                   # optional — for git/npm sources
    requires:
      - cliTool: mysql-client          # a command may depend on a CLI tool
    enabled: true
    overridable: false
```

`source` accepts the same forms as `extends` (§1): a local path,
`git+https://…@<ref>`, or `npm:<pkg>@<version>`. The lockfile records a content
hash; for git and npm sources it also records the pinned `version`.

---

## 13. Deferred — scheduled jobs

A *scheduled jobs* channel (cron-style headless `claude -p` runs) was designed
and briefly implemented as Iteration 4, then reverted from `main`. The full
design is kept at
`docs/superpowers/specs/2026-05-21-scheduled-jobs-design.md` for when the
channel is revisited. It is **not** part of the current schema.
