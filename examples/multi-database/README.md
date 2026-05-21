# Example — multi-database

This is **scenario 5**, the hardest modularity test in the design (§8) and the
primary [validation gate](../../docs/validation.md#scenario-5--the-multi-database-scenario).

## What it demonstrates

Four MySQL databases, each behind an SSH tunnel that needs the team VPN. The
files here show the design holding up:

| File | Role |
|------|------|
| `ai-stack.yaml` | Repo layer — one template, four instances. The committed source of truth. |
| `ai-stack.personal.yaml` | Personal layer — a developer's own dev replica, reusing the team template. Gitignored. |
| `ai-stack.lock` | Generated resolved state — note the four distinct, tool-allocated ports. |

## The point

The human-declared intent for all four databases is ~20 lines under
`mcpServers:` in `ai-stack.yaml`. Each instance carries *only* what differs:
host, database, ssh user, and a password reference. Everything structural — the
launch command, the tunnel, the dependency chain, the ports — lives once in the
`mysql-over-ssh-tunnel` template or is computed by the tool.

That replaces a ~200-line prose runbook ("for each DB: open a tunnel on a free
port, point the MCP server at it, make sure the VPN is up first…") with a
declarative file a new developer can `aistack apply` and reproduce exactly.

The dependency chain is fully machine-readable:

```
mcpServer(analytics-db)
  └─ requires service: analytics-db-tunnel
       ├─ requires cliTool: ssh
       ├─ requires cliTool: mysql-client
       └─ requires precondition: vpn-tvt-internal
```

`aistack apply` walks that graph leaves-first; `aistack check` verifies every
node and fails loudly — with remediation text — if the VPN is down.
