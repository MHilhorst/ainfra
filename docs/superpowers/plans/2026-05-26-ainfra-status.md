# ainfra — current state and open work

Status snapshot after the tvt-config migration. A checkpoint, not an
implementation plan — see the linked specs for those.

---

## Where ainfra is today

ainfra is live for the trein-vertraging team and replaces the `tvt` CLI end to
end. One command does everything:

- **`ainfra apply`** — full setup: marketplace + plugin installs, MCP server
  configs, `~/.claude/CLAUDE.md` render, hooks, commands, rules, tools,
  backgroundService scripts. Final step: resolve secrets and materialize
  credential files. Idempotent.
- **`ainfra sync`** — resolves all 39 secrets from **two 1Password env-file
  notes** (one shared, one personal) into `~/.claude/settings.local.json` and
  writes **4 credential files** via `path:` secrets.
- **`ainfra check`** — verifies both config drift AND preconditions (incl.
  live `dns-resolves` — the VPN signal that `tvt doctor` used to give).

Verified live: 14/14 `tvt-config` MCP servers show "Connected"; Flare returned
live data using the synced `FLARE_API_TOKEN`; the 4 credential files are
byte-identical to the pre-migration backups; `apply` finishes with
"Apply complete." and 0 failures.

`claude-config` is minimal — the `tvt` CLI (`cli/`, 69 files) is deleted; the
manifest + the plugin content is the repo.

## What landed this session

ainfra (`origin/main`):

| Commit | Change |
|---|---|
| `082aa46` | `envFile` secrets — one ref expands to a whole environment |
| `2d58e02` | `path:` secrets — credential files materialized from 1P |
| `0a9b1bf`, `c1bbb6a`, `d9ea8c7` | the `marketplaces` channel wired through buildLedger, mergeLocks, ResourcesByChannel + channelPrefix + ApplyOrder + lockfile io |
| `d0bf9d1` | `check` runs preconditions; `dns-resolves` implemented via `net.LookupHost` |
| `fe380d6` | ownership-boundaries design spec |

claude-config (`origin/main`):

- env-file migration in `ainfra.yaml`; credential files declared as `path:`
  secrets; per-secret `secrets:` entries and MCP `secret:` bindings retired;
  MySQL template parameterized
- `tvt` CLI deletion (`cli/` removed) and every `tvt`-command reference in
  README, `CLAUDE.md.template`, and 8 skills updated to `ainfra` equivalents
- `tipctl` dropped from `cliTools` (composer install model doesn't fit; the
  `using-transip-cli` skill owns its bootstrap)
- `.mcp.json` MCP servers pinned to manifest versions (interim until ainfra
  renders the live file)
- `.gitignore` for `.ainfra/` and `.claude/settings.local.json`
- Latest: `06914af`

## What's open — design captured, implementation pending

See `docs/superpowers/specs/2026-05-22-ainfra-ownership-boundaries-design.md`
for the root principle (*own configuration; declare and verify substrate*) and
the recommended decisions. Items left to implement:

1. **MCP single source of truth** *(spec #1 arch + #5)* — ainfra renders the
   plugin's `.mcp.json`; `apply` stops writing into the repo working tree.
   Interim: `.mcp.json` is hand-maintained, pinned.
2. **cliTools install-strategy model** *(spec #2)* — `install: { via: brewfile
   | run | manual }`; render a Brewfile; delegate to `brew bundle`; drop the
   bespoke brew/npm/composer adapters.
3. **Tunnels → precondition** *(spec #3)* — demote the
   `mysql-over-ssh-tunnel` template's `backgroundService` to a `port-listening`
   precondition; `install-tunnels.sh` stays the owner.
4. **Recipe → artifact** *(spec #6, strategic)* — pin delivered *bytes* in
   `ainfra.lock` (exact resolved versions + integrity); grow
   `publish`/subscriber-mode into a real artifact a subscriber applies
   verbatim. Needs its own plan.

## Honest gaps not yet covered

- **Truly fresh-instance install is untested.** `apply` only ran on machines
  already configured by `tvt`, so cliTool installs were no-ops. Onboarding is
  only really verified by a clean macOS VM or a teammate's first-time
  `ainfra apply`.
- **VPN-gated MCP servers** (`metabase`, the 2 prod DBs) only proven to
  handshake. Full functional test needs the VPN up + a live query through each
  tunnel.
- **Scheduled jobs** (`cron-jobs.json` + `scripts/manage-crons.sh`) remain
  outside ainfra by explicit team decision.

## Reference

- ainfra: `github.com/MHilhorst/ainfra` (`main` — `fe380d6`)
- Manifest: `claude-config/ainfra.yaml`
- Live MCP config: `claude-config/.mcp.json` (plugin owns, pinned)
- Design spec for the open work:
  `docs/superpowers/specs/2026-05-22-ainfra-ownership-boundaries-design.md`
- Older assessment of ainfra vs the real tvt-config:
  `docs/assessment-vs-real-config.md` (pre-this-session — partly superseded)
