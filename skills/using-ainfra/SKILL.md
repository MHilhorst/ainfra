---
name: using-ainfra
description: Use when working in a repo that contains an ainfra.yaml — explains how to install, add, remove, list, and verify the team's AI-tooling manifest without breaking the lockfile or committing personal config.
---

# Using ainfra

ainfra is config-as-code for a team's AI tooling, shaped like a package
manager. The repo you are in declares its setup in `ainfra.yaml`; `ainfra.lock`
pins resolved versions and content hashes. Your job as an agent is to keep
the manifest, the lockfile, and the developer's machine in sync — and to
never bypass that loop.

## The verbs you need

| Command | When to run it | Writes? |
|---|---|---|
| `ainfra install` | The default reconcile. Re-locks if the manifest is newer than the lock, then reconciles. | yes (machine) |
| `ainfra install --dry-run` | Preview the diff without writing | no |
| `ainfra install --dry-run --strict` | CI gate: exit non-zero on any drift | no |
| `ainfra add <channel> <id> [source]` | Add a new entry to `ainfra.yaml` and reconcile | yes (manifest + machine) |
| `ainfra remove <channel> <id>` | Remove an entry and reconcile | yes (manifest + machine) |
| `ainfra update [<channel> <id>]` | Re-resolve the lockfile and reinstall | yes (lockfile + machine) |
| `ainfra list` | Show installed entries from the merged lockfile | no |
| `ainfra outdated` | Show entries with newer resolvable versions (`--strict` for CI) | no |

Old Terraform-shaped verbs (`apply`, `plan`, `check`, `validate`, `lock`,
`schema`, `sync`, `exec`, `history`) still work as hidden aliases and print
a one-line deprecation note. Prefer the package-manager forms above when you
write scripts or docs.

## Workflow: adding a new dependency

The CLI is the primary editor. Hand-editing YAML is still supported, but most
days you can stay in the terminal:

```sh
ainfra add mcp github                          # add an MCP server
ainfra add command audit ./commands/audit.md   # add a slash command sourced from a file
ainfra add --personal mcp local-fs             # add to ainfra.personal.yaml instead
```

Each `add` writes the entry, re-locks, and runs install. Pass `--no-install`
to batch multiple `add` calls before a single reconcile. Commit `ainfra.yaml`
**and** `ainfra.lock` together; never commit one without the other.

## Workflow: editing the manifest by hand

Some entries — templates with complex `params:` blocks, hooks with embedded
shell, secrets referencing env vars — are easier to write by hand.

1. Edit `ainfra.yaml`.
2. `ainfra install --dry-run` — preview what would change.
3. `ainfra install` — reconcile. Pass `--yes` only in CI.
4. Commit `ainfra.yaml` **and** `ainfra.lock` together.

## Workflow: joining a repo that already uses ainfra

```sh
ainfra install                       # reconcile (re-locks first if needed)
ainfra install --dry-run --strict    # CI gate: should exit 0
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

`ainfra install --print-schema > ainfra.schema.json` prints the full JSON
Schema — point your editor's YAML language server at it for autocomplete.

## Layers and personal config

Three layers merge into one resolved state, with **repo wins** on conflict:

1. Org/team (optional, included from elsewhere)
2. Repo (`ainfra.yaml`, committed)
3. Personal (`ainfra.personal.yaml` in the repo, or
   `$XDG_CONFIG_HOME/ainfra/personal.yaml` globally — **never committed**)

If a developer needs a one-off MCP server or a personal secret, put it in the
personal layer with `ainfra add --personal …` or edit `ainfra.personal.yaml`
directly. Add `ainfra.personal.yaml` and `ainfra.personal.lock` to
`.gitignore` if they are not there already.

## Hard rules

- **Never commit a credential value.** Secrets are references.
- **Never commit `ainfra.personal.*`.**
- **Never hand-edit `ainfra.lock`.** Run `ainfra install` (or `ainfra update`).
- **Never hand-edit files ainfra owns** (`.mcp.json`, files under
  `.claude/skills/<id>/`, etc. that ainfra wrote). Change the manifest with
  `add`/`remove` and let `install` reconcile. The fsmerge layer will
  overwrite your edit on the next `install`.
- **`install` (without `--dry-run`) is the only verb that writes to the
  machine.** `--dry-run`, `list`, and `outdated` are safe.
- **Don't skip the re-lock.** `add`/`remove`/`update` re-lock automatically;
  hand-editing the manifest requires running `install` (or the hidden `lock`)
  before committing.

## Common failures and what they mean

| Symptom | Cause | Fix |
|---|---|---|
| `install --dry-run` says lockfile is stale | `ainfra.yaml` changed without re-locking | `ainfra install`, commit both files |
| `install --dry-run --strict` exits non-zero in CI | Drift between manifest and machine | Run `install` locally, or fix the manifest |
| `install` fails on a precondition | VPN/SSH key/file missing | Follow the precondition's `remediation` |
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
