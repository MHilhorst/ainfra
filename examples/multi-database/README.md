# Example — multi-database

This is **scenario 5**, the hardest modularity test in the design (§8) and the
primary [validation gate](../../docs/validation.md#scenario-5--the-multi-database-scenario).

## What it demonstrates

Four MySQL databases, each behind an SSH tunnel that needs the team VPN; a
fifth MCP server reached over HTTP with an auth header; a credential-file
precondition; plus a hook and a slash command. The files here show the design
holding up:

| File | Role |
|------|------|
| `ainfra.yaml` | Repo layer — one template, four instances, one HTTP server, a hook, a command. The committed source of truth. |
| `ainfra.personal.yaml` | Personal layer — a developer's own dev replica, reusing the team template. (In a real repo this file is gitignored; here it is tracked as an illustration.) |
| `ainfra.lock` | Generated resolved state for the `team`/`repo` layers — four distinct tool-allocated ports, plus `hooks` and `commands` entries with content hashes. |
| `ainfra.personal.lock` | Generated resolved state for the `personal` layer — gitignored in a real repo, tracked here to match `ainfra.personal.yaml`. |
| `hooks/`, `commands/` | The hook script and command markdown the manifest's `source` fields point at. |

## The point

The human-declared intent for all four databases is ~20 lines under
`mcpServers:` in `ainfra.yaml`. Each instance carries *only* what differs:
host, database, ssh user, and a password reference. Everything structural — the
launch command, the tunnel, the dependency chain, the ports — lives once in the
`mysql-over-ssh-tunnel` template or is computed by the tool.

That replaces a ~200-line prose runbook ("for each DB: open a tunnel on a free
port, point the MCP server at it, make sure the VPN is up first…") with a
declarative file a new developer can `ainfra apply` and reproduce exactly.

The dependency chain is fully machine-readable:

```
mcpServer(analytics-db)
  └─ requires service: analytics-db-tunnel
       ├─ requires cliTool: ssh
       ├─ requires cliTool: mysql-client
       └─ requires precondition: vpn-tvt-internal
```

`ainfra apply` walks that graph leaves-first; `ainfra check` verifies every
node and fails loudly — with remediation text — if the VPN is down.

## Hooks and commands

The `hooks` and `commands` channels sit alongside `mcpServers` — same layering,
same `requires` edges, same content hashing in the lockfile:

- `hooks.guard-destructive-sql` — a `PreToolUse`/`Bash` hook that escalates
  destructive SQL to user approval. It `requires` the `node` CLI tool, so that
  edge joins the same dependency graph as the tunnels.
- `commands.db-console` — a `/db-console` slash command, a sourced markdown file.

Both land in `ainfra.lock` with a `contentHash`, so a teammate's `ainfra check`
detects if a hook script or command is altered after the fact.

## Beyond the template

Two entries deliberately sit outside the `mysql-over-ssh-tunnel` template, to
show the schema covers more than the headline case:

- `mcpServers.linear` — a non-templated `transport: http` server. It declares a
  `url` and an `Authorization` header sourced from an `op://` secret, instead of
  a `command`/`args` subprocess launch.
- `preconditions.mysql-defaults-file` — a `file-exists` check on `~/.my.cnf`
  with `mode: "0600"`. ainfra *verifies* the credential file exists with the
  right permissions and never writes it — the environment primitive stays
  reference-only. `cliTools.mysql-client` depends on it via `requires`.
