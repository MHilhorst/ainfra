---
title: "feat: Tighten ainfra CLI/UX — trim verbs, fix drift, fix template args"
status: shipped
created: 2026-05-27
type: feat
depth: standard
---

# feat: Tighten ainfra CLI/UX

## Problem Frame

ainfra's README sells three headline features — **defined once**, **reproduced everywhere**, **verified in sync** — but a five-minute live demo against the current binary surfaces two bugs that hit exactly those headlines, plus a CLI surface that has grown to 12 verbs while the marketing only needs 4–6.

External pressure makes the gap more pointed:

- **sx (`sleuth-io/sx`) is paying for what ainfra hasn't tested yet.** Its open issues cluster on silent overwrite (`#123`), brittle install rules (`#138`), and unhandled transport errors (`#124`). ainfra has the same product shape — same trap is waiting if `apply` is left to grow without consent prompts and loud errors.
- **APM (`microsoft.github.io/apm`) is the same category — same one-manifest, one-lockfile model — and is winning on three things ainfra hasn't shipped:** a single-verb headline (`apm install`), *byte-for-byte* reproduction (content hashes that actually fail `check`), and secure-by-default install (hidden-Unicode scan, transitive-MCP trust prompts).

Two concrete bugs were verified live against `ainfra v0.0.0-dev` in `/tmp/ainfra-demo/`:

1. **Template `args` are not interpolated.** Three MCP servers instantiated from one `templates.fs-scoped` instance with `params.root` set to `.`, `./docs`, and `./commands` each emitted the literal string `${params.root}` in their `.mcp.json` args. `env:` and `headers:` interpolate correctly; `args:` does not.
2. **`ainfra check` is delete-only.** Editing the pinned version inside `.mcp.json` (`@0.6.2` → `@0.5.0`) and appending garbage to a materialized slash command both returned `No drift. exit=0`. Only file *absence* trips drift today.

Either bug, on its own, undermines the README's cover-page claim. Together they make the live demo land weaker than the prose. The CLI trim is the cheap half; the bug fixes are the load-bearing half.

## Scope

**In scope**

- Trim the public CLI surface to a Terraform-shaped core (`init`, `validate`, `lock`, `plan`, `apply`, `check`, `version`).
- Demote subscriber-mode and secrets verbs (`publish`, `installer`, `exec`, `sync`, `schema`) to flags or subcommands of the core verbs. Keep backwards-compatible top-level aliases for one release with a deprecation note.
- Fix template `args` interpolation in `internal/resolve/template.go` so `${params.*}`, `${resolved.*}`, `${secret.*}`, and `${instance.*}` substitute in `args[]` exactly as they do in `env` and `headers`.
- Fix MCP-server content-hash drift in `internal/provider/claudecode/mcp.go` so `Observe` computes a stable hash over the per-server config and `ainfra check` reports any mismatch (not just absence). Apply the same fix to hooks, commands, and rules where `Observe` returns an empty `ContentHash`.
- Refresh `README.md`, `docs/quickstart.md`, and `ainfra.yaml` so the showcase manifest exercises template `args` interpolation and the `Commands` table reflects the new top-level surface.

**Deferred to Follow-Up Work**

- APM-style security defaults (hidden-Unicode scan on materialized assets, explicit consent on `.mcp.json`/`settings.json` overwrite, trust prompt before pulling transitive MCP servers). Conceptually orthogonal to the CLI trim and large enough to warrant its own plan.
- **Tighten-only layer inheritance** (APM `enterprise → org → repo` model). Changing layer-merge semantics is a breaking change for any team already using `ainfra.personal.yaml`; deferring until security-defaults plan ships.
- Remote source fetching for skills/commands (git, npm). The `skills:` block in `ainfra.yaml` already advertises this as PREVIEW.
- Gateway adapters and runtime-MCP integration.

**Outside this product's identity**

- Becoming an MCP runtime / gateway. Already an explicit non-goal in `README.md`.
- Replacing secret managers. ainfra holds *references*, never values.

## Key Technical Decisions

1. **Backwards-compatible alias layer, not a hard cut.** Top-level `publish`, `installer`, `exec`, `sync`, and `schema` keep working in 0.x, but `--help` lists them under "Deprecated — see `<new home>`". Users of the committed `ainfra.yaml` in the repo and the multi-database example notice nothing. Documented removal target: 0.2.0.
   - `publish` → `ainfra apply --publish --out <dir>` (or stays as a subcommand of a new `ainfra dist` group if we want to keep the verb readable).
   - `installer` → `ainfra apply --publish --installer`.
   - `exec` and `sync` collapse into one verb. `sync` wins on declarativeness ("`apply` already syncs secrets; `sync` is the no-config-write subset"). `exec` becomes `ainfra run -- <cmd>` if we keep it; otherwise drop.
   - `schema` → `ainfra validate --print-schema`. Editors point at a generated `ainfra.schema.json` checked into the repo.
   - Final mapping is settled in U1.

2. **Hash the *desired* shape, not the rendered file.** For MCP servers, hooks, commands, and rules, compute `ContentHash` by canonicalizing the lockfile entry's payload (sorted keys, stable JSON) and SHA-256ing it. Compare against the same canonicalization of the observed file. This survives whitespace and key-order churn in `.mcp.json` (Claude Code rewrites it) while still catching version, command, or arg changes.

3. **Template interpolation is uniform across `Produces.MCPServer`.** Run `Args`, `Env`, `Headers`, and (for completeness) `Command` and `URL` through `Interpolate` inside `Instantiate`. The current asymmetry — `Env`/`Headers` interpolate but `Args` doesn't — is a bug, not a design choice. The fix is a 3-line change plus tests.

4. **Drift severity is "any mismatch fails check".** No allowlist in this pass. If a future tool genuinely rewrites a file ainfra owns, ainfra owns the rewrite — that file should not be hand-edited.

5. **No engine rewrite.** All four changes are local to `internal/cli/`, `internal/resolve/template.go`, and `internal/provider/claudecode/{mcp,hooks,commands,rules}.go`. The dependency graph, lockfile schema, and manifest schema do not change.

## Implementation Units

### U1. Inventory and pick the trimmed CLI surface

**Goal:** Lock in the final mapping from today's 12 verbs to a 7-verb top-level surface, with deprecation aliases for the rest.

**Files:**
- `internal/cli/command.go`
- `internal/cli/help.go`
- `cmd/ainfra/commands.go`
- `cmd/ainfra/main.go`
- `docs/plans/2026-05-27-001-feat-cli-ux-tightening-plan.md` (this doc — append the locked mapping)

**Approach:** Read `internal/cli/command.go` to confirm the registration shape, then pick one of two routings for each non-core verb: (a) collapse into a flag on a core verb, or (b) keep as a hidden alias with a stderr deprecation warning. The four "really niche" verbs (`publish`, `installer`, `exec`, `sync`, `schema`) get (a); `init` stays top-level. Output of this unit is a frozen table in this plan plus a stub command-router rewrite in U2.

**Verification:** Plan section "Locked CLI mapping" exists and has unanimous reviewer sign-off (just the user, in this case). `ainfra --help` is not modified yet.

### U2. Implement the trimmed CLI surface with deprecation warnings

**Depends on:** U1
**Goal:** `ainfra --help` lists only the 7 core verbs. Old verbs continue to work but print `warning: 'ainfra publish' is deprecated; use 'ainfra apply --publish'. Will be removed in 0.2.` on stderr before running.

**Files:**
- `internal/cli/command.go`
- `internal/cli/help.go`
- `cmd/ainfra/commands.go`
- `cmd/ainfra/cmd_publish.go`
- `cmd/ainfra/cmd_installer.go`
- `cmd/ainfra/cmd_sync.go`
- `cmd/ainfra/cmd_exec.go`
- `cmd/ainfra/cmd_schema.go`
- `cmd/ainfra/cmd_apply.go` (or wherever apply's flag set is — discover during U2)
- `cmd/ainfra/cmd_validate.go`
- `cmd/ainfra/dispatch_test.go` / `internal/cli/dispatch_test.go`

**Approach:** Add a `Hidden bool` and `DeprecatedHint string` to the command struct. Top-level help skips `Hidden`. Wire the new flags (`--publish`, `--installer`, `--print-schema`) into the appropriate core commands. Behavior of the old commands is preserved — they call the same underlying handler — so existing tests keep passing.

**Test scenarios:**
- `ainfra --help` lists exactly `init validate lock plan apply check version`.
- `ainfra publish --out /tmp/x` still produces the same artifact as before AND prints the deprecation warning to stderr.
- `ainfra apply --publish --out /tmp/x` produces the same artifact as `ainfra publish --out /tmp/x` (byte-equal `MANIFEST.sha256`).
- `ainfra validate --print-schema` prints JSON Schema identical to `ainfra schema`.
- A snapshot test on the help output prevents accidental regressions.

**Verification:** `go test ./...` passes; manual `ainfra --help` output matches the locked mapping from U1.

### U3. Fix template `args` interpolation

**Depends on:** none (parallelizable with U1/U2)
**Goal:** Templates that reference `${params.*}`/`${resolved.*}`/`${secret.*}`/`${instance.*}` in `args:` substitute them, matching the behavior already present for `env:` and `headers:`.

**Files:**
- `internal/resolve/template.go`
- `internal/resolve/template_test.go`
- `examples/multi-database/ainfra.yaml` (verify it still resolves — it doesn't currently use args interpolation but it shouldn't regress)

**Approach:** In `Instantiate` at `internal/resolve/template.go:64-66`, replace the `append([]string(nil), src.Args...)` copy with a loop that runs each arg through `Interpolate(arg, scope)`. Also interpolate `srv.Command` and (for `transport: http`) `srv.URL`, since those have the same "string carrying a `${}` reference" shape. Keep behavior identical when the source string contains no `${`.

**Execution note:** Start with a failing unit test in `template_test.go` that instantiates a one-param template with `args: ["--root", "${params.root}"]` and asserts the resolved args end with the param's value. Then make it pass.

**Test scenarios:**
- Single template with `args: ["${params.root}"]` and `params: {root: "./docs"}` resolves to `args: ["./docs"]`.
- Multi-instance: three instances of the same template with different `params.root` resolve to three different arg lists (regression for the bug observed in `/tmp/ainfra-demo/`).
- `args` with no `${}` references is byte-identical to today's output.
- `args` referencing `${resolved.tunnelPort}` resolves to the allocated port (covers the multi-database example).
- `args` referencing `${secret.foo}` resolves to the placeholder string in scope, matching `env:` behavior.
- Invalid reference (e.g. `${params.missing}`) returns a non-nil error from `Instantiate`.

**Verification:** `go test ./internal/resolve/...` passes. Re-running the `/tmp/ainfra-demo/` showcase produces a `.mcp.json` where every server has its own concrete path in `args`.

### U4. Make content-hash drift detection actually trip

**Depends on:** none (parallelizable with U1–U3)
**Goal:** `ainfra check` reports drift when `.mcp.json`, the hooks block in `.claude/settings.json`, or a materialized slash command/rule diverges from the lockfile — not only when those files are missing.

**Files:**
- `internal/provider/claudecode/mcp.go`
- `internal/provider/claudecode/hooks.go`
- `internal/provider/claudecode/commands.go`
- `internal/provider/claudecode/rules.go`
- `internal/provider/claudecode/mcp_test.go`
- `internal/provider/claudecode/hooks_test.go`
- `internal/provider/claudecode/commands_test.go`
- `internal/provider/claudecode/rules_test.go`
- `internal/provider/diff.go` (read-only; the comparison at line 42 should not need to change)
- `internal/provider/diff_test.go` (extend coverage if there are gaps)

**Approach:** Each provider's `Observe` currently leaves `ContentHash` empty for these channels. Add a `canonicalHash(payload)` helper (or use an existing one — discover during implementation) that JSON-encodes the payload with sorted keys and SHA-256s the result. Populate `ContentHash` on both sides — the desired `Resource` (built from the lockfile entry) and the observed `Resource` (built from disk) — using the *same* canonicalization. The diff at `internal/provider/diff.go:42` already compares the two; once both are non-empty, mismatches surface as `~` (change) entries in plan/check output.

**Patterns to follow:** Skills already populate `ContentHash` correctly — mirror that path. Look at `internal/provider/claudecode/skills.go` for the existing canonical-hash idiom and reuse it rather than inventing a second one.

**Test scenarios:**
- After `apply`, editing the `version` substring inside `.mcp.json` causes `check` to report `~ mcpServers.<id>` and exit non-zero.
- After `apply`, deleting a key from a server in `.mcp.json` causes `check` to report drift (today: works as long as the whole file is gone; should also work for partial edits).
- After `apply`, appending content to a materialized `.claude/commands/<cmd>.md` causes `check` to report `~ commands.<cmd>` and exit non-zero.
- Reformatting `.mcp.json` (re-pretty-printing without changing semantics) does NOT trip drift — canonical form is whitespace-insensitive.
- Key-order changes in `.mcp.json` (Claude Code rewriting it) do NOT trip drift.
- A clean post-apply tree still returns `No drift.` and exit 0.

**Verification:** The drift scenarios in this plan's "Problem Frame" — the two bugs verified live — both now exit non-zero with a clear `~ <channel>.<id>` line in `ainfra check` output.

### U5. Refresh demos and docs

**Depends on:** U2, U3, U4
**Goal:** The `ainfra.yaml` at the repo root, `docs/quickstart.md`, and the README's "Commands" table reflect the new surface and the now-working template `args` interpolation.

**Files:**
- `README.md`
- `ainfra.yaml`
- `docs/quickstart.md`
- `docs/sx-comparison.md` (touch lightly — note that the silent-overwrite trap is being addressed in the follow-up security-defaults plan)

**Approach:** Rewrite the showcase `ainfra.yaml` so it includes a small template instantiated twice with different `params.root` (proves U3 works). Update the README "Commands" table to seven rows plus a "Deprecated aliases" footnote. Update `docs/quickstart.md` to walk through `validate → lock → plan → apply → check` only — drop sidecar commands.

**Test scenarios:** none (pure docs).

**Verification:** Run the demo end-to-end against the repo's own `ainfra.yaml`: `validate`, `lock`, `plan`, `apply`, `check`, then tamper with `.mcp.json` and confirm `check` flags it.

## System-Wide Impact

- **Backwards compatibility:** Any user invoking `ainfra publish`/`installer`/`exec`/`sync`/`schema` continues to work but sees a deprecation warning. No silent breakage.
- **Lockfile schema:** Unchanged. Existing `ainfra.lock` files keep working.
- **Manifest schema:** Unchanged.
- **CI integrations:** Teams using `ainfra check` in CI will see exit code 1 in cases that previously passed silently. This is the intended behavior — the failure was already there, just invisible. Communicate in the release notes.
- **Subscriber-mode artifacts:** `publish`/`installer` output is byte-equal to today's (covered by U2 tests).

## Risks

| Risk | Severity | Mitigation |
|---|---|---|
| `apply` users in CI rely on `check` exiting 0 with hidden drift, and the fix breaks their pipeline | Medium | Release note + `ainfra check --soft` flag deferred; if a user shouts, add it. Today's behavior is the bug. |
| Template `args` interpolation breaks a manifest that was *relying on* the literal `${...}` string in args | Very low | Nobody would do this — `${...}` is ainfra's interpolation syntax and the literal is useless to the consuming MCP server. No realistic carrying capacity for the regression. |
| Deprecation warnings annoy heavy `exec`/`sync` users | Low | One-line warning, suppressible via `--no-color` (already global) or an env var if needed. Aliases keep working through 0.x. |
| Canonical hashing picks a JSON encoder that differs from skills' existing canonicalization | Low | U4 explicitly reuses the skills helper rather than introducing a second one. |

## Verification Strategy

End-to-end demo path (also the smoke test in CI):

1. Reset `/tmp/ainfra-demo/`.
2. Write a manifest with one template instantiated twice (different `params.root`), one hook, one slash command.
3. `ainfra validate && ainfra lock && ainfra plan && ainfra apply --yes`.
4. Inspect `.mcp.json` — both servers carry their own concrete `args`.
5. `ainfra check` → `No drift.` exit 0.
6. Bump a version inside `.mcp.json` → `ainfra check` reports `~ mcpServers.<id>` exit 1.
7. Append text to `.claude/commands/<cmd>.md` → `ainfra check` reports `~ commands.<cmd>` exit 1.
8. `ainfra --help` lists exactly seven verbs.

Anything that passes (1)–(8) without manual judgment is shippable.

## Deferred to Implementation

- Final wording of the deprecation warning line.
- Whether `exec` keeps a top-level form (`ainfra run --`) or is dropped — depends on real usage; if there is no internal user of `ainfra exec --`, drop it and document the replacement.
- Whether `init` survives the trim or gets demoted to `ainfra validate --init` (slightly weirder but possible). Default: keep `init` top-level — scaffolding has a different cognitive shape from validation.
- Exact name of the canonical-hash helper if it needs to be extracted into a shared package.
