---
name: using-ainfra
description: Use when working in a repo that contains an ainfra.yaml — explains how to plan, apply, lock, and check the team's AI-tooling manifest without breaking the lockfile or committing personal config.
---

# Using ainfra

ainfra is config-as-code for a team's AI tooling. The repo you are in declares
its setup in `ainfra.yaml`; `ainfra.lock` pins resolved versions and content
hashes. Your job as an agent is to keep the manifest, the lockfile, and the
developer's machine in sync — and to never bypass that loop.

## The only four commands you need

| Command | When to run it | Writes? |
|---|---|---|
| `ainfra plan` | Before any reconcile, to preview changes | no |
| `ainfra apply` | After `plan` looks right, to reconcile the machine | yes (machine) |
| `ainfra lock` | After editing `ainfra.yaml` | yes (`ainfra.lock`) |
| `ainfra check` | To verify nothing drifted (CI-safe, exits non-zero on drift) | no |

`plan` requires a committed `ainfra.lock`. If `plan` complains the lock is
stale or missing, run `ainfra lock` first and commit the result alongside the
manifest change.

## Workflow: editing the manifest

1. Edit `ainfra.yaml`.
2. `ainfra validate` — static-checks the manifest. Fix errors before locking.
3. `ainfra lock` — resolves sources and writes `ainfra.lock`.
4. `ainfra plan` — preview what would change on this machine.
5. `ainfra apply` — reconcile. Pass `--yes` only in CI.
6. Commit `ainfra.yaml` **and** `ainfra.lock` together. Never commit one without
   the other.

## Workflow: joining a repo that already uses ainfra

```sh
ainfra plan
ainfra apply
ainfra check    # should exit 0
```

No `init` step — the manifest already ships with the repo.

## The eight channels

`ainfra.yaml` is organized into channels. Add to the channel that matches the
intent; do not invent ad-hoc files Claude reads.

- `cliTools` — binaries the channels rest on (brew/apt). Pin a version
  constraint and a `check` command.
- `secrets` — **references only** (`op://…`, Vault, Doppler). Never a literal
  value. Personal secrets get `scope: personal`.
- `mcpServers` — what lands in `.mcp.json`. Package-launched servers (`npx`,
  `uvx`) **must** pin `version:`.
- `hooks` — Claude Code lifecycle hooks (`PostToolUse`, etc.).
- `commands` — slash commands under `.claude/commands/`.
- `skills` — skill bundles under `.claude/skills/<id>/`. Source from a local
  path, a git repo, or `github:org/repo/path`.
- `rules` — files like `CLAUDE.md`, declared with a `target` and `source`.
- `marketplaces` + `plugins` — plugin marketplaces and the plugins drawn from
  them.

`ainfra schema > ainfra.schema.json` prints the full JSON Schema — point your
editor's YAML language server at it for autocomplete.

## Layers and personal config

Three layers merge into one resolved state, with **repo wins** on conflict:

1. Org/team (optional, included from elsewhere)
2. Repo (`ainfra.yaml`, committed)
3. Personal (`ainfra.personal.yaml` in the repo, or
   `$XDG_CONFIG_HOME/ainfra/personal.yaml` globally — **never committed**)

If a developer needs a one-off MCP server or a personal secret, put it in the
personal layer. Add `ainfra.personal.yaml` and `ainfra.personal.lock` to
`.gitignore` if they are not there already.

## Hard rules

- **Never commit a credential value.** Secrets are references.
- **Never commit `ainfra.personal.*`.**
- **Never hand-edit `ainfra.lock`.** Run `ainfra lock`.
- **Never hand-edit files ainfra owns** (`.mcp.json`, files under
  `.claude/skills/<id>/`, etc. that ainfra wrote). Change the manifest and
  re-apply. The fsmerge layer will overwrite your edit on the next `apply`.
- **`apply` is the only command that writes to the machine.** Reach for `plan`
  and `check` freely; they are safe.
- **Don't skip `lock`.** A manifest edit without a refreshed lockfile breaks
  every teammate's `plan`.

## Common failures and what they mean

| Symptom | Cause | Fix |
|---|---|---|
| `plan` says lockfile is stale | `ainfra.yaml` changed without re-locking | `ainfra lock`, commit both |
| `check` exits non-zero in CI | Drift between manifest and machine | Run `apply` locally, or fix the manifest |
| `apply` fails on a precondition | VPN/SSH key/file missing | Follow the precondition's `remediation` |
| MCP server "schema changed" error | Upstream package changed under a floating tag | Pin or bump `version:`, re-lock |
| DNS error on `*.tvt.internal` host | Team VPN offline | Connect VPN, re-run |

## What ainfra is not

Not a runtime MCP gateway. ainfra writes the native config your tools already
read — remove ainfra tomorrow and `.mcp.json`, `CLAUDE.md`, and
`.claude/skills/` keep working. Don't add runtime-routing logic to the
manifest; add it to the tool that runs the servers.

## Pointers

- Quick start: `docs/quickstart.md`
- Design rationale (locked decisions): `docs/reference/design.md`
- Manifest schema: `spec/manifest-schema.md`
- Lockfile schema: `spec/lockfile-schema.md`
- Worked example: `examples/multi-database/`
