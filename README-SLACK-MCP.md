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

## Real fix (needed)

Add to ainfra's `secrets sync` (or new `mcp sync`) command:
1. Read `ainfra.yaml` mcpServers
2. Resolve secrets (from 1Password)
3. Update user's `~/.claude.json`

This would make it: `ainfra install && ainfra secrets sync`

Or better: `ainfra install` does everything (requires ainfra core changes).

## TODO

- [ ] Propose MCP sync feature to ainfra maintainers
- [ ] OR: Fork ainfra and add MCP syncing ourselves
- [ ] OR: Build standalone tool that syncs ainfra MCP servers to Claude Code
