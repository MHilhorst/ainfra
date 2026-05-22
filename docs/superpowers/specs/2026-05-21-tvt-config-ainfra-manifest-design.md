# Design — Author the tvt-config `ainfra.yaml` (sub-project #1)

## Context

Goal: make `ainfra apply` fully replace what the imperative `tvt` CLI does for
the `trein-vertraging` team's Claude Code setup. That is too large for one
spec — it decomposes into seven sub-projects (below). This document specs
**sub-project #1 only**: authoring the real `ainfra.yaml` for the `tvt-config`
repo and testing it against the current `ainfra` build.

`docs/assessment-vs-real-config.md` already maps `tvt-config` onto `ainfra`
channels on paper. This sub-project turns that paper map into a real,
committed manifest and an evidence-based gap report.

### The full decomposition (for reference — only #1 is specced here)

| # | Sub-project | Why needed |
|---|---|---|
| 1 | **Author the tvt-config `ainfra.yaml`** (this spec) | Target-state spec + test artifact |
| 2 | 1Password secret resolution (`gateway`/`op://`) | Replaces `tvt sync` / `tvt rotate` |
| 3 | Remote plugin install (git/marketplace fetch) | `fetch` is local-only today |
| 4 | CLI tool resolution (brew/npm-g/pip/composer/source) | cliTools are validate-only today |
| 5 | Per-developer rules + identity (templated `CLAUDE.md`) | `tvt setup` identity capture |
| 6 | Scheduled jobs channel (5 cron jobs) | Designed (Iteration 4, reverted) |
| 7 | Credential files (write, not just verify) | `tvt sync` writes credential files |

Out of scope entirely: Cursor/IDE mirroring; the interactive onboarding-wizard
UX of `tvt setup` (ainfra is `plan`/`apply`, not a wizard).

## Decisions

- **Completeness: aspirational-complete.** The manifest declares the full
  target state — all 18 MCP servers, ~30 cliTools, the 5-plugin marketplace,
  1Password-backed secrets — even where `ainfra` cannot yet *resolve* an entry.
  Entries that fail at `lock` are the concrete gap evidence for #2–#7.
- **Location: the `claude-config` repo.** The manifest is the real, permanent
  deliverable, authored at the `claude-config` repo root. Not an `ainfra`
  example.
- **Structure: two-file layered manifest.** `ainfra.yaml` (team) +
  `ainfra.personal.yaml` (per-dev, gitignored) — the layering
  `examples/multi-database/` already uses, and a direct match for tvt-config's
  shared-vault / personal-vault split.
- **Idiomatic translation.** The manifest uses `ainfra` idioms (templates for
  the MySQL tunnels, layered files, `requires` edges), not a 1:1
  transliteration of the current `.mcp.json` / shell scripts.

## Deliverables

All in the `claude-config` repo unless noted:

1. `ainfra.yaml` — the team layer.
2. `ainfra.personal.yaml` — the personal layer (added to `.gitignore`).
3. `ainfra.lock` — committed; whatever resolves. A partial lock is expected
   and acceptable.
4. Evidence-based gap report — `ainfra/docs/assessment-vs-real-config.md`
   rewritten from paper assessment to real `ainfra lock` / `plan` output.

## Channel-by-channel mapping

Exact item lists (the ~30 cliTools, the hooks, the commands, permission tiers)
are read from the live `claude-config` repo at implementation time —
`cli/src/toolchain.ts`, `hooks/hooks.json`, `commands/`, the plugin
`settings.json`. This is the shape.

### Team `ainfra.yaml`

| Channel | Source in tvt-config | Notes |
|---|---|---|
| `preconditions` | VPN check (`tvt doctor`) | `vpn-tvt-internal`: `dns-resolves` on `metabase.tvt.internal`. Gates the MySQL tunnels and the metabase MCP. |
| `templates` | 2 MySQL tunnels (launchd + `.mcp.json`) | One `mysql-over-ssh-tunnel` template, adapted from `examples/multi-database/`. ainfra allocates the tunnel port — an intentional divergence from tvt's fixed 3307/3308, noted in a manifest comment. |
| `mcpServers` | 18 servers (`.mcp.json`) | 2 templated (the MySQL pair) + 16 inline: metabase, flare, intercom, stape, figma, posthog, slack, context7, playwright, mobile, chrome-devtools, yt-dlp, meta-ads, linkedin, mobbin. The 2 `mcpServers_disabled` entries are omitted with an explanatory comment (ainfra has no disabled state). `@latest`-pinned servers (playwright, mobile, chrome-devtools, yt-dlp) get an explicit version per assessment §5.1 — a divergence noted in a comment. |
| `secrets` | shared 1Password vault (`TVT Claude Code`) | ~11 shared `op://` references, `scope: shared`, `gateway`-based: metabase, flare, uidotsh, prod-db-trein-vertraging, prod-db-business-portal, train-service-staging-db, slack-webhook, meta-ads, google-ads, google-analytics, tiktok-ads. References only — no values. |
| `cliTools` | ~30 binaries (`cli/src/toolchain.ts`) | brew / npm-g / pip / composer / build-from-source install blocks. pip/composer/source have no ainfra adapter yet — declared declare-and-check (a `check` block, no resolvable `install`). |
| `hooks` | hooks (`hooks/hooks.json`) | SessionStart project-context + branch-guard, UserPromptSubmit branch-check, PreToolUse enforce-branch + block-destructive, Notification notify-sound, PostToolUse post-edit-check + post-accounting-check. `source` scripts bundled. |
| `commands` | 9 slash commands (`commands/`) | start, pr, merge, ship, spin, … `source` files, content-hashed. |
| `plugins` | 5-plugin marketplace | `tvt-config` (local source) + 4 others (GitHub sources, one with a subpath). Git sources declared aspirationally; remote fetch is sub-project #3. The 38 bundled skills ride inside the `tvt-config` plugin — no separate `skills:` block. |
| `rules` | team `CLAUDE.md` (static) | One entry pointing at the team `CLAUDE.md`. Per-developer templating (`{{FULL_NAME}}` …) is deferred to sub-project #5. |
| `tools` | permission allow/ask/deny | Three-tier `tools` channel, populated from the plugin `settings.json`. |

### Personal `ainfra.personal.yaml`

- 3 personal secrets (`op://Private/...`), `scope: personal`:
  `linear-personal-api-key`, `x-oauth`, `posthog-personal-api-key`.
- Optional personal `rules` entry (the personal section of `CLAUDE.md`).

### Deliberately absent

- The 5 cron jobs — no `scheduledJobs` field exists in the manifest schema;
  they cannot be expressed and belong to sub-project #6.
- Cursor / IDE mirroring — out of scope.

## Test + gap-report procedure

After both manifests are authored:

1. **`ainfra validate`** — expected to pass fully. `validate` is a static
   schema check; aspirational entries (1Password `gateway` secrets, git plugin
   sources, pip/composer `cliTools`) are all schema-valid. A failure here means
   a genuine manifest error — fix it. This is the real gate that the manifest
   is well-formed.
2. **`ainfra lock`** — expected to *partially* fail. Each entry that cannot
   resolve produces an error; capture the actual error text.
   - If `lock` aborts wholesale on the first error rather than collecting all
     of them, resolve iteratively: comment out the failing entry, re-run,
     record the error, repeat — yielding the full failure list either way.
   - Whatever resolves has its `ainfra.lock` committed.
3. **`ainfra plan`** — run against whatever locked, to confirm the diff output
   is sane for the resolvable subset.
4. **Gap report** — rewrite `ainfra/docs/assessment-vs-real-config.md` from
   paper assessment to evidence-based: each open-gap row carries the real
   command and error output, and is mapped to its sub-project (#2–#7). This
   becomes the concrete input spec for the next rounds.

**No `ainfra apply`** in this sub-project. With secret and plugin resolution
unbuilt, a full apply cannot run, and a partial apply mutating `~/.claude` is
not worth it here. `apply` is exercised once #2–#4 land.

## Success criteria

- `ainfra.yaml` and `ainfra.personal.yaml` exist in the `claude-config` repo
  and pass `ainfra validate`.
- The manifest declares every in-scope tvt-config surface (all rows in the
  mapping table); the only omissions are the documented ones (cron jobs,
  IDE mirroring).
- `ainfra lock` has been run; every non-resolving entry has its real error
  text recorded.
- `ainfra/docs/assessment-vs-real-config.md` is rewritten as an evidence-based
  gap report, each gap mapped to a sub-project.

## Out of scope for #1

Building any resolver — 1Password, remote fetch, cliTool adapters, per-dev
templating, scheduled jobs, credential-file writing. Those are sub-projects
#2–#7, each with its own spec → plan → implementation cycle.
