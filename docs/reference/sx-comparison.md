# ainfra vs sx: positioning and adopted ideas

This document records the team's deep read of [`sleuth-io/sx`](https://github.com/sleuth-io/sx) — ainfra's
closest neighbour, a Go CLI that keeps a team's AI tooling (skills, rules,
agents, commands, hooks, MCP, plugins) in sync — and the design decisions that
came out of it. It is kept in-repo so the reasoning behind what ainfra adopted,
adapted, and rejected survives the conversation that produced it.

## The core architectural difference

| | ainfra | sx |
|---|---|---|
| Shape | Package manager — `install`/`add`/`remove`/`update`/`list`/`outdated` over a content-hashed lockfile. The Terraform-shaped verbs (`plan`/`apply`/`check`) are retained as hidden aliases through 0.x. (see `docs/plans/2026-05-27-002-feat-package-manager-cli-plan.md`) | Package manager — `sx add` / `sx install`, manifest + lock (npm/cargo/uv lineage) |
| Where config lives | **In the repo it configures** — `ainfra.yaml` committed beside the code | **In a separate central vault** — one `sx.toml` holds all assets for all repos |
| Composition | Three **layers** (team/repo/personal) **merge** into one resolved state | Per-asset **scopes** **filter** which assets reach which target |
| Unit | A manifest per repo | An asset in a vault, projected outward |

ainfra is config-as-code that travels *with* the code; sx is a content store
projected *onto* code. ainfra's repo-centric model is locked (design.md §0).
The *concepts* below port into it; the architecture does not.

A consequence worth stating: because ainfra's manifest lives in the repo,
"this asset only for repo X" is already free — it is in repo X's `ainfra.yaml`.
sx needs explicit `repo` scopes only because its vault is central. Not every
sx scope is a gap for ainfra.

## Concepts adopted

| sx concept | ainfra adoption | Where to find it |
|---|---|---|
| Identity scopes (`team`/`user`/`bot`, `SX_BOT`) | `scope.identities:` per entry, plus `--identity` and `AINFRA_IDENTITY` | `internal/manifest/types.go` (Selector), `internal/resolve/context.go`, `internal/cli/command.go` |
| Path scope (`#path` monorepo sub-targeting) | `scope.paths:` per entry — **filter-only in v1**; render targets unchanged | same as above |
| Profiles / multi-vault | Global personal layer at `$XDG_CONFIG_HOME/ainfra/personal.yaml` (repo wins on conflict) | `internal/manifest/load.go`, `internal/manifest/merge_personal.go` |
| Audit log (`.sx/audit/*.jsonl`) | Apply history at `.ainfra/history.jsonl` (read directly; `ainfra history` is a hidden alias) | `internal/provider/history.go`, `cmd/ainfra/cmd_history.go` |
| `metadata.toml` "wrap, don't edit" principle | Adopted as a principle for the skill provider | (not a code change) |

## Concepts rejected

- **Central vault model.** Abandoning repo-centric config-as-code would discard
  ainfra's thesis (design.md §0). The concepts above port into the repo-centric
  model; the architecture does not.
- **Cloud relay** (WebSocket bridge for claude.ai/chatgpt.com). Web clients
  have no filesystem; ainfra writes native files. This is a category boundary,
  not a gap.
- **`query` MCP server / skills.new lock-in.** A hosted runtime dependency
  contradicts ainfra's "no runtime lock-in" promise (design.md §0, §2).
- **Package-manager verbs** (`sx add`/`install`). ainfra's `plan`/`apply`/`check`
  is its discipline and is stronger (true drift `check`; sx has only
  `--dry-run`).

## What sx validates about ainfra (no action needed)

Renderer-per-agent scales (sx supports 9+ clients the same way); manifest+lock
is the right pattern; deferring Govern to product #2 matches sx's own roadmap
("RBAC and change-request flow" planned). ainfra's dependency graph, drift
`check`, secret-reference modes, and templates have **no sx equivalent** —
they are the moat and need no change.

## Open follow-ups (deliberately out of scope for v1)

- **Path scope as a render redirect.** Today `scope.paths:` only filters which
  invocations include an entry — rendering still goes to the repo-root agent
  files. Rendering into `services/api/.claude/` would require per-entry
  render targets (`provider.Env.Root` is currently fixed). Nested
  `ainfra.yaml` files cover most monorepo needs today.
- **Declared-identity registry.** `scope.identities:` is freeform — any
  string. A registry analogous to sx `[[bots]]` would let the manifest
  declare which identities it expects and validate references.
- **HTTP asset layout convention.** Once ainfra hosts skills/plugins at
  scale, sx's `{base}/{name}/{ver}/{name}-{ver}.zip` + `list.txt`
  Maven-style layout is the proven zero-infra distribution backend to copy.
