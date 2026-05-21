# Iteration 5 — env vars and credential files for MCPs and CLIs

Status: **design approved, ready for implementation plan.**

This iteration closes two open gaps from
[assessment-vs-real-config.md](../../assessment-vs-real-config.md) §5 — HTTP MCP
`headers` (#2) and secret-to-file (#5) — and gives CLI tools first-class
environment-variable support. It follows the Iteration 3 precedent (the `hooks`
and `commands` channels): small, localised schema extensions, exercised
end-to-end by the multi-database example, with no lockfile schema change.

---

## 0. Motivation

The real `tvt-config` repo configures HTTP MCP servers that need auth headers,
~30 CLI tools that need credentials, and CLIs that read credential *files*
(`aws`, `gcloud`). The current schema cannot express any of the three:

- An HTTP MCP server has no `headers` map — only `env`, which an HTTP transport
  does not consume.
- A `cliTool` has no `env` map and no way to bind a secret — credentials for a
  CLI cannot be declared at all.
- A `cliTool` has no `requires` field, so it cannot even declare a dependency
  on a credential file being present.

This iteration adds exactly what is needed to close those, and nothing more.

---

## 1. HTTP MCP `headers`

`MCPServer` gains `headers: map[string]string`. Header values are interpolated
by the same mechanism as `mcpServers.env` — `${secret.<name>}`,
`${secrets.<id>}`, and `${resolved.<name>}` outside templates; the full four
namespaces inside a template body (spec §4.4).

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

### 1.1 Transport coupling — a validation rule

`headers` is meaningful only for `transport: http`. Declaring `headers` on a
`stdio` server is a hard validation error, with a hint, in the same spirit as
the loader's `KnownFields(true)` strict decoding (design §13). Symmetrically,
`command` / `args` / `version` remain `stdio`-only. The rule lives in
`internal/manifest/validate.go` and is covered by a loader test.

This is not enforced by the type system — both transports share one `MCPServer`
struct — so it is an explicit validation check, not a structural guarantee.

---

## 2. CLI tool environment variables

`CLITool` gains two fields, mirroring `MCPServer` exactly:

- `env: map[string]string` — environment variables for the tool.
- `secret: map[string]any` — inline secret bindings, referenced from `env` as
  `${secret.<name>}`. Top-level named secrets remain reachable as
  `${secrets.<id>}`.

```yaml
cliTools:
  aws-cli:
    versionConstraint: ">=2.0"
    install:
      brew: { formula: awscli }
    env:
      AWS_REGION: "eu-west-1"
      AWS_PROFILE: "tvt"
    secret:
      ssoToken: { mode: direct, ref: "op://Engineering/aws/sso" }
```

### 2.1 Delivery mechanism

A CLI tool is an installed binary; ainfra does not run it, so there is no
process for ainfra to inject an environment into directly. The delivery vehicle
is a Claude Code **`settings.json` env block** that ainfra writes. Claude Code
applies that block to every Bash tool call in a session — which is exactly where
the credential-needing CLIs run (skills and commands invoking `aws`, `gcloud`,
the ads CLIs).

This is consistent with the existing model: MCP `env` lands in `.mcp.json`; CLI
`env` lands in `settings.json`. Both are Claude Code config artifacts ainfra
already owns. It adds no new dependency (unlike direnv) and no shell-rc
ownership problem (unlike a sourced env file).

**Boundary.** A developer running a CLI in a bare terminal *outside* Claude Code
does not receive these vars. That is an accepted limit: ainfra is a Claude Code
setup tool, and `op run` / direnv already solve the bare-terminal case for any
developer who wants it. This boundary is documented, not worked around.

---

## 3. Secret-to-file — verify-only, never written

Some CLIs read a credential *file* (`~/.aws/credentials`, a service-account
JSON, an SSH private key) rather than an environment variable. ainfra
**deliberately does not write these files.**

Writing a secret value to disk would contradict the environment primitive's
core promise — *"the tool never stores, encrypts, or syncs secret values"* (§5
non-goal) and *"no credential brokering / token holding"* (§9). It would also be
redundant: `aws sso login`, `gcloud auth`, `op inject`, and `sops -d` already
materialise credential files, and ainfra's stated position is that it consumes
secret managers as backends and owns none of their runtimes.

So the thing that belongs in config is the **requirement**, not the secret.
ainfra expresses and *checks* "this credential file must exist," and never
writes it. Two small additions make this expressible:

### 3.1 `CLITool` gains `requires`

`CLITool` gains `requires: []Require`. CLI tools have no `requires` field today
(they are a substrate, not a common-field channel — spec §2). This is a
deliberate, scoped extension so a CLI tool can depend on a precondition.

### 3.2 The `file-exists` precondition check gains `mode`

The `file-exists` precondition check (spec §6) gains an optional `mode:` field.
When set, the check also verifies the file's permission bits and flags an
over-permissive credential file (e.g. a world-readable private key) — a cheap,
real security win. Because precondition `check` is a `map[string]any`, this is a
documented key plus check-engine behaviour (Phase 3), not a struct change.

```yaml
preconditions:
  aws-credentials:
    description: AWS SSO credentials must be present.
    check:
      type: file-exists
      path: ~/.aws/sso/cache
      mode: "0600"
    remediation: "Run: aws sso login --profile tvt"

cliTools:
  aws-cli:
    requires:
      - precondition: aws-credentials
```

The dependency graph (§9) already walks precondition edges; the only change is
that a `cliTool` node may now own one. `internal/graph` is expected to handle
this generically — the implementation plan verifies that and adds a test.

---

## 4. Cross-cutting concerns

### 4.1 Secrets never land in committed config

Per the §13 "secrets in committed config" failure mode: any `env` or `headers`
value that resolves *from a secret reference* may be written only to gitignored
Claude Code config (`settings.local.json`, a local `.mcp.json`). Literal,
non-secret values may go to committed config.

The precise per-value routing is an apply-time concern (Phase 3, not yet built).
MCP `env` already carries this exact unresolved question; `headers` and CLI
`env` inherit the same pending Phase 3 resolution and introduce no new problem.

### 4.2 Lockfile

The new fields are ordinary manifest content. They fold into the existing
`manifestHash` and per-entry `contentHash` with no lockfile schema change.

### 4.3 JSON Schema

`spec/` JSON Schema is generated by reflection from the loader's Go structs
(`ainfra schema`, design §13). New struct fields appear automatically; there is
no hand-maintained schema file to edit.

---

## 5. Schema changes — summary

| Struct | Field | Type | Notes |
|--------|-------|------|-------|
| `MCPServer` | `Headers` | `map[string]string` | `transport: http` only |
| `CLITool` | `Env` | `map[string]string` | delivered via `settings.json` |
| `CLITool` | `Secret` | `map[string]any` | inline secret bindings |
| `CLITool` | `Requires` | `[]Require` | dependency edges |

No new struct types. No lockfile change.

---

## 6. Files touched

- `internal/manifest/types.go` — the four fields above.
- `internal/manifest/validate.go` (+ `validate_test.go`) — the `headers`↔`http`
  coupling rule.
- `internal/resolve/` — route `headers` and `cliTools.env` through the same
  interpolation path as `mcpServers.env` (`template.go` / `pipeline.go`).
- `internal/graph/` — confirm a `cliTool`→`precondition` edge resolves; add a
  test.
- `spec/manifest-schema.md` — §5 (`headers`), §6 (`file-exists` `mode`), §7
  (CLI `env` / `secret` / `requires`).
- `docs/assessment-vs-real-config.md` — add an "Iteration 5" section; move gaps
  #2 and #5 to Clean. Gap #5 is reframed: Clean as verify-only — ainfra checks
  credential files and deliberately does not write them.
- `examples/multi-database/ainfra.yaml` — add an HTTP MCP server with `headers`
  and a CLI tool with `env`, exercised end-to-end (matching how Iteration 3
  added a hook and a command to the example).

---

## 7. Out of scope

Deliberately excluded:

- **ainfra writing secret files.** See §3 — this is the secret manager's job.
- **Per-key committed-vs-local routing internals.** Phase 3 apply mechanics.
- **Bare-terminal CLI env** outside Claude Code sessions. See §2.1.
- **The remaining open gaps** #1 (scheduled jobs), #3 (plugin git+subpath),
  #4 (`pip`/`composer` adapters), #6 (per-developer `rules` templating).

---

## 8. Validation

This iteration is assessment-driven, like Iterations 3 and 4 — it is recorded in
`assessment-vs-real-config.md`, not as a new `validation.md` paper scenario. It
is exercised concretely by the additions to `examples/multi-database/ainfra.yaml`
(§6) and by loader/validator/graph tests.
