# Phase 2 — Lockfile Schema (`ainfra.lock`)

Status: **implemented.** The lockfile — `ainfra.lock`, the auto-generated file
that pins exact versions — is what makes a setup reproducible *and* lets the
tool detect drift (config quietly falling out of sync with what was declared).
`ainfra install` generates it (or the hidden `ainfra lock` verb directly). You never hand-edit it, and you commit it to the
repo.

---

## 1. Purpose

The manifest — `ainfra.yaml`, the file describing the team's setup — holds the
*desired state* (what you want). The lockfile holds the *resolved state*: the
manifest after templates are instantiated, layers merged, and tool-owned fields
computed. It has two jobs:

1. **Reproducibility.** A second developer running `ainfra install` against the
   same manifest + lock gets the identical stack — including the *same*
   allocated ports, because they are recorded here, not re-derived.
2. **Drift / rug-pull detection.** A rug-pull is a dependency changing silently
   upstream after you adopted it. Each entry carries a content hash.
   `ainfra install --dry-run --strict` recomputes hashes from the live
   environment; a mismatch means drift
   (an MCP server's schema changed, a skill's content changed, a file was
   edited).

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

`hooks` and `commands` (manifest §11–§12) each record a `layer` and a
`contentHash` of the entry's declared config — so `check` catches any edit to
that config — and `commands` additionally records its pinned `version`.
(Hashing the *contents* of a sourced hook script or command file is a
fast-follow; see §6.) Both follow the layered-lockfile split of §7:
personal-layer hooks and commands go to `ainfra.personal.lock`, never the
committed file.

`manifestHash` lets `plan` answer "did the inputs change?" in O(1) (constant
time — one hash comparison) before doing per-entry work.

---

## 3. Entry shape

Common to every entry:

| Field | Meaning |
|-------|---------|
| `layer` | Which layer the winning definition came from (`team` / `repo` / `personal`). A layer is one tier of config; when several layers define the same entry, one wins. |
| `contentHash` | `sha256:` of the **normalized** resolved config (§5). Normalized means reduced to a canonical form so cosmetic differences do not matter. |

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
    toolsetHash: sha256:c2b1…                 # hash of advertised tool list, if reachable at lock time (populated since v1)
    command: npx                              # resolved launch command, replayed at check time
    args: ["-y", "@vendor/server", "--flag"]  # resolved launch args, replayed at check time
    env:                                      # resolved env (secret values excluded)
      LOG_LEVEL: info
    lockedTools:                              # per-tool fingerprint, populated since v1
      - name: query
        descriptionHash: sha256:aa11…
        inputSchemaHash: sha256:bb22…
      - name: list_tables
        descriptionHash: sha256:cc33…
        inputSchemaHash: sha256:dd44…
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
the binary itself. `resolvedVersion` records the version that was actually
installed. `check` compares `resolvedVersion` against `constraint` and flags a
mismatch, but it cannot promise a byte-identical binary. This weakness is
acknowledged, not hidden.

For `mcpServers` launched from a package registry, `contentHash` alone is not
enough: the launch string can be byte-identical while the package code changes
underneath it (validation Iteration 1). Three more fields close that gap:

- `version` — the exact pinned version from the manifest (§5.1 there).
- `integrity` — `sha256:` of the resolved package content (the tarball or
  module tree). This field makes a tampered package of the same version fail
  `check`.
- `toolsetHash` — `sha256:` of the server's advertised tool list, captured when
  the server was reachable at `lock` time. It catches a behavioural change even
  within one version. It is omitted (and `check` skips it) when the server
  could not be reached at lock time. Populated since v1 by the MCP stdio
  introspection client; older lockfiles read with it empty fall back to a
  "re-run `ainfra lock`" nudge.
- `lockedTools` — additive since v1, populated alongside `toolsetHash`. Each
  entry has a `name` plus a `descriptionHash` and `inputSchemaHash`. The list
  is what lets `ainfra check` identify *which* tool changed by name when the
  toolset hash drifts, rather than only reporting that the aggregate hash
  moved. Optional: lockfiles written before v1 read with the field empty, and
  `check` falls back to a single-line aggregate-drift report in that case.
- `command`, `args`, `env` — additive since v1. They record the resolved
  launch invocation so `ainfra check` can replay the same subprocess at
  drift-detection time without re-resolving the manifest. Secret *values* are
  excluded from `env` for the same reason `contentHash` excludes them. These
  fields are optional; older lockfiles read with them empty and skip toolset
  drift detection.

---

## 4. Allocated ports are sticky

A template is a reusable config blueprint. When a template declares a
`resolved` field of `kind: allocated-port`, `lock` allocates a free port
**once** and writes it under `resolved:`. Every later `apply` reads the port
from the lock instead of allocating a new one. This is why a new developer's
tunnels land on the same ports as everyone else's, and why two instances (two
separate uses of the same template) can never collide — the tool, which holds
the whole set, allocates distinct ports across all of them.

---

## 5. Hashing is semantic, not textual

`contentHash` is computed over a **normalized** form of the resolved config, so
cosmetic differences do not show up as false drift:

- map keys sorted; insignificant whitespace removed
- equivalent flag spellings folded together (`-y` ⇔ `--yes`) by a per-channel
  normalizer — code that reduces config to its canonical form, with one
  normalizer per channel (a channel is one category of AI-tooling config — MCP
  servers, hooks, and so on)
- secret *values* excluded — only the reference shape is hashed, so resolving a
  secret never registers as drift

The drift checker compares these normalized hashes. Comparing raw text is
explicitly wrong here and is not used.

---

## 6. v1 coverage

| Channel | v1 capability |
|---------|---------------|
| MCP servers | drift **detection** (hash compare) |
| Skills | drift **detection** (hash compare) |
| Background services | drift detection of the generated definition |
| Plugins, rules | pinning + hash recorded; detection is fast-follow |
| CLI tools | pinning + resolved version recorded; best-effort (§3) |

Full drift detection for MCP servers and Skills ships in v1. The rest record
enough information now to turn detection on later without changing the lockfile
format.

---

## 7. The lockfile is layered

> Added by Iteration 2 of the [validation gate](../docs/reference/validation.md#scenario-4--a-personal-skill-then-promoted-to-the-team).

The lockfile is split to mirror the manifest's layers, so personal config never
lands in a committed file:

| Lockfile | Contains | Committed? |
|----------|----------|-----------|
| `ainfra.lock` | entries whose winning `layer` is `team` or `repo` | yes |
| `ainfra.personal.lock` | entries whose winning `layer` is `personal` | **no** — gitignored |

`ainfra install` writes both files (re-locking when the manifest is newer)
and reads both during reconcile. Personal entries
still get sticky allocated ports — recorded in the personal lock — so a
developer's own tunnels are as stable as the team's.

This makes promotion (validation scenario 4) automatic. When a personal entry
is moved into the team layer and committed, the next `lock` run finds that its
winning layer is now `team`. It drops the entry from `ainfra.personal.lock` and
writes it into `ainfra.lock`. Its `contentHash` does not change, so teammates
see a clean *addition*, not churn.
