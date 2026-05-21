# Assessment — `ainfra` vs. a real team config repo

The [validation gate](validation.md) ran the schema against a *hand-picked*
multi-database scenario. This document does the harder, honester test: it maps
`ainfra` against a **real, in-production Claude Code config repo** — the
`tvt-config` plugin (`trein-vertraging` marketplace, v2.11.0) — using an
actual, committed manifest and the output of `ainfra validate`, `lock`, and
`plan`.

The manifest lives in the `claude-config` repo, branch `ainfra-manifest`:
`ainfra.yaml` (team) + `ainfra.personal.yaml` (personal, gitignored).

---

## Verdict

`ainfra validate` and `ainfra lock` both pass against the *real, full*
tvt-config — a stronger result than the spec predicted. The spec expected
`lock` to partially fail and produce concrete error text as gap evidence; that
did not happen. `lock` completed with zero errors, resolving 19 MCP servers,
2 background services, 8 hooks, 9 commands, 11 CLI tools, 5 plugins, rules,
and tools in one pass.

**This does not equal "ainfra replaces tvt setup."** `lock` records references
and content hashes; it does not exercise secret resolution, remote plugin fetch,
or cliTool install. Those are deferred to `ainfra apply`, which was
deliberately not run in this sub-project. The genuine gaps remain real — they
are just not visible in the lockfile, which carries `<resolved:...>`
placeholders where runtime values will eventually be substituted. The next
real test is `ainfra apply`.

---

## 1. What the real repo contains

| Surface | Count / detail |
|---|---|
| MCP servers | 19 active (16 inline + 2 templated MySQL tunnels + 1 `linear-server`) |
| Skills | 38, bundled in the `tvt-config` plugin |
| Plugins | 5 (the marketplace) — local + GitHub sources, two with no semver tags |
| Hooks | 8 — branch guard/enforce, destructive-command interception, project-context, post-edit and post-accounting checks, notify-sound |
| Slash commands | 9 — `start`, `pr`, `merge`, `ship`, `spin`, `document`, `review-wip`, `stop`, `dbaccess` |
| Cron jobs | 5 — headless `claude -p` runs (Flare triage, bookmark triage, audits) |
| CLI tool deps | 11 declared (brew, npm-g, uv, composer — several without an ainfra adapter yet) |
| Secrets | 1Password (`op://`), two vaults — shared `TVT Claude Code` + per-dev `Private` |
| CLAUDE.md | Templated per-developer (`{{FULL_NAME}}` …) + a personal section |
| SSH tunnels | 2 MySQL tunnels via launchd agents, VPN-gated |

## 2. What `validate` and `lock` actually did

### `ainfra validate` — passed fully

Static schema check against both `ainfra.yaml` and `ainfra.personal.yaml`. No
errors. All aspirational entries (1Password `gateway` secrets, git plugin
sources, `uv`/`composer` cliTools) are schema-valid. This confirms the
manifest is well-formed, not that its references will resolve at apply time.

### `ainfra lock` — completed with zero errors

```
ainfra: resolved 19 MCP servers, 2 background services, 8 hooks, 9 commands, 11 CLI tools
        wrote ainfra.lock and ainfra.personal.lock
```

Every declared entry was accepted and written to `ainfra.lock`. No iterative
comment-out was needed. Key evidence from the lockfile:

**Templated MySQL tunnels — allocated ports, placeholder launchers:**
```yaml
trein-vertraging-platform-db-prod:
    layer: repo
    fromTemplate: mysql-over-ssh-tunnel
    resolved:
        launcher: <resolved:trein-vertraging-platform-db-prod.launcher>
        tunnelPort: 13307
    version: 2.0.8
    requires:
        - svc:trein-vertraging-platform-db-prod-tunnel
```
`tunnelPort` was allocated (13307) but `launcher` is a `<resolved:...>`
placeholder — the launchd plist path is not generated until `apply`. The same
pattern appears in `backgroundServices`:
```yaml
trein-vertraging-business-portal-db-prod-tunnel:
    resolved:
        launcher: <resolved:trein-vertraging-business-portal-db-prod.launcher>
        tunnelPort: 13306
```

**Plugins without semver tags — locked as `0.0.0-main`:**
```yaml
expo:
    layer: repo
    version: 0.0.0-main
    contentHash: sha256:daa26ad115e7589a0ab68b1744c6aab21d7f8e37a81f7fdb9ebda8726f5bded5
higgsfield:
    layer: repo
    version: 0.0.0-main
    contentHash: sha256:d25a521a5d783fa805b700b4bc010f3e86f5c51a6a784aa6fd4a5ffe421d85f3
```
`expo` and `higgsfield` have no git tags in their repos; `lock` recorded the
branch head as `0.0.0-main`. Remote fetch (the actual `git clone`) is
sub-project #3.

### `ainfra plan` — clean output

```
39 to add, 17 to change, 0 to destroy.
```
The 17 changes are MCP servers that differ from an earlier lockfile snapshot;
the 39 adds are all the new channels (hooks, commands, backgroundServices,
cliTools, plugins, rules, tools) being introduced for the first time. Zero
destroys.

## 3. Coverage map

Verified = confirmed by `validate`/`lock` run · Pending-apply = locked but not
exercised at apply time · Gap = not expressible in the schema.

| Real-repo surface | `ainfra` channel | Status |
|---|---|---|
| stdio MCP servers (`npx`/`uvx`) | `mcpServers` | Verified — all 17 non-templated servers locked with `contentHash` |
| HTTP MCP servers with auth `headers` | `mcpServers` | Verified — `flare`, `intercom`, `stape` locked as native `transport: http` (see §5) |
| 2 MySQL servers via SSH tunnels + VPN | `templates` + `backgroundService` + `precondition` | Verified — both templated instances locked; launcher paths pending-apply |
| 38 skills | `plugins` (bundled inside `tvt-config`) | Verified — `tvt-config` plugin locked |
| 5 plugins (GitHub sources, two without semver tags) | `plugins` | Verified — all 5 locked; `expo` and `higgsfield` at `0.0.0-main`; remote fetch pending-apply (#3) |
| Team `CLAUDE.md` (static) | `rules` | Verified — `team-claude-md` locked with `contentHash` |
| Per-dev templated `CLAUDE.md` | `rules` | Gap — no per-developer templating; sub-project #5 |
| Permission `allow`/`ask`/`deny` | `tools` | Verified — `tools` entry locked |
| CLI tools via `brew` / `npm -g` | `cliTools` | Verified in lock — actual install pending-apply (#4) |
| CLI tools via `uv` / `composer` / source | `cliTools` | Locked as declare-and-check; no adapter resolver yet (#4) |
| `op://` secrets, shared + personal vaults | `secrets` | Verified — all refs locked; 1Password resolution pending-apply (#2) |
| Secrets materialised to credential files | `secrets` + `preconditions` | Verified schema-only — ainfra checks the file exists; write is pending-apply (#7) |
| `requires` dependency graph (VPN → tunnel → MCP) | `requires` | Verified — `metabase-prod` and both tunnel services carry `pre:vpn-tvt-internal` edges |
| 8 hooks | `hooks` | Verified — all 8 locked with `contentHash` and `requires` edges |
| 9 slash commands | `commands` | Verified — all 9 locked with `contentHash` |
| 5 cron jobs | — | Gap — no `scheduledJobs` field in schema; sub-project #6 |
| `tvt` CLI, `bootstrap-hub.sh`, IDE configs | — | Out of scope (correctly) |

## 4. Why `lock` succeeded where the spec expected failure

The spec assumed `lock` would fail on unresolvable entries (1Password `op://`
references, git plugin sources, `uv`/`pip`/`composer` cliTool install blocks).
That assumption was wrong. `lock` does not resolve secrets or fetch remote
sources — it records references and computes content hashes. An `op://` ref is
valid to `lock` because the *reference string* is well-formed; the 1Password
gateway is not consulted until `apply`. Similarly, a git plugin source is
locked with a `version` and `contentHash` derived from the manifest, not from
actually cloning the repo.

The `<resolved:...>` placeholders in the lockfile mark the boundary: they are
fields whose values can only be determined at apply time (allocated port numbers
become real ports; generated script paths become real filesystem paths). Those
are the genuine pending items, not errors.

## 5. Intentional divergences from the live config

These are deliberate decisions recorded in the manifest comments, not gaps:

1. **Tunnel ports — 13306/13307 vs live 3307/3308.** ainfra allocates tunnel
   ports itself; it does not inherit the hand-assigned ports `tvt` uses. The
   manifest notes this explicitly. The live ports will not conflict; ainfra's
   allocated ports are independent.

2. **Four `@latest` MCP servers pinned to explicit versions.** `playwright`
   (0.0.75), `mobile` (0.0.56), `chrome-devtools` (1.0.1), and `yt-dlp`
   (0.9.0) are pinned rather than tracking `@latest`. This is an improvement —
   reproducible installs — not a gap.

3. **`flare`, `intercom`, `stape` modelled as native `transport: http` instead
   of `npx mcp-remote` stdio bridges.** The live `.mcp.json` wraps these HTTP
   servers with `npx -y mcp-remote <url>` because Claude Code's MCP client
   lacked direct HTTP support at the time. The ainfra manifest declares them
   at their HTTP endpoint directly. This is the correct target state; apply
   will need to verify Claude Code supports the transport.

## 6. Gaps still open

These are not closed by sub-project #1. Each is mapped to its sub-project:

1. **1Password secret resolution (#2).** All `op://` references are locked as
   reference strings. `ainfra apply` cannot inject them into the Claude Code
   environment until the `gateway`/`op://` resolver is built. Replaces
   `tvt sync` / `tvt rotate`.

2. **Remote plugin git fetch (#3).** The `expo` and `higgsfield` plugins are
   locked at `0.0.0-main` because their repos have no semver tags. The
   `claude-ads`, `compound-engineering`, and `tvt-config` plugins are locked
   from local paths. `ainfra apply` cannot `git clone` any of them until the
   remote fetch resolver lands.

3. **CLI tool install adapters (#4).** `brew` entries are declared and locked;
   the brew adapter is the first priority. `uv` (`meta`), `composer` (`tipctl`),
   and `ssh` (system-provided, no install block) have no ainfra adapter yet.
   Until #4 lands, `cliTools` entries are verify-only.

4. **Per-developer `rules` templating (#5).** The real `CLAUDE.md` is rendered
   per developer with `{{FULL_NAME}}` and similar placeholders. The `rules`
   channel is static-file-oriented; per-developer identity capture is
   deferred.

5. **Scheduled jobs (#6).** The 5 headless `claude -p` cron runs cannot be
   expressed — no `scheduledJobs` field exists in the manifest schema. The
   design exists (Iteration 4, reverted from `main`) and is deferred, not
   abandoned.

6. **Credential-file writing (#7).** The manifest uses `preconditions` to
   *check* that credential files exist with the right permissions. Writing
   those files (what `tvt sync` does today) requires a write-capable
   credential primitive.

## 7. The honest bottom line

`validate` and `lock` both pass against the real, full tvt-config. The manifest
is complete and well-formed. The `<resolved:...>` placeholders in `ainfra.lock`
mark what `apply` still has to do: generate launchd launcher scripts, allocate
real ports, inject 1Password secrets, fetch remote plugins, and install CLI
tools. Where `ainfra` fits today, it replaces imperative scripts (`tvt sync`,
`install-tunnels.sh`, hand-edited `.mcp.json`) with one declarative manifest
and a lockfile. With sub-projects #2–#7, that extends to the full setup
lifecycle.
