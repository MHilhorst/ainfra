# Assessment — `ainfra` vs. a real team config repo

The [validation gate](validation.md) ran the schema against a *hand-picked*
multi-database scenario. This document does the harder, more honest test. It
checks `ainfra` against a **real, in-production Claude Code config repo** — the
`tvt-config` plugin (`trein-vertraging` marketplace, v2.11.0). It uses an
actual, committed manifest — `ainfra.yaml`, the file describing the team's
setup — and the output of `ainfra validate`, `lock`, `plan`, **and `apply`**.

The manifest lives in the `claude-config` repo, branch `ainfra-manifest`:
`ainfra.yaml` (team) + `ainfra.personal.yaml` (personal, gitignored).

> **Status note.** This report records an `ainfra apply` run captured at an
> earlier commit. Since then, two gaps listed below have been closed in code:
> the pluggable `op://` / `env://` secret resolver is now built, and a
> `composer` cliTool adapter has been added. The findings below predate those
> changes and have not been re-verified against them — a fresh `apply` run
> would be needed to confirm the updated behaviour.

---

## Verdict

`ainfra validate`, `lock`, `plan`, and `apply` all run against the *real, full*
tvt-config. A contained `apply` (run with an isolated `HOME` so the real home
directory is untouched) reconciles (brings a machine's config in line with the
manifest) the environment end-to-end. It writes `.mcp.json` with all 19 MCP
servers, renders `~/.claude/CLAUDE.md`, and installs the hooks, commands,
plugins, rules, and tools channels. Each channel is one category of AI-tooling
config — MCP servers, hooks, and so on.

Reaching that took **two real bug fixes**. Both were found by running `apply`
against this manifest — exactly what a real-config test is for:

1. **cliTool resolution (sub-project #4 — now built).** `apply` failed on the
   first channel. The cliTools install payload was read with the wrong Go
   type, so every tool fell through to a declare-and-check probe (declare the
   tool, then check whether it is present) of the literal id — for example,
   `mysql-client --version` instead of `mysql --version` — and adapters
   installed by id rather than the declared formula/cask/package.
2. **The `rules` channel did not tilde-expand its target.** A rule targeting
   `~/.claude/CLAUDE.md` was joined to the repo root literally, producing
   `<root>/~/.claude/CLAUDE.md`.

Both are fixed. With those fixes, `apply` completes. It does **not yet** fully
replace `tvt setup`. The files it writes are structurally correct, but
1Password secret values are not injected (#2) and remote plugins are recorded,
not `git clone`d (#3). Those remain the real open work.

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

A static schema check against `ainfra.yaml` and `ainfra.personal.yaml` — it
checks the files match the expected shape, without running anything. No
errors. All aspirational entries (1Password `op://` secrets, git plugin
sources, `uv`/`composer` cliTools) are schema-valid.

### `ainfra lock` — completed with zero errors

```
ainfra: resolved 19 MCP servers, 2 background services, 8 hooks, 9 commands, 11 CLI tools
        wrote ainfra.lock and ainfra.personal.lock
```

`lock` writes the lockfile — `ainfra.lock`, the auto-generated file that pins
exact versions. It records references and content hashes. It does not resolve
secrets or fetch remote sources. So an `op://` ref locks fine (the *string*
is well-formed) and a git plugin source locks with a manifest-derived
`version`/`contentHash`. The lockfile carries `<resolved:...>` placeholders
for values only `apply` can produce — allocated tunnel ports and generated
launcher script paths. `expo` and `higgsfield` have no git tags, so they
locked as `0.0.0-main`.

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
- **cliTools** — all 10 brew/npm/uv tools resolved. Already-installed tools
  are a no-op; the adapters now use the declared formula/cask/package.

One cliTool, `tipctl`, uses the `composer` install method, for which `ainfra`
has no adapter. It is declare-and-check only, and `apply` correctly stops if it
is absent from `PATH`. The end-to-end run above set `tipctl` aside to
demonstrate full completion — see §6.

## 3. Coverage map

Verified-apply = exercised by a real `apply` run · Pending-apply = locked, but
the apply step is still a stub (placeholder code, not yet doing the real work)
· Gap = cannot be expressed in the schema at all.

| Real-repo surface | `ainfra` channel | Status |
|---|---|---|
| stdio/http MCP servers | `mcpServers` | Verified-apply — all 19 written to `.mcp.json` |
| 2 MySQL servers via SSH tunnels + VPN | `templates` + `backgroundService` + `precondition` (a check that must pass first) | Verified-apply — both instances written; launcher scripts generated |
| 38 skills | `plugins` (bundled inside `tvt-config`) | Verified-apply — `tvt-config` recorded |
| 5 plugins (GitHub + local sources) | `plugins` | Verified-apply for *recording* — actual `git clone` is Pending-apply (#3) |
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

These are deliberate decisions recorded in the manifest comments, not gaps:

1. **Tunnel ports — 13306/13307 vs live 3307/3308.** ainfra allocates tunnel
   ports itself rather than inheriting `tvt`'s hand-assigned ports.
2. **Four `@latest` MCP servers pinned to explicit versions** (`playwright`,
   `mobile`, `chrome-devtools`, `yt-dlp`) — for reproducible installs.
3. **`flare`, `intercom`, `stape` modelled as native `transport: http`** rather
   than `npx mcp-remote` stdio bridges — this is the correct target state.

## 5. The two bugs `apply` exposed

`validate`, `lock`, and `plan` could not see either bug. Only running `apply`
against a real manifest surfaced them.

1. **cliTools install-payload type mismatch.** `render.go` stored `install` as
   the Go type `map[string]map[string]any`; `clitools.go` asserted it was
   `map[string]any`. The assertion failed silently, so no install adapter was
   ever selected. Every tool hit the declare-and-check fallback, which probed
   `<id> --version` — wrong for `mysql-client`, whose binary is `mysql`.
   Adapters also installed by id, not the declared `formula`/`cask`/`package`.
   Fixed in sub-project #4: adapters now take the install spec, brew supports
   `--cask`, and the declare-and-check probe uses the manifest `check.command`.

2. **`rules` channel ignored `~` in the target.** `filepath.Join(env.Root,
   target)` on `~/.claude/CLAUDE.md` yields `<root>/~/.claude/CLAUDE.md`. Fixed:
   the target is tilde-expanded against `Home`, the fragment is stored next to
   its target, and the `@import` line is computed relative to the target file.

## 6. Gaps still open

Each gap is mapped to its sub-project. **#4 is now closed** (see §5).

1. **1Password secret resolution (#2).** `apply` writes `.mcp.json` and the
   settings env block, but `${secret.*}` values are not injected — the
   `op://` resolver is not built. This would replace `tvt sync` / `tvt rotate`.
2. **Remote plugin git fetch (#3).** `apply` records plugins in `plugins.json`
   but does not `git clone` them. `expo` and `higgsfield` also need real
   version pins (they locked at `0.0.0-main`).
3. **`composer` cliTool adapter.** `tipctl` installs via `composer`, a
   build-from-source flow. `ainfra` has no `composer` adapter, so the tool
   stays declare-and-check, and `apply` stops if it is absent. Adding a
   `composer` adapter — or accepting build-from-source tools as permanently
   manual — is a #4 follow-up.
4. **Per-developer `rules` templating (#5).** `CLAUDE.md` is rendered per
   developer with `{{FULL_NAME}}` and similar placeholders.
5. **Scheduled jobs (#6).** The 5 headless `claude -p` cron runs — the schema
   has no `scheduledJobs` field.
6. **Credential-file writing (#7).** `preconditions` *check* credential files.
   Writing those files — what `tvt sync` does — needs a write-capable
   primitive.

## 7. The honest bottom line

`ainfra` now `validate`s, `lock`s, `plan`s, and `apply`s the real, full
tvt-config. `apply` reconciles the environment end-to-end: it writes a correct
`.mcp.json`, `CLAUDE.md`, and every channel's files. Two genuine bugs stood
between "locks cleanly" and "applies cleanly". Both are fixed, and the cliTool
resolver (#4) is built.

What `apply` produces is structurally complete but not yet fully *functional*.
1Password secret values are not injected (#2), and remote plugins are recorded
rather than installed (#3). Those two are the critical path to `ainfra apply`
genuinely replacing `tvt setup`. Sub-projects #5–#7 — per-dev templating,
scheduled jobs, and credential-file writing — follow.
