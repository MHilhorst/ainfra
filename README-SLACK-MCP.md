# Slack MCP Setup

**Status**: Works locally. Team setup requires ainfra enhancement.

## Current setup

1. **Binary**: `slack-mcp-server` is in `ainfra.yaml` cliTools ✓
2. **Config**: Slack MCP server is defined in `ainfra.yaml` mcpServers ✓  
3. **Problem**: ainfra doesn't auto-sync MCP servers to `~/.claude.json` ✗

## Why

ainfra manages:
- `ainfra.yaml` (team config) ✓
- `ainfra.lock` (pinned versions) ✓
- `.mcp.json` (project-level Claude config) ✓

But NOT:
- `~/.claude.json` (user-level Claude config) ✗

This creates a gap: team config exists but isn't loaded globally.

## Workaround (current)

```bash
# Installs slack-mcp-server binary
ainfra install

# Syncs it to ~/.claude.json
./scripts/setup-claude-mcp.sh
```

## Known gap (by ainfra team)

From `cmd/ainfra/commands.go`:
```
"User-scope MCP (~/.claude.json) is a follow-up; the server will only be visible in this repo."
```

**Status**: ainfra team knows this feature is missing and has it planned ("follow-up").

Currently: MCP servers go to `.mcp.json` (repo-level), not `~/.claude.json` (user-level).

## When ainfra adds ~/.claude.json sync

Once ainfra ships user-scope MCP support, it will be:
```bash
ainfra install  # that's it
```

Until then: workaround is `ainfra install && ./scripts/setup-claude-mcp.sh`
