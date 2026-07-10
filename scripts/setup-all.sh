#!/bin/bash
set -e

echo "Setting up ainfra + Slack MCP for Claude Code..."
echo ""

# Step 1: Install ainfra tools and dependencies
echo "1/2 Installing ainfra..."
ainfra install

# Step 2: Wire slack-mcp-server into Claude Code
echo ""
echo "2/2 Wiring into Claude Code..."
./scripts/setup-claude-mcp.sh

echo ""
echo "✓ All done! Restart Claude Code to start using Slack search."
