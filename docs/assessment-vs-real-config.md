# Assessment — `ainfra` vs. a real team config repo

The [validation gate](validation.md) ran the schema against a *hand-picked*
multi-database scenario. This document does the harder, honester test: it runs
the schema against a **real, in-production Claude Code config repo** — the
`tvt-config` plugin (`trein-vertraging` marketplace, v2.11.0) — and asks whether
everything that repo configures is reproducible in `ainfra` "in a nice setup
way."

**Verdict:** `ainfra` cleanly reproduces — and improves on — the *connectivity*
layer (MCP servers, the tunnel/VPN dependency chain, CLI tooling, `op://`
secrets). It did **not**, as originally specced, have channels for the
*automation* layer. That finding produced **Iteration 3** (this change): the
`hooks` and `commands` channels. One gap remains open (`scheduled jobs`).

---

## 1. What the real repo contains

| Surface | Count / detail |
|---|---|
| MCP servers | 16 active + 2 disabled — `npx`/`uvx` stdio, HTTP, one local-built binary |
| Skills | 38, bundled in the plugin |
| Plugins | 5 (the marketplace) — local + GitHub sources, one with a subpath |
| Hooks | 7 — branch guard/enforce, destructive-command interception, debug-leftover + accounting-invariant checks, TTS notify |
| Slash commands | 9 — `start`, `pr`, `merge`, `ship`, `spin`, … |
| Cron jobs | 5 — headless `claude -p` runs (Flare triage, bookmark triage, audits) |
| CLI tool deps | ~30 binaries — `brew`, `npm -g`, `pip`, `composer`, custom taps, build-from-source |
| Secrets | 1Password (`op://`), two vaults — shared `TVT Claude Code` + per-dev `Private` |
| CLAUDE.md | Templated per-developer (`{{FULL_NAME}}` …) + a personal section |
| SSH tunnels | 2 MySQL tunnels via launchd agents, VPN-gated |

## 2. Coverage map

Clean = expressible directly · Bends = expressible with a workaround or a small
schema extension · Gap = not expressible.

| Real-repo surface | `ainfra` channel | Verdict |
|---|---|---|
| stdio MCP servers (`npx`/`uvx`) | `mcpServers` | Clean — `@latest` users must pin (§5.1) |
| HTTP MCP servers with auth `headers` | `mcpServers` | Clean — `url` + `headers` added (Iteration 5) |
| build-from-source MCP binary | `mcpServers` + `cliTools` | Bends — declare-and-check only |
| 2 MySQL servers → SSH tunnels → VPN | `templates` + `backgroundService` + `precondition` | **Clean — best fit**, improves on the status quo |
| 38 skills | `plugins` (bundled) | Clean as a bundle |
| 5 plugins (GitHub sources, subpaths) | `plugins` | Bends — `source` needs git + subpath |
| Team `CLAUDE.md` (static) | `rules` | Clean |
| Per-dev templated `CLAUDE.md` | `rules` | Gap — no per-developer rules templating |
| Permission `allow`/`deny` | `tools` | Clean |
| Permission `ask` tier | `tools` | Clean — three-tier `allow`/`ask`/`deny` |
| CLI tools via `brew` / `npm -g` | `cliTools` | Clean — pinned npm globals are a strong fit |
| CLI tools via `pip` / `composer` / source | `cliTools` | Gap — no `pip`/`composer` adapter |
| `op://` secrets, shared + personal vaults | environment primitive | **Clean — strong fit** (`scope` = the two vaults) |
| secrets materialised to *files* | environment | Clean (verify-only) — ainfra checks the file via a precondition; never writes it (Iteration 5) |
| tunnel→VPN, MCP→binary dependencies | `requires` graph | **Clean — strongest fit** |
| **7 hooks** | **`hooks` (NEW — Iteration 3)** | **Clean — channel added by this change** |
| **9 slash commands** | **`commands` (NEW — Iteration 3)** | **Clean — channel added by this change** |
| 5 cron jobs | — | Gap — designed, not built (see the scheduled-jobs design doc) |
| `tvt` CLI, `bootstrap-hub.sh`, ide-configs | — | Out of scope (correctly) |

## 3. The three axes

**Scalability — strong.** 16 MCP servers, 38 skills, 9 repos across 2 orgs: the
channel + template + layer model does not bloat with count. The multi-DB
template proves N-instance scaling; team/repo/personal layering fits the
multi-repo reality. No structural concern.

**Modularity — excellent for owned channels.** The 2 MySQL tunnels today are
spread across `.mcp.json` + `install-tunnels.sh` + a plist template +
`CLAUDE.local.md` port docs + a `tvt doctor` VPN check — five artifacts of
copy-paste and prose. `ainfra` collapses that to one template + two instances +
a `vpn` precondition, with `ainfra check` replacing the manual VPN check. That
is a real win.

**Adaptability — this is where the original design fell short.** The design's
§1 declared "six channels," and the validation gate only ever exercised a
hand-picked scenario with no hooks, no commands, no cron. The real repo is the
better gate, and it showed the channel list was *incomplete*: hooks and slash
commands are pure declarative config (not processes — §9's process-supervision
non-goal does not cover them), and the team relies on them heavily. Leaving them
out was an oversight, not a decision.

## 4. Iteration 3 — what this change adds

Two new first-class channels, built to the same shape as the existing six
(layerable, `requires`-aware, content-hashed in the lockfile):

- **`hooks`** (manifest §11) — automation bound to a lifecycle event
  (`SessionStart`, `PreToolUse`, …), with an optional bundled `source` script.
- **`commands`** (manifest §12) — slash commands, modelled like `skills`: a
  `source` file plus optional `version`.

Both flow through `ainfra lock`, land in `ainfra.lock` with a `contentHash`,
and honour the layered-lockfile split. The multi-database example now carries
one hook and one command end-to-end.

This converts the two biggest "Gap" rows above into "Clean."

## 5. Iteration 5 — what this change adds

Three schema additions, closing two more gaps:

- **`mcpServers.url` + `headers`** (manifest §5.2) — HTTP MCP servers can
  declare an endpoint and auth headers.
- **`cliTools.env` / `secret` / `requires`** (manifest §7) — CLI tools get
  environment variables (delivered via the Claude Code `settings.json` env
  block), inline secret bindings, and dependency edges.
- **`file-exists` precondition `mode`** (manifest §6) — secret-to-file is
  modelled verify-only: ainfra checks a credential file exists with the right
  permissions and never writes it, keeping the environment primitive
  reference-only.

This is a schema iteration; CLI tool and non-templated MCP *resolution* at lock
time is deferred to the follow-up plan for non-templated entries.

## 6. Gaps still open

These are recorded honestly; they are *not* closed by Iteration 3:

1. **Scheduled jobs.** The 5 headless `claude -p` cron runs. A full design
   exists — a `scheduledJobs` targeted-infrastructure channel
   (`docs/superpowers/specs/2026-05-21-scheduled-jobs-design.md`). It was built
   as Iteration 4 and then reverted from `main`; it is deferred for now, not
   abandoned.
2. **Plugin `source` git + subpath.** Real marketplaces use GitHub sources.
3. **`pip` / `composer` `cliTool` adapters**, and acceptance that
   build-from-source binaries stay declare-and-check.
4. **Per-developer `rules` templating.** The real `CLAUDE.md` is rendered per
   developer; the `rules` channel is static-file-oriented.

**HTTP MCP `headers`** and **secret-to-file** were previously listed here;
Iteration 5 closes both (see §5). The **permission `ask` tier** was closed
earlier by the three-tier `tools` channel.

## 7. The honest bottom line

Where `ainfra` fits, it replaces imperative scripts (`tvt sync`,
`install-tunnels.sh`, hand-edited `.mcp.json`) with one declarative manifest and
a lockfile — the "nice setup way." With Iteration 3, that now covers the
automation layer's hooks and commands too. Scheduled jobs remain designed but
deferred; everything else open is a small, localised schema extension.
