---
title: "feat: v1 ship sharpener — adopt, toolset hashing, remote sources, status"
created: 2026-05-27
status: active
depth: standard
origin: docs/brainstorms/2026-05-27-v1-ship-sharpener-requirements.md
---

# feat: v1 ship sharpener — adopt, toolset hashing, remote sources, status

## Summary

Four targeted additions that close the gap between ainfra's design coverage and the evidence in `docs/problem-space.md`. Land all four before v1 is declared shipped. Each is small; together they make the v1 story credible to a team adopting cold.

- **MCP toolset hashing** turns the lockfile from a reproducibility control into a reproducibility *and* integrity control — `ainfra check` catches "your MCP server's tool descriptions changed last night" before the next prompt runs through.
- **`ainfra status`** closes the day-one "what is actually installed?" UX gap that `/mcp` and `/doctor` only partially answer.
- **`ainfra adopt`** removes the cold-start friction — teams arrive with existing `.mcp.json`, `.claude/`, hooks, and CLAUDE.md, not blank repos.
- **Remote-source resolver** executes the already-designed `github:` / `npm:` / `https://` source model that the README "Status" section currently flags as the remaining gap.

Out of scope this round: Cursor renderer, Sigstore signature verification, persona-drift hardening, path-scoped render targets. See `docs/brainstorms/2026-05-27-v1-ship-sharpener-requirements.md` §6 for rationale.

## Problem Frame

ainfra's design is unusually well-aligned with the npm/Terraform principles in `docs/design-philosophy-references.md` and with the documented team pains in `docs/problem-space.md`. Cross-referencing surfaced a narrow set of specific gaps:

- **Tool-description drift attack class is documented and unmitigated.** `Entry.ToolsetHash` exists in `internal/lockfile/types.go:33` and is documented in `spec/lockfile-schema.md`, but no code path populates it. Same-version MCP servers with poisoned tool descriptions pass `ainfra check` cleanly today.
- **No "what is installed" view.** `ainfra plan` and `check` are diff-shaped; there is no just-show-me read-only command equivalent to `npm ls` or `terraform state list`.
- **Greenfield-only onboarding.** `ainfra init` scaffolds a blank manifest. Teams adopting ainfra must hand-translate existing config — onboarding ranks as the #2 documented team pain.
- **Remote sources unbuilt.** `LocalFetcher.Fetch` at `internal/provider/fetch/fetch.go:30-66` errors on `git+` and `npm:`. `internal/resolve/render.go:438-445` already recognizes the remote-scheme set; the resolvers themselves are unimplemented.

## Scope Boundaries

### In scope

- New MCP stdio JSON-RPC client (foundation for toolset hashing)
- Populating `Entry.ToolsetHash` at `ainfra lock`
- Re-introspecting and comparing toolset hashes at `ainfra check`
- `ainfra status` read-only inventory command with `--json` and `--channel` flags
- GitHub / npm / HTTPS source fetchers in `internal/provider/fetch/`
- Source cache at `~/.cache/ainfra/sources/`
- `ainfra adopt` command + `internal/adopt/` parsing package
- Documentation deltas: README quick-start, `docs/reference/design.md` §13, `spec/lockfile-schema.md` confirmation

### Out of scope (explicit non-goals)

- Cursor or Copilot renderers — separate brainstorm; v1.5 multiplier
- Sigstore / npm provenance verification — v2; reactive
- Skills persona-drift hardening — evidence thin
- Cross-agent orchestration coordination — premature
- MCP registry curation or quality scoring — not ainfra's job
- HTTP/SSE MCP transport — stdio only for v1
- Path-scoped render targets — already deferred in design §15

### Deferred to follow-up work

- HTTP/SSE MCP transport (after stdio lands)
- Lock-time MCP credentials (opt-in token mode beyond skip-with-marker)
- A formal lockfile JSON Schema generator (today only manifest has one — see `internal/schema/schema.go`)

## Key Technical Decisions

| Decision | Choice | Rationale |
|---|---|---|
| Field name | Use existing `ToolsetHash` / `toolsetHash` (not brainstorm's `toolsHash`) | Field already declared in `internal/lockfile/types.go:33` and documented in `spec/lockfile-schema.md`. Reconcile brainstorm vocabulary to code. |
| Missing-hash sentinel | Leave `ToolsetHash` empty + emit warning at lock time; `check` treats empty as "unverified" not "drift" | The struct tag is already `omitempty`; a sentinel string would force a schema change. |
| Lock-time credential model | Skip-with-marker for servers that fail to start. Opt-in read-only token mode deferred. | Credentials at lock time is a separate trust model; not blocking v1. |
| Drift severity at check | Hard fail (non-zero exit) on toolset hash mismatch | This is the integrity-control point; warning would defeat the purpose. |
| Hash scheme | sha256 of canonical JSON of sorted `[{name, description, inputSchema}]` from `tools/list` | Reuse `lockfile.ContentHash` helper at `internal/lockfile/hash.go:15`. Sort to keep hashes stable across reorderings. |
| MCP transport scope | stdio JSON-RPC only for v1 | Matches majority of community MCP servers; HTTP/SSE deferred. |
| Lockfile schema migration | Additive, no version bump. Existing lockfiles read with empty `ToolsetHash` are valid and re-populated on next `lock`. | Field is already `omitempty`; backward-compatible by construction. |
| Cache location | `$XDG_CACHE_HOME/ainfra/sources/` (fall back to `~/.cache/ainfra/sources/`) | Global, content-addressed; matches XDG convention. Reuse across worktrees. |
| Source tag resolution | Manifest accepts tag or commit; lockfile pins commit SHA (git) / resolved tarball URL + integrity (npm) / effective URL + content hash (https) | Mirrors npm's range-vs-resolved-version separation. |
| Adopt secret handling | Strip any literal value matching `env:`/`token:`/`apiKey:`/`password:` patterns; replace with `direct` reference placeholder; emit one warning line per stripped secret | Never write literals to a file destined for commit. Matches problem-space §4. |
| Adopt overwrite protection | Refuse overwrite without `--force` (or `--merge` for additive re-runs) | Mirror `cmd_init.go:68-76` pattern. |
| Naming: adopt vs import | `adopt` | "Import" collides with `terraform import` semantics (single-resource). `adopt` better captures whole-repo intake. |

## High-Level Technical Design

*Directional guidance for review, not implementation specification.*

```
ainfra lock
  └─ resolve.RunLock
       ├─ existing pipeline (manifests, templates, ContentHash)
       ├─ NEW: for each MCPServer entry:
       │       mcpclient.Introspect(server.command, server.args, env, timeout)
       │         └─ stdio subprocess → JSON-RPC initialize + tools/list
       │         → returns canonical-JSON([]{name, description, inputSchema})
       │       entry.ToolsetHash = lockfile.ContentHash(toolsCanonical)
       │       on failure: leave empty + warn
       └─ NEW: for each remote-source entry (skills, plugins, rules, marketplaces):
              fetch.Resolve(source) → Bundle{path: bytes}
                ├─ github:org/repo/path@ref  → GitHubFetcher  (tarball API)
                ├─ npm:@scope/pkg@ver        → NPMFetcher     (registry → tarball)
                └─ https://... | http://...  → HTTPFetcher    (existing http.FetchURL)
              cache by content hash → ~/.cache/ainfra/sources/<sha>/
              entry.ContentHash, entry.Integrity = ...

ainfra check
  └─ runCheck (existing)
       ├─ orchestrator.PlanAll
       ├─ checkSecrets, checkPreconditions  (existing)
       └─ NEW: checkToolsetDrift
              for each MCPServer entry with non-empty locked ToolsetHash:
                live = mcpclient.Introspect(...)
                if hash(live) != entry.ToolsetHash: drift event

ainfra status
  └─ manifest.LoadLayers + lockfile.Read + mergeLocks + ResolveAgent
       → flat inventory grouped by channel
       → human / --json
       → --channel <name> filter

ainfra adopt
  └─ adopt.Scan(dir)
       ├─ .mcp.json                 → MCPServer entries (secrets stripped)
       ├─ .claude/settings.json     → permissions, hooks (when ledger-mappable)
       ├─ .claude/hooks/*           → Hook entries
       ├─ .claude/commands/*        → Command entries
       └─ CLAUDE.md                 → Rules entry
  → manifest.Manifest value → yaml.Marshal → ainfra.yaml (refuse overwrite without --force)
```

## Implementation Units

### U1. MCP stdio client

**Goal:** Provide a sandboxed stdio JSON-RPC client capable of executing `initialize` + `tools/list` against an MCP server and returning a canonical tool list. This is the foundation for U2 and U3.

**Requirements:** Unblocks brainstorm §2 success criteria (lockfile records per-tool hashes, check detects description drift).

**Dependencies:** None.

**Files:**
- `internal/mcpclient/client.go` (new — `Introspect(ctx, cmd, args, env, timeout) (ToolList, error)`)
- `internal/mcpclient/jsonrpc.go` (new — request/response framing)
- `internal/mcpclient/sandbox.go` (new — restricted-env subprocess via `provider.CommandRunner`)
- `internal/mcpclient/client_test.go` (new — table-driven, stdlib only)
- `internal/mcpclient/testdata/` (new — captured stdio fixtures from known servers)

**Approach:**
- Reuse `provider.CommandRunner` / `provider.ExecRunner` for subprocess injection (testability mirrors lock-pipeline pattern).
- Restricted env: pass only env vars the manifest declares for the server. No inherited shell env beyond `PATH`, `HOME`.
- Wall-clock timeout default 15s, configurable. Hard-kill subprocess on timeout.
- Frame as line-delimited JSON-RPC 2.0 per the MCP spec; speak only `initialize` and `tools/list` (no notifications, no resource reads).
- `ToolList` is `[]Tool{Name, Description, InputSchema json.RawMessage}` — input schema kept raw to preserve canonical form for hashing.
- Canonicalize: sort tools by name, sort keys in each `InputSchema` via stable JSON re-encoding before returning. Hashing is the caller's job (U2).

**Patterns to follow:**
- Subprocess injection via `provider.CommandRunner` (see references in `internal/resolve/pipeline.go`).
- Test layout: stdlib-only, table-driven, `_test.go` sibling, fixture-based — mirror `internal/lockfile/hash_test.go` and `internal/provider/fetch/fetch.go` test style.
- Error wrapping: `fmt.Errorf("mcpclient: %s: %w", op, err)`.

**Test scenarios:**
- *Happy path:* fake transport replays a captured `tools/list` response with two tools; `Introspect` returns a sorted, canonicalized list.
- *Single-tool server:* one-tool response round-trips intact.
- *Empty toolset:* server returns `{tools: []}`; result is a non-nil empty slice (not nil).
- *Sort stability:* same response with tools in reversed order produces an identical canonical output.
- *Input-schema canonicalization:* keys reordered inside `inputSchema` produce identical canonical bytes.
- *Timeout:* fake transport delays past timeout; `Introspect` returns a clearly-tagged timeout error and the subprocess is killed.
- *Protocol error:* server returns a JSON-RPC error to `initialize`; `Introspect` surfaces it without panicking.
- *Subprocess exits before `tools/list`:* error is wrapped with the subprocess's stderr tail.
- *Malformed JSON:* parser error is wrapped, not panicked.
- *Restricted env:* runner receives only the env keys declared in the request, no host shell env beyond `PATH`/`HOME`.

**Verification:** `go test ./internal/mcpclient/...` passes; package surface is `Introspect`, `ToolList`, `Tool`; no callers in main code yet.

---

### U2. Populate `ToolsetHash` at lock time

**Goal:** Wire U1 into the resolve pipeline so every MCP server entry in `ainfra.lock` carries a `toolsetHash` (or empty when introspection is impossible).

**Requirements:** Brainstorm §2 (toolset hashing in lockfile).

**Dependencies:** U1.

**Files:**
- `internal/resolve/pipeline.go` (modify — MCP server loops at lines ~122-160 (templated) and ~207-235 (inline))
- `internal/resolve/pipeline_test.go` (extend)
- `cmd/ainfra/cmd_lock.go` (modify — surface introspection skip warnings under the "MCP servers" lock summary; mirror the CLI-tool warning pattern at `cmd_lock.go:44-48`)

**Approach:**
- For each rendered MCP server entry, call `mcpclient.Introspect` using the runner threaded through `RunLock(dir, runner)`. Pass the entry's command, args, and only the env keys declared in `secret:` references resolved against the secret pipeline (or the literal env from the manifest if `direct`+literal).
- If introspection returns an error (timeout, missing creds, subprocess failure), leave `ToolsetHash` empty and append a one-line warning to the lock summary: `mcp server <name>: toolset unverified (<reason>)`.
- If introspection succeeds: hash `lockfile.ContentHash(canonicalToolList)` and set `entry.ToolsetHash`.
- Hash input includes name + description + InputSchema raw bytes after canonicalization (decided in §Key Technical Decisions).

**Patterns to follow:**
- Sorting before hashing (`slices.Sorted(maps.Keys(...))` is the project's idiom — see `pipeline.go:179,193,207,237`).
- `lockfile.ContentHash` at `internal/lockfile/hash.go:15`.
- Per-server warning style mirrors `cmd_lock.go:44-48`.

**Test scenarios:**
- *Happy path:* manifest with one MCP server; after `RunLock`, the lockfile entry has a non-empty `toolsetHash`.
- *Two servers, distinct toolsets:* hashes differ.
- *Same server declared twice across layers:* entry merges and hash is stable.
- *Introspection failure (subprocess exits non-zero):* `toolsetHash` is empty, lock summary contains an `unverified` warning, lock command exits 0 (warning, not error).
- *Introspection timeout:* `toolsetHash` empty, warning includes "timeout".
- *Templated server (multi-instance):* each instance gets its own hash based on its rendered command/args/env.
- *Re-running lock against unchanged manifest:* `toolsetHash` is identical (stability).
- *MCP entry that uses HTTP/SSE transport (not stdio):* skipped with `unverified` + reason "non-stdio transport not supported in v1".
- Covers AE for "lockfile records per-tool hash" (origin §2).

**Verification:** `go test ./internal/resolve/...` passes; `ainfra lock` against `ainfra.yaml` produces a lockfile whose MCP entries have populated `toolsetHash` fields.

---

### U3. Toolset drift check at `ainfra check`

**Goal:** Make `ainfra check` re-introspect each locked MCP server and exit non-zero on toolset drift, with a per-tool diff in the output.

**Requirements:** Brainstorm §2 success criteria — `check` exits non-zero when an MCP server's `tools/list` hashes differently than locked; per-tool granularity so the user knows *which* tool changed.

**Dependencies:** U1, U2.

**Files:**
- `cmd/ainfra/commands.go` (modify `runCheck` at lines ~438-504 — add `checkToolsetDrift` call alongside `checkSecrets` and `checkPreconditions`)
- `internal/check/toolset.go` (new — drift detection helper)
- `internal/check/toolset_test.go` (new)
- `internal/ui/diag.go` (modify — add a `Diagnostic` shape for per-tool diffs if the existing one is too generic)

**Approach:**
- For each MCP server entry with non-empty locked `toolsetHash`: re-introspect via U1, compute the live hash, compare.
- On mismatch: render a per-tool diff (added / removed / description-changed / input-schema-changed) using sorted symmetric diff against the canonical lock fixture if reachable, or a "live differs from locked hash X" message if the original tool list is not persisted.
- Empty `toolsetHash` (unverified at lock time) is *not* drift — it is informational; emit a one-line note that the entry was never verified.
- Exit code 1 when any non-empty hash mismatches; exit code 0 when all match or all are unverified.

**Patterns to follow:**
- Drift integration into `runCheck` mirrors `checkSecrets` / `checkPreconditions` sequencing in `cmd/ainfra/commands.go:438-504`.
- Diagnostic rendering via `internal/diag` + `internal/ui/diag.go`.
- Exit code convention: 1 on drift (matches existing `commands.go:503`).

**Test scenarios:**
- *Match:* `check` against a fresh lockfile exits 0 with no drift messages.
- *Description-only change:* one tool's description differs between lock and live; check exits 1 with a `description changed` diagnostic naming the tool.
- *Tool added upstream:* live has one tool the lock did not; check exits 1, diagnostic lists the added tool name.
- *Tool removed upstream:* check exits 1, diagnostic lists removed tool name.
- *Multiple drifted tools:* all are reported in one run.
- *Unverified entry:* lock has empty `toolsetHash`; check exits 0 with an informational note.
- *Subprocess fails at check time:* exit code 1 with a clear "could not re-introspect server X" message, distinct from drift.
- *Mix of clean and drifted servers:* exit 1; clean servers do not appear in output, drifted ones do.
- Covers AE for "check detects description drift" (origin §2).

**Verification:** `go test ./internal/check/...` and `go test ./cmd/ainfra/...` pass; manual run of `ainfra check` against a poisoned fixture exits 1 with a readable per-tool diff.

---

### U4. `ainfra status` — read-only inventory

**Goal:** A read-only command that prints the resolved manifest+lockfile state grouped by channel, with versions, hashes, source layer, and target agent. Supports `--json` and `--channel <name>` filters.

**Requirements:** Brainstorm §4 (explicit inventory).

**Dependencies:** None. Can land in parallel with U1-U3.

**Files:**
- `cmd/ainfra/cmd_status.go` (new — registered in `cmd/ainfra/main.go:18-34` between `newCheckCommand` and `newExecCommand`)
- `cmd/ainfra/cmd_status_test.go` (new)
- `internal/ui/status.go` (new — `RenderStatus(w, status, colorizer)`)
- `internal/ui/status_test.go` (new)

**Approach:**
- Load via `manifest.LoadLayers(dir)`, `lockfile.Read(path)`, `mergeLocks(...)` from `cmd/ainfra/commands.go:47-73`, `manifest.ResolveAgent(layers)` at `internal/manifest/agent.go:11`. **No network, no subprocess, no MCP introspection.**
- Build a `StatusReport` struct: per-channel entries with `Name`, `Source` (which layer), `Version`, `ContentHash`, `ToolsetHash` (if MCP), `Agents` (if gated).
- Default output: human-readable, grouped by channel, with a header line per channel and one row per entry. Uses `internal/ui/status.go` renderer.
- `--json` emits the same struct via `json.MarshalIndent`.
- `--channel mcpServers` filters to one channel.
- `--all` includes entries currently filtered out by selectors / agent gating (default: hide).
- `warnIfStale` at `cmd/ainfra/commands.go:82-95` runs; the warning goes to stderr in human mode and into a `warnings: []` array in JSON mode.

**Patterns to follow:**
- Read-only command shape: `cmd_plan.go` load/merge/warnIfStale path with the orchestrator call removed.
- `--json` flag idiom: `newVersionCommand` at `cmd/ainfra/commands.go:25-42`.
- JSON output: `json.MarshalIndent` per `cmd_schema.go`.
- Stdout = inventory; stderr = warnings (matches project convention).

**Test scenarios:**
- *Happy path:* manifest+lock with one entry per channel; `status` prints all channels with one row each.
- *--json:* same input produces JSON that round-trips to the `StatusReport` struct.
- *--channel mcpServers:* output contains only MCP servers.
- *--channel unknown:* error message lists valid channel names, exit 1.
- *Selectors filter an entry:* default `status` hides it; `--all` shows it with a `(filtered)` marker.
- *Agent gating filters an entry:* same behavior as selectors.
- *Stale manifest (manifest hash mismatch):* stderr warning in human mode; `warnings` array populated in JSON mode; exit 0 regardless.
- *Missing lockfile:* clear error "no `ainfra.lock` — run `ainfra lock` first", exit 1.
- *Empty manifest:* prints "no channels declared", exit 0.

**Verification:** `go test ./cmd/ainfra/... ./internal/ui/...` passes; running `ainfra status` and `ainfra status --json` against the showcase `ainfra.yaml` produces sane output.

---

### U5. Remote-source resolver — GitHub, npm, HTTPS fetchers

**Goal:** Implement the three remote source schemes already designed in `design.md` §1 and recognized in `internal/resolve/render.go:438-445`. Content-hash everything and cache at `$XDG_CACHE_HOME/ainfra/sources/`.

**Requirements:** Brainstorm §3 success criteria (`github:`, `npm:`, `https:` work end-to-end; lockfile pins resolved address + content hash; offline-capable cache).

**Dependencies:** None. Can land in parallel with U1-U4.

**Files:**
- `internal/provider/fetch/fetch.go` (modify — extend `LocalFetcher.Fetch` or introduce a `MultiSchemeFetcher` that dispatches on scheme)
- `internal/provider/fetch/github.go` (new — `GitHubFetcher`)
- `internal/provider/fetch/npm.go` (new — `NPMFetcher`)
- `internal/provider/fetch/https.go` (new — wraps existing `http.FetchURL` at `internal/provider/fetch/http.go:16-27`)
- `internal/provider/fetch/cache.go` (new — content-addressed cache at `~/.cache/ainfra/sources/<sha>/`)
- `internal/provider/fetch/*_test.go` (new for each)
- `internal/provider/fetch/testdata/` (new — captured tarball fixtures)
- `internal/resolve/render.go` (modify — `isRemoteSource` if scheme normalization needed; reconcile `github:` form vs `git+` form already in code)
- `internal/resolve/pipeline.go` (modify — call the resolver from the plugin / skill / rules / marketplaces lock paths at ~lines 251-301; today only `skills.Apply` consumes `env.Fetch`)
- `internal/lockfile/types.go` (verify `Entry.Integrity` field is wired through; no struct change expected)

**Approach:**
- `GitHubFetcher`: parse `github:org/repo[/sub/path]@ref` → resolve `ref` to a commit SHA via `GET /repos/{org}/{repo}/commits/{ref}` → download tarball via `GET /repos/{org}/{repo}/tarball/{sha}` → extract → if `sub/path` was specified, slice the bundle to that subtree. Auth: `GITHUB_TOKEN` env if set.
- `NPMFetcher`: parse `npm:[@scope/]pkg@ver` → resolve via `GET https://registry.npmjs.org/{pkg}/{ver}` → download `dist.tarball` → verify against `dist.integrity` → extract `package/` subtree. Auth: `NPM_TOKEN` env if set, scoped via `.npmrc` parsing deferred.
- `HTTPSFetcher`: thin wrapper over existing `http.FetchURL`; treats response body as the bundle if it's a single file, or extracts if Content-Type indicates archive.
- Cache: key by content hash of (scheme + resolved address); store extracted bundle at `~/.cache/ainfra/sources/<sha>/`. Cache hits are silent and offline-capable.
- Lockfile pins: commit SHA for `github:`, tarball URL + npm integrity for `npm:`, effective URL + content hash for `https:`. All three feed into `Entry.ContentHash` and `Entry.Integrity`.
- Path-traversal guard from `LocalFetcher.Fetch` at `internal/provider/fetch/fetch.go:38` is repeated in every extractor.

**Patterns to follow:**
- `fetch.Bundle = map[string][]byte` is the established multi-file shape (used by `claudecode.Skills.Apply` at `internal/provider/claudecode/skills.go:72-77`). All new fetchers return it.
- `FakeFetcher{Bundles, Err}` in `internal/provider/fetch/fetch.go:71-86` is the test-injection pattern; mirror for HTTP/GitHub/npm clients.
- Error wrapping: `fmt.Errorf("fetch: %s: %w", scheme, err)` per `fetch.go:32`.
- 30s timeout, 256 MiB cap from `http.go:16-27`.

**Test scenarios:**
- *GitHub: tag → SHA:* fixture server returns a fake tag-resolution + tarball; fetcher resolves tag, downloads, extracts, returns Bundle.
- *GitHub: sub-path:* `github:acme/skills/incident-response@2.3.0` returns only the `incident-response/` subtree.
- *GitHub: 404:* returns wrapped error mentioning org/repo.
- *npm: tarball integrity match:* fixture registry returns metadata + tarball matching declared integrity; fetcher succeeds.
- *npm: integrity mismatch:* fetcher errors with "integrity mismatch" naming the package.
- *https: single file:* `https://example.com/file.md` returns Bundle with one entry keyed by basename.
- *https: archive:* tar.gz response is detected and extracted.
- *Path traversal in extracted archive:* fetcher rejects with "path escapes fetch root".
- *Size cap exceeded:* fetcher errors at 256 MiB without buffering further.
- *Cache hit:* second call with identical resolved address returns Bundle without network calls (verified by injecting a fake that errors when called).
- *Cache miss after delete:* removing `~/.cache/ainfra/sources/<sha>/` causes a re-fetch.
- *Offline (network unreachable) with cache hit:* succeeds silently.
- *Offline with cache miss:* errors with a clear "source not cached and network unreachable" message.
- *Auth: `GITHUB_TOKEN` honored:* request carries `Authorization: bearer` header (assert via fake transport).
- *Lockfile pinning:* after lock, `ainfra.lock` shows resolved commit SHA for `github:`, tarball URL + integrity for `npm:`.

**Verification:** `go test ./internal/provider/fetch/...` and `go test ./internal/resolve/...` pass; running `ainfra lock` against a manifest with one entry of each scheme produces a lockfile with all three resolved addresses + content hashes.

---

### U6. `ainfra adopt` — brownfield import

**Goal:** New command that scans a repo's existing `.mcp.json`, `.claude/`, and `CLAUDE.md`, emits a draft `ainfra.yaml`, and refuses to overwrite without `--force` (or `--merge` for additive re-runs).

**Requirements:** Brainstorm §1 success criteria — `adopt && validate && lock && plan` shows zero changes against a non-trivial existing setup; literal credentials are stripped with placeholders and warnings.

**Dependencies:** None. Can land in parallel with U1-U5. Benefits from U4 existing so users can `adopt` then `status` to inspect.

**Files:**
- `cmd/ainfra/cmd_adopt.go` (new — registered in `cmd/ainfra/main.go:18-34` between `newInitCommand` and `newValidateCommand`)
- `cmd/ainfra/cmd_adopt_test.go` (new)
- `internal/adopt/scan.go` (new — orchestrator)
- `internal/adopt/mcp.go` (new — reads `.mcp.json`)
- `internal/adopt/hooks.go` (new — reads `.claude/settings.json` hooks + `.claude/hooks/`)
- `internal/adopt/commands.go` (new — reads `.claude/commands/`)
- `internal/adopt/rules.go` (new — reads `CLAUDE.md`, `AGENTS.md`)
- `internal/adopt/secrets.go` (new — credential pattern detection + stripping)
- `internal/adopt/emit.go` (new — `manifest.Manifest` → YAML)
- `internal/adopt/*_test.go` (new for each)
- `internal/adopt/testdata/` (new — fixture `.claude/` trees and `.mcp.json` files)

**Approach:**
- Per-source readers each return a partial `manifest.Manifest`. The orchestrator merges them in a fixed order (mcp, hooks, commands, rules) and emits one canonical YAML via `yaml.Marshal` against the project's existing manifest types (`internal/manifest/types.go:43-63`).
- Each reader reuses provider `Observe` parsing logic where possible — `claudecode.mcp.go:28-55` for `.mcp.json` shape, `claudecode.hooks.go:31-42` for settings.json hooks.
- **Secret stripping**: any literal value matching common patterns (`ghp_*`, `sk-*`, `xoxb-*`, `Bearer *`, generic `*token*` / `*key*` / `*password*` keys with non-template-form values) is replaced with a `direct` reference placeholder pointing at a synthesized env-var name. One warning line per stripped secret in command output.
- **Skills**: per design §1, skills committed to a repo's own `.claude/skills/` are out of scope for ainfra (they arrive with `git clone`). Adopt does not enumerate or write entries for them. Externally-sourced skills (from a different repo) are not detectable from on-disk state; adopt emits a comment in the YAML pointing the user at the skills section to fill in.
- **Plugins / tools permissions**: included in v1 if the parsing is straightforward; otherwise emit a `# TODO: review` comment and a stderr note. Decision in plan: include plugins, defer tool-permission ingestion to a follow-up (the matching engine is the broken-upstream point; ingesting today's settings.json is low-value).
- Overwrite protection: refuse if `ainfra.yaml` exists unless `--force` or `--merge`. `--merge` adds new entries without overwriting existing keys.
- Output formatting: canonical key order matching the showcase `ainfra.yaml` (cliTools → secrets → mcpServers → hooks → skills → commands → rules).

**Patterns to follow:**
- Command shape: `cmd/ainfra/cmd_init.go` — refuse overwrite + `--force`, write file, end with `ui.Next` hint.
- Test pattern: `cmd_init_test.go` — drive via `run(...)`, `bytes.Buffer` streams, assert on file contents.
- Provider `Observe` inverse: `internal/provider/claudecode/mcp.go:28-55`, `hooks.go:31-42`.
- Error/warning style: `diag.Diagnostic{Summary, Hint, Detail}` per `cmd_init.go:70-73`.

**Test scenarios:**
- *Empty repo:* `adopt` produces a `version: 1` manifest with no channel sections, exits 0 with a hint to run `validate`.
- *MCP-only repo:* fixture `.mcp.json` with two servers → manifest has both under `mcpServers`, no other channels.
- *Hooks present in `.claude/settings.json`:* hooks land under `hooks:` with mapped event/matcher/command.
- *Slash commands in `.claude/commands/`:* each `.md` file becomes a command entry.
- *CLAUDE.md present:* rules entry references the file.
- *Literal token in `.mcp.json` env:* stripped, replaced with `secret:` reference, warning emitted.
- *Literal GitHub token (`ghp_*`):* detected, stripped, warning emitted.
- *Generic `password` key with literal value:* stripped, warning emitted.
- *Template-form env (`${VAR}`):* preserved as-is, no warning.
- *Existing `ainfra.yaml`, no flag:* error "manifest exists — use --merge or --force", exit 1.
- *Existing `ainfra.yaml`, `--force`:* overwrites.
- *Existing `ainfra.yaml`, `--merge`:* adds new entries, leaves existing keys untouched.
- *Adopt then validate then lock:* `validate` passes, `lock` runs to completion (against a fixture that doesn't require remote-source resolution).
- *Round-trip property:* a manifest emitted by adopt, when re-parsed and re-emitted, is byte-identical (sortedness, formatting).
- *No `.claude/` directory:* exits 0 with a manifest containing only what `.mcp.json` and `CLAUDE.md` provide.
- *Codex target detected (no `.claude/`, presence of Codex config):* sets `agent: codex` in the manifest.
- Covers AE for "non-trivial existing setup adopts to zero-change plan" (origin §1).

**Verification:** `go test ./internal/adopt/... ./cmd/ainfra/...` pass; manual: `ainfra adopt && ainfra validate && ainfra lock && ainfra plan` against a fixture repo shows no plan changes.

---

### U7. Documentation and spec deltas

**Goal:** Update user-facing docs to reflect the four shipped capabilities, retire the README "remote sources unbuilt" caveat, and add a new failure-mode row to the design.

**Requirements:** Brainstorm §5 (Cross-cutting documentation deltas).

**Dependencies:** U1-U6 (this lands last).

**Files:**
- `README.md` (modify — add `ainfra adopt` to the "Try it" section above `init`; add `ainfra status` to the post-apply commands; drop the "Local source files and inline … remote locations (git/npm) … remaining follow-ups" paragraph in the "Status" section)
- `docs/quickstart.md` (modify — mirror the README changes)
- `docs/reference/design.md` (modify — add a §13 table row: "MCP tool description drift" → "Where it bit: well-documented MCP attack class" → "ainfra's defense: lockfile records `toolsetHash` of `tools/list` at lock; `check` re-fetches and exits non-zero on drift"; add a Phase 6 entry to §10)
- `spec/lockfile-schema.md` (modify — confirm `toolsetHash` documentation is accurate; add a one-line "populated since v1" note)
- `spec/manifest-schema.md` (modify — confirm remote-source scheme list is current; add explicit `github:` form in addition to `git+`)
- `docs/reference/validation.md` (modify — add a sixth scenario for toolset hash drift, mirroring the existing five)

**Approach:**
- Documentation only. No code.
- Run `ainfra schema` and `go test ./...` post-edit to confirm nothing drifted.

**Patterns to follow:**
- Existing failure-mode table format in `docs/reference/design.md` §13.
- README "Try it" formatting in current `README.md:21-37`.

**Test scenarios:**
- None — documentation unit. `Test expectation: none -- documentation-only unit. Verification is reading the rendered Markdown.`

**Verification:** `git diff` review; `ainfra schema` output unchanged except for the `toolsetHash` field documentation; `go test ./...` still green.

## System-Wide Impact

- **Lockfile compatibility.** Additive field only; existing lockfiles read with empty `ToolsetHash` are valid. No schema version bump. Once U2 lands, the next `ainfra lock` populates the field for all MCP entries.
- **Performance at lock time.** U2 starts a subprocess per MCP server. Wall-clock impact: ~1-5s per server in the happy path; up to the configured timeout (15s default) per failing server. For a typical 3-5 server manifest, lock-time goes from ~instant to a few seconds.
- **Performance at check time.** Same subprocess overhead as lock. `ainfra check` becomes the slowest of the read-only commands. Acceptable; this is the integrity guarantee.
- **Status is fast.** U4 has zero network and zero subprocess; it's purely file I/O over loaded YAML.
- **Adopt is read-only.** U6 reads but never executes anything from the user's repo; safe to run as the first command after `git clone`.
- **Cache footprint.** U5's `~/.cache/ainfra/sources/` grows monotonically until explicit cleanup. Not addressed in v1; users can `rm -rf` it.

## Deferred Questions (to resolve during implementation)

- Exact JSON-RPC framing — line-delimited vs Content-Length headers per MCP spec evolution. Resolve when wiring U1's first integration test.
- Whether `mcpclient.Introspect` should accept a context.Context for cancellation in addition to the wall-clock timeout. Probably yes; decide when writing the signature.
- npm scoped-registry `.npmrc` parsing — deferred to a follow-up unless a test fixture forces it earlier.
- Exact secret-detection regex set in U6 — start with the patterns in the Approach bullet; expand as fixtures surface real cases.
- Whether U7 adds the new failure-mode row to §13 or a fresh §16. Decide when reading §13's current shape during edit.

## Risk Notes

- **Lock-time MCP subprocess execution is a new trust boundary.** U1's sandboxing (restricted env, timeout, no shell inheritance) is the entire defense. Review the runner invocation surface carefully in U1's PR. The pre-existing `provider.CommandRunner` injection point limits blast radius.
- **GitHub / npm rate limits.** Without auth, GitHub API rate-limits aggressively. U5 should surface rate-limit errors clearly (e.g., "GitHub API rate-limited — set `GITHUB_TOKEN`").
- **Adopt's secret-detection is best-effort.** False negatives (literal value not matching any pattern) are possible. U6 should emit a one-line caveat at the end of every adopt run: "review the manifest before committing — secret detection is best-effort."
- **Brittle MCP servers may not survive lock-time introspection.** Some servers initialize slow or expect non-default args. The `unverified` fallback path covers correctness, but the resulting lockfile is weaker. Document in U2's command output how to debug.

## References

- Origin: `docs/brainstorms/2026-05-27-v1-ship-sharpener-requirements.md`
- Problem evidence: `docs/problem-space.md`
- Design principles: `docs/design-philosophy-references.md`
- Current design: `docs/reference/design.md`
- Showcase manifest: `ainfra.yaml`
- Lockfile types: `internal/lockfile/types.go`
- Lockfile spec: `spec/lockfile-schema.md`
- Resolve pipeline: `internal/resolve/pipeline.go`
- Fetch primitives: `internal/provider/fetch/`
- Init command (closest analog for adopt): `cmd/ainfra/cmd_init.go`
- Plan command (closest analog for status): `cmd/ainfra/commands.go` `newPlanCommand`
- Check command (drift detection seam): `cmd/ainfra/commands.go` `runCheck`
