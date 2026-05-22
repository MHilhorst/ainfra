# ainfra Subscriber Mode — Design

Bringing a team's AI tooling to non-engineers (sales, support, operations) who
use the **Claude Desktop app**, without giving them a git repo, an
`ainfra.yaml`, or a terminal.

## 0. Problem

ainfra today knows two roles:

- **Author** — an engineer who writes `ainfra.yaml` and runs `ainfra lock`.
- **Developer-consumer** — an engineer who runs `ainfra plan` / `apply` /
  `check` against a checked-out repo.

Both assume a git repo, a manifest on disk, and a person comfortable in a
terminal. A salesperson has none of these — but the team still wants them
running the same MCP servers in their native Claude Desktop app.

This design adds a third role: the **subscriber** — a repo-less, terminal-less
consumer whose machine subscribes to a published artifact and silently keeps
itself in sync.

## 1. Decisions (settled in brainstorming)

| Question | Decision |
|----------|----------|
| Subscriber touchpoint | One-time friendly installer, then silent updates. |
| Distribution | A **published release artifact** — engineer runs `ainfra publish`, team hosts the artifact at a URL. |
| Curation | None. Subscribers get the full set. Per-role policy / access management is a later phase. |
| Render target | New `claude-desktop` agent, **MCP servers only** for now. |
| Update trigger | A generated OS scheduled job (launchd / Task Scheduler), `RunAtLoad` + interval. |
| Configuration | The team configures interval, run-at-login, and artifact URL in the manifest; baked into the artifact at publish time. The subscriber configures nothing. |
| Secrets | Out of scope for v1 — subscribers get no-credential or in-app-OAuth servers only. |

Two principles carry over unchanged and constrain everything below:

- **ainfra owns no runtime and no hosting.** `publish` produces an artifact;
  the team uploads it wherever it likes. The scheduled job runs `ainfra` and
  exits — it is a generated service definition (design §7), not a daemon.
- **Stop using ainfra and the files it wrote keep working.** The subscriber's
  `claude_desktop_config.json` is a normal file; ainfra merely keeps the keys
  it owns reconciled.

## 2. The `claude-desktop` render target

Extend the agent registry (`internal/agent/agent.go`), currently
`claude-code | codex`, with a third agent `claude-desktop`. Its capability set
contains exactly one channel: `mcpServers`. Every other channel
(`hooks`, `commands`, `rules`, `skills`, `plugins`, `tools`, `cliTools`) is
absent — `agent.Supports` already gates on this map, so a manifest entry
targeting `claude-desktop` with an unsupported channel fails validation with
the existing actionable error. No new gating mechanism.

A new provider package `internal/provider/claudedesktop` ships one provider,
`MCP`, modelled on `codex.MCP`:

- **Target file** — `claude_desktop_config.json`, top-level `mcpServers` key:
  - macOS: `~/Library/Application Support/Claude/claude_desktop_config.json`
  - Windows: `%APPDATA%\Claude\claude_desktop_config.json`
  - Path resolution lives in the provider, keyed off `runtime.GOOS`; `env.Home`
    supplies the base.
- **Server shape** — Claude Desktop's `mcpServers` entry is the same
  `command` / `args` / `env` shape as `.mcp.json` for stdio servers. Reuse the
  `buildMCPServerObject` logic (stdio fields). Remote-transport servers (`url`,
  `headers`) render as Claude Desktop supports them; if a server's transport is
  unsupported by the installed app version, that is a runtime concern, not a
  reconciliation concern.
- **Merge, never clobber** — write via `fsmerge.MergeJSONKeys` against the
  `mcpServers` object, owning only the keys ainfra wrote (tracked in the
  ledger). A subscriber's hand-added servers survive every sync.

`agentset.ForAgent` gains a `case agent.ClaudeDesktop` returning
`[]provider.Provider{claudedesktop.MCP{}}` plus the shared providers. (CLI
tools remain shared; in practice a `claude-desktop` manifest declares none.)

## 3. The published artifact (`ainfra publish`)

A new author-side command. Precondition: a current `ainfra.lock` exists
(`publish` does not re-resolve; run `ainfra lock` first, mirroring how `plan`
consumes the lockfile).

`ainfra publish [--out <dir>]` writes a self-contained artifact directory:

```
artifact/
  ainfra.lock              copy of the resolved lockfile
  ainfra.sub.json          subscription descriptor (see below)
  bundles/                 materialized skill/plugin bundles, if any
  MANIFEST.sha256          content hash of every file above
```

For an MCP-only `claude-desktop` artifact, `bundles/` is typically empty — but
the layout is the same for any agent, so `publish` is not desktop-specific.

The **subscription descriptor** `ainfra.sub.json`:

```json
{
  "schemaVersion": 1,
  "artifactURL": "https://...",
  "agent": "claude-desktop",
  "sync": { "intervalMinutes": 360, "runAtLogin": true }
}
```

Its fields come from a new optional top-level `publish:` block in `ainfra.yaml`
(see §5). `publish` fails with an actionable error if `publish.artifactURL` is
unset — a descriptor with nowhere to fetch from is useless.

`publish` does **not** upload. The team runs `gh release upload`, `aws s3 cp`,
or copies the directory to a static host. ainfra owns no hosting.

## 4. Subscriber mode (`apply --from` / `check --from`)

`apply` and `check` gain a `--from <url-or-path>` flag. With `--from`:

- No manifest, no layering, no precedence. The artifact's `ainfra.lock` **is**
  already-resolved state.
- Pipeline: **fetch artifact → verify `MANIFEST.sha256` → load `ainfra.lock` →
  render via the artifact's `agent` → record in ledger.**
- The resolution engine is skipped entirely; the provider / diff / ledger
  engine downstream of the lockfile is reused unchanged.

Fetching: extend `internal/provider/fetch` with an HTTP(S) fetcher so
`--from https://...` works (today `LocalFetcher` rejects remote schemes).
`--from ./path` keeps using the local fetcher — this is also how the installer
and tests exercise subscriber mode offline.

**Failure is a safe no-op.** `apply` is reconciliation against a hash-pinned
artifact. A failed fetch, a hash mismatch, or a network outage changes nothing
— the machine stays on last-known-good config and the ledger records the
failed attempt. There is no partial or corrupt state.

`reconcile.go` grows a second path: `providersForArtifact(artifact)` resolves
the agent from the descriptor instead of from manifest layers, and
`buildEnv` for subscriber mode roots at the subscriber's home rather than a
repo (`claude-desktop` writes only absolute home-anchored paths anyway).

## 5. Configuration — the `publish:` block

A new optional top-level block in `ainfra.yaml`. The team owns every knob; the
subscriber owns none.

```yaml
publish:
  artifactURL: "https://downloads.acme.com/ainfra/sales/latest"
  agent: claude-desktop
  sync:
    intervalMinutes: 360      # default 360 (6h)
    runAtLogin: true          # default true
```

Schema, validation, and JSON Schema output (`internal/schema`) all extend to
cover it. `agent` here selects which agent the *artifact* renders for, and may
differ from the repo's own `agent:` — a team developing in `claude-code` can
publish a `claude-desktop` artifact for sales.

## 6. The one-time installer (`ainfra installer`)

`ainfra installer [--out <path>]` is an author-side command that emits a
ready-to-send installer for the artifact's target OS family. v1 ships a
**signed-friendly shell script** (`.command` on macOS so it is double-clickable
from Finder); a `.pkg` and a Windows `.ps1` are follow-ups behind the same
command.

The emitted installer, run once by the subscriber, does four things:

1. Installs the `ainfra` binary (downloads the release binary for the host
   OS/arch into `~/.ainfra/bin`).
2. Writes the subscription descriptor to `~/.ainfra/ainfra.sub.json`.
3. Generates and loads the scheduled job from the descriptor's `sync` block.
4. Runs the first `ainfra apply --from <artifactURL>`.

The **scheduled job** is a generated service definition — the same category as
the SSH-tunnel service definitions ainfra already generates (design §7), here
pointed at ainfra itself:

- macOS: a launchd `LaunchAgent` plist in `~/Library/LaunchAgents`, with
  `RunAtLoad` and `StartInterval` from the descriptor, invoking
  `ainfra apply --from <url>`.
- Windows: a Task Scheduler task (follow-up).

ainfra generates and loads the job; it does not supervise it. The launchd
plist generation can reuse machinery from the reverted scheduled-jobs work
(`docs/superpowers/specs/2026-05-21-scheduled-jobs-design.md`).

## 7. Scope — what v1 builds vs. defers

**Builds:**

- `claude-desktop` agent + `claudedesktop.MCP` provider + `agentset` wiring.
- `publish:` manifest block — schema, validation, JSON Schema.
- `ainfra publish` — artifact directory + descriptor + `MANIFEST.sha256`.
- `apply --from` / `check --from` — subscriber pipeline; HTTP fetcher.
- `ainfra installer` — macOS `.command` script + launchd plist generation.

**Defers (named, not silently dropped):**

- **Secrets for subscribers.** No `direct`-literal token paste, no brokered
  mode on a subscriber machine. v1 artifacts carry only no-credential or
  in-app-OAuth servers. Revisit with the access-management phase.
- **Per-role curation.** Every subscriber gets the full published set. Profiles
  / roles wait until policy work begins.
- **Windows `.ps1` installer and `.pkg`.** macOS `.command` first; same command
  surface.
- **Artifact signing.** v1 verifies content via `MANIFEST.sha256` (integrity).
  Cryptographic signing (authenticity) is a later hardening step.
- **Upload / hosting.** Never in scope — `publish` writes a directory; the team
  hosts it.

## 8. Testing

- `claudedesktop.MCP` — observe/apply unit tests against an in-memory FS,
  mirroring `codex` provider tests, including the merge-preserves-foreign-keys
  case and per-OS path resolution.
- `publish` — golden test on the emitted artifact layout and descriptor;
  `MANIFEST.sha256` correctness; missing-`artifactURL` error.
- `apply --from` — e2e against a local artifact directory: fetch → hash-verify
  → render `claude_desktop_config.json`; idempotent re-apply; tampered-hash
  rejection; failed-fetch is a no-op leaving prior config intact.
- `installer` — golden test on the emitted script and launchd plist for a
  given descriptor.
- Validation — `publish:` block schema acceptance/rejection cases.

## 9. Data flow

```
AUTHOR (engineer, in repo)
  ainfra.yaml ──lock──▶ ainfra.lock ──publish──▶ artifact/ (lock + descriptor + bundles + MANIFEST.sha256)
                                                      │
                                          team uploads to artifactURL
                                                      │
  ainfra installer ──▶ one-time installer ──sent to──▶ SUBSCRIBER

SUBSCRIBER (salesperson machine, no repo)
  double-click installer ──▶ ainfra binary + descriptor + scheduled job + first apply
                                                      │
  scheduled job (RunAtLoad + interval) ──▶ ainfra apply --from <artifactURL>
       fetch artifact ──▶ verify MANIFEST.sha256 ──▶ render claude_desktop_config.json ──▶ ledger
```
