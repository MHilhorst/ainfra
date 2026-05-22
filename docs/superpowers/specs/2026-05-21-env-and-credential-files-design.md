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
  does not consume. It also has no `url` field: `transport: http` is named in
  the schema but the endpoint cannot be declared at all.
- A `cliTool` has no `env` map and no way to bind a secret — credentials for a
  CLI cannot be declared at all.
- A `cliTool` has no `requires` field, so it cannot even declare a dependency
  on a credential file being present.

This iteration adds exactly what is needed to close those, and nothing more.

---

## 1. HTTP MCP `url` and `headers`

`MCPServer` gains two fields:

- `url: string` — the HTTP endpoint. Required for `transport: http`; an HTTP
  server cannot be declared without it. This is a pre-existing schema gap that
  must be closed here, because `headers` is meaningless without an endpoint.
- `headers: map[string]string` — request headers. Header values are
  interpolated by the same mechanism as `mcpServers.env` — `${secret.<name>}`,
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

The two transports use disjoint field sets:

- `transport: http` requires `url`; `headers` is optional. `command` / `args` /
  `version` on an HTTP server are a hard error.
- `transport: stdio` requires `command`; `url` and `headers` on a stdio server
  are a hard error.

Each violation fails loudly with a hint, in the same spirit as the loader's
`KnownFields(true)` strict decoding (design §13). The rule lives in
`internal/manifest/validate.go` and is covered by a loader test.

This is not enforced by the type system — both transports share one `MCPServer`
struct — so these are explicit validation checks, not structural guarantees.

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

> **Refinement (2026-05-22) — `path:` secrets.** This section's "never
> written" position predates the `ainfra sync` command. `sync` is now the
> single, explicit, machine-local step where references become concrete: it
> already resolves secrets and writes their values to a file —
> `~/.claude/settings.local.json`. Writing a credential file is the identical
> act with a different destination. A `Secret` therefore gains an optional
> `path:` field — the file-destination counterpart of `env:` — and `sync`
> materializes it (parent dir `0700`, file `0600`).
>
> The inviolable rule still holds: the **committed manifest and lockfile never
> contain a value** — only a `ref` and a destination. And ainfra stays
> **content-blind**: the whole file lives in the resolver (e.g. one 1Password
> item) as an opaque blob; ainfra moves that blob from `ref` to `path` and
> never composes, templates, or parses credential content. The `requires`
> check below is unchanged — a `path:` secret is still *declared* and
> *checked*, and now also *materialized*. §3.1–§3.2 describe the check; the
> general principle is: **a credential a tool cannot receive at runtime is
> still a secret — model it in `secrets:` with an explicit materialization
> target, keep the content opaque in the resolver, and let `sync` place it.**

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

The `requires` edge-wiring helper (`resolve.addRequireEdges`) already handles
`cliTool` and `precondition` edges generically. But the lock pipeline does not
yet iterate `cliTools` — non-templated-entry resolution is explicitly deferred
in the codebase (`pipeline.go`: *"fully-inlined mcpServers are handled by the
follow-up plan"*). So `requires` on a `cliTool` parses and validates now, and
becomes graph-active when that follow-up plan lands CLI tool resolution. This
iteration adds and documents the field; it does not build CLI tool lock-time
resolution.

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

The new fields are ordinary manifest content with no lockfile schema change. A
templated MCP server's per-entry `contentHash` gains `url` and `headers` so a
change to either is caught as drift. CLI tool fields ride the merged-manifest
hash; CLI tools get no dedicated lock entry until the non-templated follow-up
plan (§7).

### 4.3 JSON Schema

`spec/` JSON Schema is generated by reflection from the loader's Go structs
(`ainfra schema`, design §13). New struct fields appear automatically; there is
no hand-maintained schema file to edit.

---

## 5. Schema changes — summary

| Struct | Field | Type | Notes |
|--------|-------|------|-------|
| `MCPServer` | `URL` | `string` | required for `transport: http` |
| `MCPServer` | `Headers` | `map[string]string` | `transport: http` only |
| `CLITool` | `Env` | `map[string]string` | delivered via `settings.json` |
| `CLITool` | `Secret` | `map[string]any` | inline secret bindings |
| `CLITool` | `Requires` | `[]Require` | dependency edges |

No new struct types. No lockfile change.

---

## 6. Files touched

- `internal/manifest/types.go` — the five fields above.
- `internal/manifest/validate.go` (+ `validate_test.go`) — the transport
  field-set coupling rules (§1.1).
- `internal/resolve/template.go` — interpolate a template-produced server's
  `headers`, mirroring how `env` is already interpolated.
- `internal/resolve/pipeline.go` — fold `url` and `headers` into the templated
  MCP server `contentHash` so drift detection covers them.
- `spec/manifest-schema.md` — §5 (`url` + `headers`), §6 (`file-exists`
  `mode`), §7 (CLI `env` / `secret` / `requires`).
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
- **CLI tool and non-templated MCP server lock-time resolution** — env/headers
  interpolation, graph edges, and lock entries for non-templated entries. Owned
  by the existing follow-up plan for non-templated entries; this iteration is a
  schema iteration.
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
