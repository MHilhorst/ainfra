# Example — multi-database

This is **scenario 5**, the hardest modularity test in the design (§8) and the
primary [validation gate](../../docs/validation.md#scenario-5--the-multi-database-scenario).

## What it demonstrates

Four MySQL databases, each behind an SSH tunnel that needs the team VPN, plus a
hook and a slash command. The files here show the design holding up:

| File | Role |
|------|------|
| `ainfra.yaml` | Repo layer — one template, four instances, a hook, a command. The committed source of truth. |
| `ainfra.personal.yaml` | Personal layer — a developer's own dev replica, reusing the team template. (In a real repo this file is gitignored; here it is tracked as an illustration.) |
| `ainfra.lock` | Generated resolved state — four distinct tool-allocated ports, plus `hooks` and `commands` entries with content hashes. |
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
