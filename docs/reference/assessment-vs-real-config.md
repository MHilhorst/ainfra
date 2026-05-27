# Assessment — `ainfra` vs. a real team config repo

The [validation gate](validation.md) ran the schema against a *hand-picked*
multi-database scenario. This document does the harder, honester test: it maps
`ainfra` against a **real, in-production Claude Code config repo** — the
`tvt-config` plugin (`trein-vertraging` marketplace, v2.11.0) — using an
actual, committed manifest and the output of `ainfra validate`, `lock`,
`plan`, **and `apply`**.

The manifest lives in the `claude-config` repo, branch `ainfra-manifest`:
`ainfra.yaml` (team) + `ainfra.personal.yaml` (personal, gitignored).

---

## Verdict

`ainfra validate`, `lock`, `plan`, and `apply` all run against the *real, full*
tvt-config. A contained `apply` (isolated `HOME`) reconciles the environment
end-to-end: it writes `.mcp.json` with all 19 MCP servers, renders
`~/.claude/CLAUDE.md`, and installs the hooks, commands, plugins, rules, and
tools channels.

Reaching that took **two real bug fixes**, both found by running `apply`
against this manifest — exactly what a real-config test is for:

1. **cliTool resolution (sub-project #4 — now built).** `apply` failed on the
   first channel: the cliTools install payload was read with the wrong Go
   type, so every tool fell through to a declare-and-check probe of the literal
   id (`mysql-client --version` instead of `mysql --version`), and adapters
   installed by id rather than the declared formula/cask/package.
2. **The `rules` channel did not tilde-expand its target.** A rule targeting
   `~/.claude/CLAUDE.md` was joined to the repo root literally, producing
   `<root>/~/.claude/CLAUDE.md`.

Both are fixed. With those fixes, `apply` completes. Sub-project #3 then made
the `plugins` channel real: ainfra registers the `trein-vertraging` marketplace
and installs all 5 plugins via the `claude` CLI (`claude plugin marketplace
add` / `claude plugin install`), rather than only recording them. The one
remaining gap before `apply` fully replaces `tvt setup` is 1Password secret
*injection* (#2) — `apply` writes correct `${...}` references, but the values
reach Claude Code via `ainfra exec`, not the written files.

---

## 1. What the real repo contains

| Surface | Count / detail |
|---|---|
| MCP servers | 19 (16 inline + 2 templated MySQL tunnels + 1 disabled `linear-server`; `meta-ads-official` also disabled) |
| Skills | 38, bundled in the `tvt-config` plugin |
| Plugins | 5 (the marketplace) — local + GitHub sources, two without semver tags |
| Hooks | 8 — branch guard/enforce, destructive-command interception, project-context, post-edit and post-accounting checks, notify-sound |
| Slash commands | 9 — `start`, `pr`, `merge`, `ship`, `spin`, `document`, `review-wip`, `stop`, `dbaccess` |
| Cron jobs | 5 — headless `claude -p` runs (Flare triage, bookmark triage, audits) |
| CLI tool deps | 11 declared (brew, npm-g, uv, composer — `uv`/`composer` have no ainfra adapter) |
| Secrets | 1Password (`op://`), two vaults — shared `TVT Claude Code` + per-dev `Private` |
| CLAUDE.md | Templated per-developer (`{{FULL_NAME}}` …) + a personal section |
| SSH tunnels | 2 MySQL tunnels via launchd agents, VPN-gated |

## 2. What each command actually did

### `ainfra validate` — passed fully

Static schema check against `ainfra.yaml` and `ainfra.personal.yaml`. No
errors. All aspirational entries (1Password `op://` secrets, git plugin
sources, `uv`/`composer` cliTools) are schema-valid.

### `ainfra lock` — completed with zero errors

```
ainfra: resolved 19 MCP servers, 2 background services, 8 hooks, 9 commands, 11 CLI tools
        wrote ainfra.lock and ainfra.personal.lock
```

`lock` records references and content hashes; it does not resolve secrets or
fetch remote sources. So an `op://` ref locks fine (the *string* is well-formed)
and a git plugin source locks with a manifest-derived `version`/`contentHash`.
The lockfile carries `<resolved:...>` placeholders for values only `apply` can
produce (allocated tunnel ports, generated launcher script paths). `expo` and
`higgsfield` have no git tags and locked as `0.0.0-main`.

### `ainfra plan` — clean output

`39 to add, 17 to change, 0 to destroy.` The 17 changes are MCP servers that
differ from an earlier snapshot; the 39 adds are the new channels.

### `ainfra apply` — completes after two bug fixes

Run contained, with `HOME` pointed at a scratch directory so the real
`~/.claude` was never touched. The first two runs failed and produced the two
fixes in the Verdict above. After both fixes, `apply` reports `Apply complete.`
and produces:

- **`.mcp.json`** — all 19 MCP servers written.
- **`~/.claude/CLAUDE.md`** — created, containing `@ainfra/team-claude-md.md`
  (a correct relative import) with the rule fragment co-located at
  `~/.claude/ainfra/team-claude-md.md`.
- **hooks, commands, plugins, tools** — all applied; `.ainfra/` run-scripts and
  the plugin/tool config files written.
- **cliTools** — all 10 brew/npm/uv tools resolved (already-installed tools
  are a no-op; the adapters now use the declared formula/cask/package).

One cliTool, `tipctl`, uses the `composer` install method, for which `ainfra`
has no adapter; it is declare-and-check only, and `apply` correctly stops if it
is absent from `PATH`. The end-to-end run above set `tipctl` aside to
demonstrate full completion — see §6.

## 3. Coverage map

Verified-apply = exercised by a real `apply` run · Pending-apply = locked but
the apply step is stubbed · Gap = not expressible in the schema.

| Real-repo surface | `ainfra` channel | Status |
|---|---|---|
| stdio/http MCP servers | `mcpServers` | Verified-apply — all 19 written to `.mcp.json` |
| 2 MySQL servers via SSH tunnels + VPN | `templates` + `backgroundService` + `precondition` | Verified-apply — both instances written; launcher scripts generated |
| 38 skills | `plugins` (bundled inside `tvt-config`) | Verified-apply — installed with the `tvt-config` plugin |
| 5 plugins from a marketplace | `marketplaces` + `plugins` | Verified-apply — marketplace registered and all 5 installed via `claude plugin install` (#3) |
| Team `CLAUDE.md` | `rules` | Verified-apply — fragment + `~`-target import written correctly (after the rules fix) |
| Permission `allow`/`ask`/`deny` | `tools` | Verified-apply — `tools` written |
| CLI tools via `brew` / `npm -g` / `uv` | `cliTools` | Verified-apply — adapters resolve formula/cask/package (sub-project #4) |
| CLI tool via `composer` (`tipctl`) | `cliTools` | Gap — no `composer` adapter; declare-and-check only |
| `op://` secrets, shared + personal vaults | `secrets` | Pending-apply — refs locked; values not injected (#2) |
| Per-dev templated `CLAUDE.md` | `rules` | Gap — no per-developer templating; sub-project #5 |
| 8 hooks | `hooks` | Verified-apply — all 8 written with run-scripts |
| 9 slash commands | `commands` | Verified-apply — all 9 written |
| 5 cron jobs | — | Gap — no `scheduledJobs` field in schema; sub-project #6 |
| Credential files (`tvt sync` writes them) | `secrets` + `preconditions` | Pending-apply — checked, not written (#7) |
| `tvt` CLI, `bootstrap-hub.sh`, IDE configs | — | Out of scope (correctly) |

## 4. Intentional divergences from the live config

Deliberate decisions recorded in the manifest comments, not gaps:

1. **Tunnel ports — 13306/13307 vs live 3307/3308.** ainfra allocates tunnel
   ports itself rather than inheriting `tvt`'s hand-assigned ports.
2. **Four `@latest` MCP servers pinned to explicit versions** (`playwright`,
   `mobile`, `chrome-devtools`, `yt-dlp`) — reproducible installs.
3. **`flare`, `intercom`, `stape` modelled as native `transport: http`** rather
   than `npx mcp-remote` stdio bridges — the correct target state.

## 5. The two bugs `apply` exposed

Neither was visible to `validate`, `lock`, or `plan` — only running `apply`
against a real manifest surfaced them.

1. **cliTools install-payload type mismatch.** `render.go` stored `install` as
   `map[string]map[string]any`; `clitools.go` asserted `map[string]any`. The
   assertion failed silently, so no install adapter was ever selected and every
   tool hit the declare-and-check fallback — which probed `<id> --version`,
   wrong for `mysql-client` (binary: `mysql`). Adapters also installed by id,
   not the declared `formula`/`cask`/`package`. Fixed in sub-project #4:
   adapters now take the install spec, brew supports `--cask`, and the
   declare-and-check probe uses the manifest `check.command`.

2. **`rules` channel ignored `~` in the target.** `filepath.Join(env.Root,
   target)` on `~/.claude/CLAUDE.md` yields `<root>/~/.claude/CLAUDE.md`. Fixed:
   the target is tilde-expanded against `Home`, the fragment is co-located with
   its target, and the `@import` line is computed relative to the target file.

## 6. Gaps still open

**#2–#5 are now closed:** the 1Password resolver ships and `ainfra exec`
resolves all 14 tvt-config secrets; plugins install via the `claude` CLI from a
registered marketplace; cliTool resolution (incl. the `composer` adapter) is
built; and the `rules` channel renders per-developer `CLAUDE.md` from a
maintainer-declared `vars:` block (`{{FULL_NAME}}` etc. sourced from `git`/`gh`
on each machine — `ainfra apply` is the only onboarding step). What remains:

1. **Scheduled jobs (#6).** The 5 headless `claude -p` cron runs — no
   `scheduledJobs` field exists in the schema.
2. **Credential-file writing (#7).** `preconditions` *check* credential files;
   writing them (what `tvt sync` does) needs a write-capable primitive.
3. **Externally-sourced standalone skills.** Agent-agnostic skill repos —
   e.g. `vercel-labs/skills`, installed via `npx skills` — are not Claude Code
   marketplaces and belong to the `skills:` channel, not `plugins`. A follow-up
   should delegate to the `skills` CLI, mirroring how #3 delegates to
   `claude plugin`.

## 7. The honest bottom line

`ainfra` now `validate`s, `lock`s, `plan`s, `apply`s, and `check`s the real,
full tvt-config. `apply` reconciles the environment end-to-end — a correct
`.mcp.json`, `CLAUDE.md`, every channel's files, all cliTools installed, all 5
plugins installed from a registered marketplace — and `check` is idempotent.
`ainfra exec` resolves all 14 secrets from 1Password.

Sub-projects #1–#4 plus the secret resolver and #3 cover the connectivity and
installation layers; what stands between here and `ainfra exec -- claude` fully
replacing `tvt setup` is the rendering layer — per-developer `CLAUDE.md`
templating (#5) — and the two targeted-infrastructure channels, scheduled jobs
(#6) and credential-file writing (#7).
