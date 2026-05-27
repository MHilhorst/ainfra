# Validation Gate

Per design §11, the Phase 1 + 2 schemas are tested *on paper* against five
scenarios **before** any implementation code is written. If a scenario reveals
a gap in the schema, the schema is changed to fix it — and this document
records each change.

**Result: the gate passed after 2 schema iterations** (two rounds of changes).
Both are applied to [spec/manifest-schema.md](../spec/manifest-schema.md) and
[spec/lockfile-schema.md](../spec/lockfile-schema.md), and described below.

---

## Scenario 1 — A new developer onboards

**Walk.** A developer clones the repo (the manifest — `ainfra.yaml`, the file
describing the team's setup — and the lockfile — `ainfra.lock`, the
auto-generated file that pins exact versions — are both committed) and runs
`ainfra install`. The tool then:

- loads the repo layer (a layer is one source of config that gets merged with
  others);
- fetches the team layer named by `extends:`;
- merges them under the Option-C precedence table (the rules deciding which
  layer wins on conflict);
- builds the dependency graph;
- walks it leaves-first — installs `cliTools`, verifies `preconditions`
  (checks that must pass before a step runs), generates and starts
  `backgroundServices`, then writes the six channels' config (a channel is one
  category of AI-tooling config — MCP servers, hooks, and so on).

Allocated ports come from the committed lockfile, so the new developer's
tunnels match everyone else's.

**Holds.** One note, no schema change: the secret-manager CLI (`op`, `doppler`)
is itself just a `cliTool` entry, so installing it as part of bootstrap is
already expressible. If the developer is not authenticated to the vault, secret
resolution fails with the resolver's own error — which is correct and loud.

---

## Scenario 2 — A skill version bumps

**Walk.** A maintainer changes `skills.disruption-debugging.version` from
`1.4.0` to `1.5.0`. The merged manifest changes, so `manifestHash` changes too.
`ainfra install --dry-run` re-resolves the skill, fetches `1.5.0`, computes its new
`contentHash`, and renders a diff: `~ skills.disruption-debugging  1.4.0 →
1.5.0`. `ainfra install` installs it and rewrites that lockfile entry.

**Holds.** No schema change. Skills pin an exact version and the lockfile
carries a content hash, so the diff is exact.

---

## Scenario 3 — An MCP server's schema silently changes

**Walk.** A server is launched as `npx -y @vendor/server@latest`. The vendor
ships new code, but the *launch config* (`command`, `args`, `env`) is
byte-identical. A `contentHash` taken over the launch config alone therefore
**does not change**, so `check` would stay green while the server's behaviour
changed underneath it.

**The schema bent.** This is exactly the rug-pull (a dependency changing
silently upstream after you adopted it) the tool exists to catch (design §0),
and the first-draft schema missed it.

**Iteration 1 — applied:**

1. *Manifest schema.* An MCP server (or a template-produced `mcpServer`) whose
   `command`/`args` launch from a package registry **must** pin an exact
   `version`. `@latest` and floating ranges are a validation error. Added to
   [manifest-schema.md §5](../spec/manifest-schema.md#5-channel-1--mcp-servers).
2. *Lockfile schema.* Each MCP entry records two things. First, `integrity:` —
   a hash of the *resolved package content*, not of the launch string. Second,
   when the server was reachable at `lock` time, `toolsetHash:` — a hash of the
   server's advertised tool list. `check` recomputes both, so a changed package
   or a changed advertised toolset now fails loudly. Added to
   [lockfile-schema.md §3](../spec/lockfile-schema.md#3-entry-shape).

After the iteration: a pinned version makes the registry content immutable, the
`integrity` hash catches a tampered package, and `toolsetHash` catches a
behavioural change even within the same version. `check` fails loudly.
**Holds.**

---

## Scenario 4 — A personal skill, then promoted to the team

**Walk.** A developer adds a skill to `ainfra.personal.yaml`. Its id exists in
no other layer, so it is simply *added* — there is no precedence conflict.
Later they promote it: they move the block verbatim into the team layer and
commit it. For them, the resolved state is unchanged (only the `layer` field
moves). For teammates, `plan` shows one added entry.

**The schema bent.** The lockfile is committed. If `ainfra install` (which re-locks) had written
the personal skill's resolved entry into `ainfra.lock`, it would leak
personal-layer config into a committed file — and create churn in that file for
every teammate.

**Iteration 2 — applied:** the lockfile is layered the same way the manifest
is. The committed `ainfra.lock` carries **only `team` + `repo`** entries. A
gitignored `ainfra.personal.lock` carries `personal` entries (and gives them
sticky ports too). Promotion then does the right thing automatically: on the
next `lock`, the entry moves from the personal lockfile to the committed
lockfile. Added to
[lockfile-schema.md §7](../spec/lockfile-schema.md#7-the-lockfile-is-layered).

After the iteration: personal config never touches a committed file, and
promotion is a clean move with no content-hash change. **Holds.**

---

## Scenario 5 — The multi-database scenario

**Walk.** See [examples/multi-database/](../examples/multi-database/). There is
one `mysql-over-ssh-tunnel` template (a reusable config blueprint) and four
instances of it (`analytics-db`, `billing-db`, `catalog-db`, `reporting-db`) —
an instance is one concrete use of a template. The four instances differ only
in `host` / `database` / `sshUser` and a password `ref`. Each instance's
`produces.backgroundService` is namespaced `${instance.id}-tunnel`, so the four
tunnels are distinct by construction.

The `requires` chain — `mcpServer → service → cliTool ssh + cliTool
mysql-client → precondition vpn-tvt-internal` — is fully declared. `tunnelPort`
is a `resolved` field of `kind: allocated-port`: `ainfra install` (which re-locks), which holds the
whole set, hands out `13306..13309`. No human types a port, so a collision is
structurally impossible. `ainfra install --dry-run --strict` walks the graph and, if the VPN is
down, fails on the `vpn-tvt-internal` precondition and prints its `remediation`
text.

**Holds, no schema change.** The human-declared intent for all four databases
is about 20 lines, and it replaces a roughly 200-line runbook. The
template / instance / tool-owned-resolved separation (design §8) carries the
weight.

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
§11 intends — and both are reflected in the specs. Implementation then
proceeded against the iterated schema; all five build phases (design §10) are
now complete. The plan that drove it is
[docs/superpowers/plans/2026-05-21-ainfra-cli.md](superpowers/plans/2026-05-21-ainfra-cli.md).
