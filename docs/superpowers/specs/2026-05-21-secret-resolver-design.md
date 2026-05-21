# Secret resolver — design

The pluggable secret resolver deferred from Phase 3. design.md §5 names three
credential modes and promises that `direct` + `ref` is "resolved at
apply/session time by shelling out to the team's secrets manager via a
pluggable resolver." Phase 3 shipped everything *except* that resolver: today
the `Secret` struct's `Mode`/`Ref`/`Gateway` fields parse but are consumed
nowhere, and `resolve/template.go` renders every templated secret as the
literal placeholder string `<secret:id.name>`. This increment builds the
resolver.

## 0. Context — what exists, what is missing

- **Schema (complete).** `manifest.Secret` (`Mode`, `Value`, `Ref`, `Gateway`,
  `Scope`), top-level `secrets:`, template `Secrets`, and instance `secret:`
  maps on `MCPServer` and `CLITool` all parse and validate.
- **Reference syntax (complete).** `ref` scheme selects the adapter: `op://`,
  `doppler://`, `vault://`, `sops://<file>#<key>`, `env://<VARNAME>`.
- **Resolution (missing).** No code reads `Ref`. `resolve/template.go` emits
  `<secret:id.name>` placeholders; instance `secret:` maps are nulled after
  instantiation. Nothing resolves a ref, and the Phase 3 "refs are checked"
  promise was described but never built.

## 1. Locked decisions

Decided during brainstorming; not open.

1. **ainfra owns the resolver abstraction.** It does not delegate wholesale to
   `op run`. A manifest may mix schemes; `op run` only knows `op://`. ainfra
   exposes a common adapter interface and dispatches per scheme.
2. **Delivery is a session wrapper.** `apply` renders env-var *placeholders*
   into `.mcp.json`; a new `ainfra exec` command resolves refs in-memory and
   `exec`s the child with the environment populated. No secret value is ever
   written to disk. This honors design §5's non-goal: "the tool never stores,
   encrypts, or syncs secret values — references only."
3. **Adapters this increment: `op://` and `env://` only.** The other three
   schemes register but fail with a clear "not implemented in this increment"
   error.
4. **`brokered` mode stays check-only.** No per-dev value exists to resolve;
   ainfra only verifies the gateway is reachable. Unchanged by this increment.

### Why not the alternatives

- **Managed dotenv file** — `apply` resolving refs into a gitignored `.env`
  puts plaintext secret values at rest. Dispreferred by 12-factor / OWASP
  guidance and a direct violation of the §5 non-goal.
- **Delegate to `op run`** — cleanest for a pure-1Password shop, but `op`,
  `doppler`, `vault`, and `sops` expose no uniform `run` interface; a mixed
  manifest degrades to fragile `op run -- sops exec-env -- …` chains. The
  `teller` project exists precisely to paper over that gap — so ainfra owns
  the abstraction itself.

## 2. The resolver package — `internal/secret/`

### 2.1 `Resolver` interface

```go
// Resolver turns one ref scheme into a credential value.
type Resolver interface {
    // Scheme is the URI scheme this resolver handles, e.g. "op", "env".
    Scheme() string
    // Resolve returns the secret value for ref. The value is held in memory
    // and never logged. ref is the full URI including scheme.
    Resolve(ref string) (string, error)
    // Check verifies ref is resolvable (manager reachable, item exists,
    // session authenticated) without returning or exposing the value.
    Check(ref string) error
}
```

### 2.2 Registry

A `Registry` maps scheme → `Resolver`. `Resolve(ref)` and `Check(ref)` parse
the scheme from `ref` and dispatch. An unknown scheme is an error naming the
scheme and the schemes that are registered.

### 2.3 The `op` adapter

- `Scheme()` → `"op"`.
- `Resolve(ref)` shells out to `op read <ref>` (the 1Password CLI's native
  secret-reference reader) and returns trimmed stdout.
- `Check(ref)` verifies the `op` binary is on `PATH` and the session is
  authenticated, then confirms the ref resolves. It must not print the value.
- Failure modes produce actionable errors: `op` not installed → install hint;
  not signed in → `"run: op signin"`; item/field not found → the ref, no value.
- The adapter takes a command-runner seam (an interface wrapping
  `exec.Command`) so tests substitute a fake `op`.

### 2.4 The `env` adapter

- `Scheme()` → `"env"`.
- `Resolve("env://VARNAME")` returns `os.Getenv("VARNAME")`; an unset or empty
  variable is an error.
- `Check` is the same lookup without returning the value.
- This is the always-works fallback for a dev who injects secrets themselves.

### 2.5 The stub adapter

`doppler://`, `vault://`, `sops://` register a shared stub whose `Resolve` and
`Check` both return `"<scheme>:// is not implemented in this increment"`. This
keeps `validate` honest (a manifest using those schemes is well-formed) while
making the runtime boundary explicit.

## 3. Render side — `apply`

### 3.1 The three modes diverge

| Mode | `apply` behavior |
|------|------------------|
| `direct` + literal `value` | Value substituted into config directly. Unchanged from today. |
| `direct` + `ref` | A deterministic placeholder `${AINFRA_SECRET_<KEY>}` is rendered into the config; `{KEY → ref}` is recorded in the lockfile. |
| `brokered` | No placeholder, no value. Check-only. Unchanged. |

### 3.2 Placeholder generation

`resolve/template.go` currently sets `secret[name] = "<secret:id.name>"`. That
placeholder becomes `${AINFRA_SECRET_<KEY>}`, where `<KEY>` is derived from the
secret's logical identity:

- A named top-level secret → its map key.
- An inline instance secret → `<CHANNEL>_<ENTRY_ID>_<SECRET_NAME>`.

`<KEY>` is uppercased with non-alphanumeric characters collapsed to `_`. The
derivation is deterministic so the content hash is stable across runs.

Claude Code expands `${VAR}` and `${VAR:-default}` in `.mcp.json` across
`command`, `args`, `env`, `url`, and `headers` (confirmed in the official MCP
docs). The placeholder is therefore valid in every field a secret can land in.

### 3.3 `settings.json` constraint

`settings.json` does **not** expand `${VAR}` (Claude Code issue #4276). CLI-tool
secrets (`cliTools.secret`) are therefore **not** written into `settings.json`.
They are delivered purely through the session environment (§4), which CLI tools
inherit from the Claude Code process that `ainfra exec` launches. Non-secret
`cliTools.env` values continue to be written into `settings.json` literally.

### 3.4 Lockfile

The lockfile is the desired-state ledger the reconcile commands consume. It
gains a record of each placeholder: `{ key, ref, scheme, scope }`. This holds
references only — commit-safe — and is what `ainfra exec` reads to know which
refs to resolve. Placeholder keys are deterministic, so existing content-hash
behavior is unaffected.

## 4. Resolve side — `ainfra exec`

A new command in `cmd/ainfra/`, alongside `plan`/`apply`/`check`:

```
ainfra exec [-- <cmd> [args...]]
```

Default command when none is given: `claude`.

Steps:

1. Read the merged lockfile (`ainfra.lock` + `ainfra.personal.lock`); collect
   every secret-placeholder record.
2. For each `scope: personal` ref, expand `${user}` to the OS username before
   resolution.
3. Dispatch each ref to its adapter via the registry; resolve in-memory.
4. If any ref fails to resolve, abort before launching the child — print every
   failure with remediation, exit non-zero.
5. Build the child environment: the current process environment plus one
   `AINFRA_SECRET_<KEY>=<value>` variable per resolved ref.
6. `syscall.Exec` the target command. Process replacement gives clean signal
   delivery and leaves no parent process holding resolved values.

Resolution is fresh on every invocation — no caching, no disk writes. Output
masking is **not** in scope: `exec(2)` replaces the process, so there is no
pipe to scan (this is the deliberate trade-off for clean signals and no
lingering parent). Masking is a possible future increment via a fork+pipe mode.

## 5. `check` integration

`ainfra check` calls `Registry.Check(ref)` for every placeholder record. Each
unresolvable ref is reported with actionable remediation and no value. This
implements the Phase 3 "`direct` + `ref` is checked" promise that was specified
but never built. `brokered` entries continue to be checked for gateway
reachability.

`plan` does not resolve secrets; it reports only that a placeholder will be
written.

## 6. Error handling

- No adapter error, log line, or check output ever embeds a secret value.
- `op` missing → error names the binary and how to install it.
- `op` not authenticated → `"run: op signin"`.
- Unknown scheme → error names the scheme and lists registered schemes.
- `env://` variable unset → error names the variable.
- `ainfra exec` with any unresolvable ref → all failures printed, non-zero
  exit, child never launched.

## 7. Testing

- **Unit — registry:** scheme dispatch, unknown-scheme error.
- **Unit — `env` adapter:** set/unset variable, value never logged.
- **Unit — `op` adapter:** fake `op` via the command-runner seam — success,
  not-installed, not-signed-in, ref-not-found.
- **Unit — placeholder generation:** deterministic `<KEY>` derivation for
  named and inline secrets; collision and sanitization cases.
- **Unit — `exec`:** child-environment construction with a fake resolver;
  abort-before-launch on a failing ref.
- **e2e:** manifest with an `op://` ref → `lock` → `apply` renders
  `${AINFRA_SECRET_*}` into `.mcp.json` → `exec` with a fake resolver → a fake
  child process observes the resolved value in its environment.

## 8. Non-goals — this increment

- `doppler://`, `vault://`, `sops://` adapters (interface + failing stub only).
- `brokered`-mode resolution (stays check-only).
- Output/stdout masking.
- Caching resolved values.
- `settings.json` `${VAR}` expansion (a Claude Code limitation, not ainfra's).
