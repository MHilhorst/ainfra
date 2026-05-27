---
description: Open a read-only MySQL console against one of the tunnelled databases.
---

# /db-console

Open an interactive MySQL console against one of the four tunnelled databases
defined in `ainfra.yaml` (`analytics-db`, `billing-db`, `catalog-db`,
`reporting-db`).

Steps:

1. Ask the user which database they want if not given as an argument.
2. Read the allocated local tunnel port for that instance from `ainfra.lock`
   (`entries.mcpServers.<name>.resolved.tunnelPort`).
3. Confirm the tunnel is up and the VPN precondition holds (`ainfra install --dry-run --strict`).
4. Launch: `mysql -h 127.0.0.1 -P <tunnelPort> -u <database>_ro <database>`.

This command never types a port by hand — the port comes from the lockfile,
which is exactly where `ainfra` recorded the tool-allocated value.
