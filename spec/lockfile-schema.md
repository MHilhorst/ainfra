# Phase 2 — Lockfile Schema (`ainfra.lock`)

Status: **drafted.** The lockfile is the artifact that closes reproducibility
*and* drift detection. It is generated, never hand-edited, and committed.

---

## 1. Purpose

The manifest is *desired* state. The lockfile is *resolved* state — the
manifest after templates are instantiated, layers merged, and tool-owned fields
computed. Two jobs:

1. **Reproducibility.** A second developer running `ainfra apply` against the
   same manifest + lock gets the identical stack — including the *same*
   allocated ports, because they are recorded here, not re-derived.
2. **Drift / rug-pull detection.** Each entry carries a content hash. `ainfra
   check` recomputes hashes from the live environment; a mismatch is drift (an
   MCP server's schema changed, a skill's content changed, a file was edited).

---

## 2. Structure

```yaml
version: 1
generatedAt: 2026-05-21T09:30:00Z
manifestHash: sha256:<hex>      # hash of the merged, normalized manifest inputs
entries:
  mcpServers:        { <id>: <entry> }
  backgroundServices:{ <id>: <entry> }
  hooks:             { <id>: <entry> }
  commands:          { <id>: <entry> }
  skills:            { <id>: <entry> }
  plugins:           { <id>: <entry> }
  rules:             { <id>: <entry> }
  cliTools:          { <id>: <entry> }
```

`hooks` and `commands` (manifest §11–§12) record a `layer` and a `contentHash`
of the entry's declared config — so an edit to that config is caught by `check`
— and `commands` additionally records its pinned `version`. (Hashing a sourced
hook script's or command file's *contents* is a fast-follow, §6.) Both honour
the layered-lockfile split of §7: personal-layer hooks and commands go to
`ainfra.personal.lock`, never the committed file.

`manifestHash` lets `plan` answer "did the inputs change?" in O(1) before doing
per-entry work.

---

## 3. Entry shape

Common to every entry:

| Field | Meaning |
|-------|---------|
| `layer` | Which layer the winning definition came from (`team` / `repo` / `personal`). |
| `contentHash` | `sha256:` of the **normalized** resolved config (§5). |

Channel-specific additions:

```yaml
# mcpServers / backgroundServices — template-derived
mcpServers:
  analytics-db:
    layer: repo
    fromTemplate: mysql-over-ssh-tunnel
    resolved:
      tunnelPort: 13306                       # allocated once, reused forever
    version: "0.6.2"                          # pinned package version (manifest §5.1)
    integrity: sha256:5e4d…                   # hash of resolved package content
    toolsetHash: sha256:c2b1…                 # hash of advertised tool list, if reachable at lock time
    contentHash: sha256:1a2b…

# skills / plugins / rules — pinned to exact version + hash (strong)
skills:
  disruption-debugging:
    layer: team
    version: 1.4.0
    contentHash: sha256:9f8e…

# cliTools — best-effort (design §6 caveat)
cliTools:
  mysql-client:
    layer: repo
    constraint: ">=8.0"
    resolvedVersion: "8.4.0"                  # recorded, NOT pinned
    contentHash: sha256:7c6d…                 # hash of the install/check config, not the binary
```

For `cliTools`, `contentHash` covers the *declared* install/check config — not
the binary. `resolvedVersion` records what was actually installed. `check`
compares `resolvedVersion` against `constraint` and flags a mismatch, but cannot
promise a byte-identical binary. This weakness is acknowledged, not hidden.

For `mcpServers` launched from a package registry, `contentHash` alone is
insufficient: the launch string can be byte-identical while the package code
changes underneath it (validation Iteration 1). Three additional fields close
the gap:

- `version` — the exact pinned version from the manifest (§5.1 there).
- `integrity` — `sha256:` of the resolved package content (the tarball / module
  tree), so a tampered package of the same version fails `check`.
- `toolsetHash` — `sha256:` of the server's advertised tool list, captured when
  the server was reachable at `lock` time. Catches a behavioural change even
  within one version. Omitted (and `check` skips it) when the server could not
  be reached at lock time.

---

## 4. Allocated ports are sticky

When a template declares a `resolved` field of `kind: allocated-port`, `lock`
allocates a free port **once** and writes it under `resolved:`. Every later
`apply` reads the port from the lock instead of re-allocating. This is why a new
developer's tunnels land on the same ports as everyone else's, and why two
instances of the same template can never collide — the tool, holding the whole
set, allocates distinct ports across all of them.

---

## 5. Hashing is semantic, not textual

`contentHash` is computed over a **normalized** form of the resolved config, so
cosmetic differences are not false drift:

- map keys sorted; insignificant whitespace removed
- equivalent flag spellings folded (`-y` ⇔ `--yes`) per a per-channel normalizer
- secret *values* excluded — only the reference shape is hashed, so resolving a
  secret never registers as drift

The drift checker compares normalized hashes. Raw-text comparison is explicitly
wrong here and is not used.

---

## 6. v1 coverage

| Channel | v1 capability |
|---------|---------------|
| MCP servers | drift **detection** (hash compare) |
| Skills | drift **detection** (hash compare) |
| Background services | drift detection of the generated definition |
| Plugins, rules | pinning + hash recorded; detection is fast-follow |
| CLI tools | pinning + resolved version recorded; best-effort (§3) |

Full drift detection for MCP + Skills ships in v1; the rest record enough now to
turn detection on without a lockfile format change.

---

## 7. The lockfile is layered

> Added by Iteration 2 of the [validation gate](../docs/validation.md#scenario-4--a-personal-skill-then-promoted-to-the-team).

The lockfile is split to mirror the manifest's layers, so personal config never
lands in a committed file:

| Lockfile | Contains | Committed? |
|----------|----------|-----------|
| `ainfra.lock` | entries whose winning `layer` is `team` or `repo` | yes |
| `ainfra.personal.lock` | entries whose winning `layer` is `personal` | **no** — gitignored |

`ainfra lock` writes both. `ainfra apply` reads both. Personal entries still
get sticky allocated ports — recorded in the personal lock — so a developer's
own tunnels are as stable as the team's.

This makes promotion (validation scenario 4) automatic: when a personal entry is
moved into the team layer and committed, the next `lock` run finds its winning
layer is now `team`, drops it from `ainfra.personal.lock`, and writes it into
`ainfra.lock`. Its `contentHash` is unchanged, so teammates see a clean
*addition*, not a churn.
