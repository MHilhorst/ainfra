# ainfra ownership boundaries — own, declare, or delegate

Status: **design — approved direction, implementation pending.**

This spec comes out of a review of the real `tvt-config` setup. Two findings
were fixed immediately (see Status below); four are design-level and captured
here. They share one root cause, so they belong in one document.

---

## 0. The root principle

ainfra is a desired-state reconciler. That model only works for a resource
ainfra can **create, observe, and destroy — reproducibly**. Run every channel
through that test and two categories fall out:

- **Owned configuration** — exists *because* ainfra writes it, meaningless
  outside the Claude Code setup, single-owner. `.mcp.json`, `CLAUDE.md`, hooks,
  permissions, secret references. ainfra should fully own these.
- **Required substrate** — exists independently in the wider system, shared,
  with a lifecycle owned by someone else (Homebrew, the OS, a VPN). CLI tools,
  SSH tunnels. ainfra **depends on** these; it must not pretend to own them.

Every finding below is the same mistake: a piece of substrate modelled as owned
configuration. The fix is always to move it to **declare + verify + delegate**,
never **own + reconcile**.

A second principle, for reproducibility: ainfra today ships *recipes*
(`apply` re-resolves every run). Reproducibility only ever comes from
*artifacts* (git, Docker, lockfiles). That is finding #6.

---

## 1. MCP servers — single source of truth (findings #1, #5)

**Problem.** Two files describe the MCP servers: the manifest's `mcpServers:`
block and the `tvt-config` plugin's `.mcp.json` (the file Claude actually
loads). They diverge silently — the manifest pinned versions while `.mcp.json`
ran `@latest`; the manifest models `flare` as native `transport: http` while
`.mcp.json` runs it via an `npx mcp-remote` bridge. Worse, `ainfra apply`
renders its *own* `.mcp.json` into the repo working tree, overwriting the
committed plugin file (finding #5).

**Decision.** One source of truth. The plugin's `.mcp.json` is what runs, so it
wins. Two viable shapes:

- **(a) ainfra renders the plugin's `.mcp.json`** — the manifest becomes the
  source; `apply` writes to the plugin's path; the pins become real. Requires
  reconciling the transport differences (`mcp-remote` bridge vs native http).
- **(b) Delete the manifest `mcpServers:` block** — the plugin owns its
  `.mcp.json` outright; ainfra stops describing MCP servers.

Recommend **(a)**: it keeps a single declarative manifest and makes pinning
real. Until it lands, `.mcp.json` is hand-maintained — and is now pinned
(Status below).

**Also (#5):** `apply` must never write into the repo working tree. Its
rendered output belongs under `~/.claude/` or an explicit build dir.

---

## 2. CLI tools are substrate, not resources (finding #2)

**Problem.** `cliTools` is reconciled like `mcpServers`, but ainfra can't own a
`brew` package: it never uninstalls (Delete is a deliberate no-op), can't truly
pin a version (brew gives "latest"), mutates global machine state, and can't be
tested in isolation. ainfra reimplements brew/npm/composer via adapters — and
`tipctl` already fell out because its install doesn't fit the adapter shape.

**Decision.** `cliTools` always **declares + checks** (ainfra is good at this).
Installation becomes a per-tool *strategy* the manifest names — ainfra
orchestrates, never reimplements:

```yaml
cliTools:
  mysql-client:         { install: { via: brewfile }, check: {...} }
  google-analytics-cli: { install: { via: run, run: "npm i -g google-analytics-cli@1.1.1" }, check: {...} }
  tipctl:               { install: { via: manual, docs: "skills/using-transip-cli" }, check: {...} }
```

- `via: brewfile` → ainfra renders one `Brewfile`; `apply` delegates to
  `brew bundle` (covers the brew formula/cask/tap majority; gains a real
  uninstall via `brew bundle cleanup` and an isolated check via `brew bundle
  check`).
- `via: run` → ainfra runs one opaque command (the npm/uv long tail).
- `via: manual` → ainfra only checks and points at docs (the genuinely weird
  ones, e.g. `tipctl`).

Drop the bespoke brew/npm/composer adapters. The honest ceiling: ~90% of an
arbitrary tool set becomes clean; the last 10% stays bespoke and that is fine.

---

## 3. SSH tunnels — declare, don't pretend to run (finding #3)

**Problem.** The `mysql-over-ssh-tunnel` template emits a `backgroundService`
per tunnel, but ainfra's services provider only writes `start.sh`/`stop.sh` — it
never loads a launchd agent. The tunnels are really run by
`scripts/install-tunnels.sh`. The template also declares ports (13306/13307)
that do not match the live ones (3307/3308). The manifest describes a tunnel
system ainfra does not operate.

**Decision.** Stop pretending. Either ainfra genuinely manages the launchd
agents (real lifecycle: load/unload, the proper fix), or — the honest minimal
move — the tunnels become a **`precondition`** (`type: port-listening` on
3307/3308), `install-tunnels.sh` stays their owner, and the template drops the
`backgroundService`. Recommend the precondition route until launchd lifecycle
is genuinely built; it makes the manifest truthful today.

---

## 4. Recipe → artifact (finding #6)

**Problem.** `ainfra apply` is a recipe — it re-resolves on every run.
`ainfra.lock` pins manifest *content hashes*, not delivered *bytes*: `npx
@latest` and `brew` latest re-resolve regardless. So "reproducible across the
team" is not actually true.

**Decision (direction, not a quick change).** Reproducibility comes only from
artifacts. The agent environment should become a **pinned, content-addressed
artifact a teammate receives**, not a recipe they re-run. Concretely: extend
the lockfile to pin resolved bytes (exact npm versions + integrity hashes,
brew bottle revisions), and grow the existing `publish`/subscriber-mode concept
into a real artifact a subscriber applies verbatim. This is a project of its
own and needs its own plan.

---

## Status

Fixed during the review (already on `main`):

- **`ainfra check` runs preconditions**, and the `dns-resolves` check type is
  implemented via `net.LookupHost` — the VPN-offline signal is restored
  (`ainfra` `d0bf9d1`).
- **`.mcp.json` MCP servers pinned** to the manifest versions — interim fix for
  #1 until the source-of-truth decision lands (`claude-config` `06914af`).

Pending: #1 (source-of-truth (a)), #2 (cliTools strategy model), #3 (tunnels →
precondition), #5 (`apply` stops writing the repo), #6 (artifact model — own
plan).

---

## Out of scope

Scheduled jobs (`cron-jobs.json` + `manage-crons.sh`) remain outside ainfra by
explicit team decision.
