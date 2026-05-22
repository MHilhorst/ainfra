# Validation Gate

Per design §11, the Phase 1 + 2 schemas are run *on paper* against five
scenarios **before** implementation code is written. Where a scenario bends the
schema, the schema is iterated — and this document records the iteration.

**Result: gate passed after 2 schema iterations.** Both iterations are applied
to [spec/manifest-schema.md](../spec/manifest-schema.md) and
[spec/lockfile-schema.md](../spec/lockfile-schema.md), and described below.

---

## Scenario 1 — A new developer onboards

**Walk.** Dev clones the repo (`ainfra.yaml` + `ainfra.lock` committed),
runs `ainfra apply`. The tool: loads the repo layer, fetches the team layer
named by `extends:`, merges under the Option-C precedence table, builds the
dependency graph, walks it leaves-first — installs `cliTools`, verifies
`preconditions`, generates + starts `backgroundServices`, then writes the six
channels' config. Allocated ports come from the committed lock, so the new dev's
tunnels match everyone else's.

**Holds.** One note, no schema change: the secret-manager CLI (`op`, `doppler`)
is itself just a `cliTool` entry, so bootstrapping it is already expressible. If
the dev is not authenticated to the vault, secret resolution fails with the
resolver's own error — correct and loud.

---

## Scenario 2 — A skill version bumps

**Walk.** A maintainer changes `skills.disruption-debugging.version` from
`1.4.0` to `1.5.0`. The merged manifest changes, so `manifestHash` changes.
`ainfra plan` re-resolves the skill, fetches `1.5.0`, computes its new
`contentHash`, and renders a diff: `~ skills.disruption-debugging  1.4.0 →
1.5.0`. `ainfra apply` installs it and rewrites that lock entry.

**Holds.** No schema change. Skills pin an exact version and the lock carries a
content hash — the diff is exact.

---

## Scenario 3 — An MCP server's schema silently changes

**Walk.** A server is launched as `npx -y @vendor/server@latest`. The vendor
ships new code; the *launch config* (`command`, `args`, `env`) is byte-identical,
so a `contentHash` taken over launch config alone **does not change**. `check`
would stay green while the server's behaviour changed underneath it.

**The schema bent.** This is exactly the rug-pull the tool exists to catch
(design §0), and the first-draft schema missed it.

**Iteration 1 — applied:**

1. *Manifest schema.* An MCP server (or a template-produced `mcpServer`) whose
   `command`/`args` launch from a package registry **must** pin an exact
   `version`. `@latest` and floating ranges are a validation error. Added to
   [manifest-schema.md §5](../spec/manifest-schema.md#5-channel-1--mcp-servers).
2. *Lockfile schema.* Each MCP entry records `integrity:` — a hash of the
   *resolved package content*, not the launch string — and, when the server was
   reachable at `lock` time, `toolsetHash:` — a hash of the server's advertised
   tool list. `check` recomputes both. A changed package or a changed advertised
   toolset now fails loudly. Added to
   [lockfile-schema.md §3](../spec/lockfile-schema.md#3-entry-shape).

After the iteration: a pinned version makes registry content immutable, the
`integrity` hash catches a tampered package, and `toolsetHash` catches a
behavioural change even within a version. `check` fails loudly. **Holds.**

---

## Scenario 4 — A personal skill, then promoted to the team

**Walk.** A dev adds a skill to `ainfra.personal.yaml`. Its id exists in no
other layer, so it is simply *added* — no precedence conflict. Later they
promote it: move the block verbatim into the team layer and commit. For them,
resolved state is unchanged (only the `layer` field moves). For teammates,
`plan` shows one added entry.

**The schema bent.** The lockfile is committed. If `ainfra lock` had written
the personal skill's resolved entry into `ainfra.lock`, it would leak
personal-layer config into a committed file — and churn it for every teammate.

**Iteration 2 — applied:** the lockfile is layered like the manifest. Committed
`ainfra.lock` carries **only `team` + `repo`** entries. A gitignored
`ainfra.personal.lock` carries `personal` entries (and gives them sticky ports
too). Promotion then does the right thing automatically: the entry moves from
the personal lock to the committed lock on the next `lock`. Added to
[lockfile-schema.md §7](../spec/lockfile-schema.md#7-the-lockfile-is-layered).

After the iteration: personal config never touches a committed file; promotion
is a clean move with no content-hash change. **Holds.**

---

## Scenario 5 — The multi-database scenario

**Walk.** See [examples/multi-database/](../examples/multi-database/). One
`mysql-over-ssh-tunnel` template; four instances (`analytics-db`, `billing-db`,
`catalog-db`, `reporting-db`) differing only in `host` / `database` / `sshUser`
and a password `ref`. Each instance's `produces.backgroundService` is namespaced
`${instance.id}-tunnel`, so the four tunnels are distinct by construction. The
`requires` chain — `mcpServer → service → cliTool ssh + cliTool mysql-client →
precondition vpn-tvt-internal` — is fully declared. `tunnelPort` is a `resolved`
field of `kind: allocated-port`: `ainfra lock`, holding the whole set, hands
out `13306..13309`; no human types a port, and collision is structurally
impossible. `ainfra check` walks the graph and, if the VPN is down, fails on
the `vpn-tvt-internal` precondition with its `remediation` text.

**Holds, no schema change.** Human-declared intent for all four databases is
~20 lines; it replaces a ~200-line runbook. The template / instance /
tool-owned-resolved separation (design §8) carries the weight.

---

## Outcome

| Scenario | Result |
|----------|--------|
| 1 — Onboard | Holds |
| 2 — Skill bump | Holds |
| 3 — Silent MCP change | Holds **after Iteration 1** |
| 4 — Personal → team | Holds **after Iteration 2** |
| 5 — Multi-database | Holds |

The gate is passed. Both iterations were found cheaply, on paper, exactly as
§11 intends — and are reflected in the specs. Implementation proceeded against
the iterated schema; all five build phases (design §10) are now complete. The
plan that drove it is
[docs/superpowers/plans/2026-05-21-ainfra-cli.md](superpowers/plans/2026-05-21-ainfra-cli.md).
