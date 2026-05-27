# Example — multi-database

This is **scenario 5**, the hardest modularity test in the design (§8) and the
primary [validation gate](../../docs/validation.md#scenario-5--the-multi-database-scenario).

## What it demonstrates

This example sets up four MySQL databases. Each one is reached through an SSH
tunnel (a secure connection that requires the team VPN). It also adds a fifth
MCP server reached over HTTP with an auth header; a precondition (a check that
must pass before something runs) on a credential file; plus a hook and a slash
command. The files here show the design holding up:

| File | Role |
|------|------|
| `ainfra.yaml` | Repo layer (a layer is one source of config that stacks onto the others) — one template, four instances, one HTTP server, a hook, a command. The committed source of truth. |
| `ainfra.personal.yaml` | Personal layer — a developer's own dev replica, reusing the team template. (In a real repo this file is gitignored; here it is tracked as an illustration.) |
| `ainfra.lock` | The lockfile — `ainfra.lock`, the auto-generated file that pins exact versions. This one holds the resolved state for the `team`/`repo` layers: four distinct tool-allocated ports, plus `hooks` and `commands` entries with content hashes. |
| `ainfra.personal.lock` | Generated resolved state for the `personal` layer — gitignored in a real repo, tracked here to match `ainfra.personal.yaml`. |
| `hooks/`, `commands/` | The hook script and command markdown that the manifest — `ainfra.yaml`, the file describing the team's setup — points at through its `source` fields. |

## The point

All four databases are declared in about 20 lines under `mcpServers:` in
`ainfra.yaml`. Each instance (one concrete copy made from a template) carries
*only* what differs between databases: host, database, ssh user, and a password
reference. Everything structural — the launch command, the tunnel, the
dependency chain, the ports — lives once in the `mysql-over-ssh-tunnel`
template (a reusable blueprint), or the tool computes it.

That replaces a ~200-line prose runbook ("for each DB: open a tunnel on a free
port, point the MCP server at it, make sure the VPN is up first…") with a
single declarative file. A new developer can run `ainfra install` and reproduce
the setup exactly.

The dependency chain is fully machine-readable:

```
mcpServer(analytics-db)
  └─ requires service: analytics-db-tunnel
       ├─ requires cliTool: ssh
       ├─ requires cliTool: mysql-client
       └─ requires precondition: vpn-tvt-internal
```

`ainfra install` walks that graph leaves-first. `ainfra install --dry-run --strict` verifies every
node and fails loudly — with text on how to fix it — if the VPN is down.

## Hooks and commands

The `hooks` and `commands` channels (a channel is one category of AI-tooling
config — MCP servers, hooks, and so on) sit alongside `mcpServers`. They use
the same layering, the same `requires` edges, and the same content hashing in
the lockfile:

- `hooks.guard-destructive-sql` — a `PreToolUse`/`Bash` hook that sends
  destructive SQL to the user for approval. It `requires` the `node` CLI tool,
  so that edge joins the same dependency graph as the tunnels.
- `commands.db-console` — a `/db-console` slash command, a sourced markdown file.

Both land in `ainfra.lock` with a `contentHash`. That way, a teammate's
`ainfra install --dry-run --strict` detects whether a hook script or command was altered after the
fact.

## Beyond the template

Two entries deliberately sit outside the `mysql-over-ssh-tunnel` template, to
show the schema covers more than the headline case:

- `mcpServers.linear` — an `http` server that does not use the template. It
  declares a `url` and an `Authorization` header sourced from an `op://` secret,
  instead of launching a `command`/`args` subprocess.
- `preconditions.mysql-defaults-file` — a `file-exists` check on `~/.my.cnf`
  with `mode: "0600"`. ainfra *verifies* that the credential file exists with
  the right permissions; it never writes the file. The credential file stays
  reference-only — something ainfra checks but does not manage.
  `cliTools.mysql-client` depends on it via `requires`.
