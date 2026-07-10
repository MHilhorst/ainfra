#!/bin/bash
set -e

# Wire slack-mcp-server into Claude Code's ~/.claude.json after ainfra install
# This script reads the SLACK_MCP_XOXD_TOKEN from 1Password and updates the user's MCP config

SLACK_MCP_PATH=$(which slack-mcp-server 2>/dev/null || echo "$HOME/.local/bin/slack-mcp-server")

if [ ! -f "$SLACK_MCP_PATH" ]; then
  echo "Error: slack-mcp-server not found at $SLACK_MCP_PATH"
  echo "Run 'ainfra install' first"
  exit 1
fi

# Get token from 1Password
if ! TOKEN=$(op read "op://Private/Slack session token/notesPlain" 2>/dev/null); then
  echo "Error: Could not read Slack token from 1Password"
  echo "Make sure you have a Secure Note titled 'Slack session token' in your Private vault"
  exit 1
fi

# Update ~/.claude.json
CLAUDE_JSON="$HOME/.claude.json"

if [ ! -f "$CLAUDE_JSON" ]; then
  echo "Error: $CLAUDE_JSON not found"
  exit 1
fi

# Add slack MCP server to config
jq --arg path "$SLACK_MCP_PATH" --arg token "$TOKEN" '.mcpServers.slack = {
  "command": $path,
  "args": ["-transport", "stdio"],
  "env": {
    "SLACK_MCP_XOXD_TOKEN": $token
  }
}' "$CLAUDE_JSON" > "$CLAUDE_JSON.tmp" && mv "$CLAUDE_JSON.tmp" "$CLAUDE_JSON"

# Disable OAuth Slack plugin
jq '.enabledPlugins["slack@claude-plugins-official"] = false' "$CLAUDE_JSON" > "$CLAUDE_JSON.tmp" && mv "$CLAUDE_JSON.tmp" "$CLAUDE_JSON"

echo "✓ Slack MCP wired into Claude Code"
echo "✓ Old OAuth plugin disabled"
echo ""
echo "Next: Restart Claude Code and search Slack!"
