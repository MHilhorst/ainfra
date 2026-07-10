# Slack MCP Server Setup

The Slack MCP server is now available to all team members via ainfra. It allows Claude Code to search, read, and interact with Slack messages directly — **no bot tokens, no scope approvals needed**.

## How It Works

The server uses "stealth mode" — it extracts your personal Slack browser session and uses that to access messages. No special permissions or admin approval required.

## Setup (One-Time Per Developer)

### 1. Get Your Session Token

1. **Open Slack in Chrome** (must be Chrome or Chromium-based)
2. **Open DevTools** → Press `Cmd+Option+J` (Mac) or `Ctrl+Shift+J` (Windows/Linux)
3. Go to **Console** tab
4. **Paste and run this command:**
   ```javascript
   console.log(document.cookie)
   ```
5. **Find the line** that contains `d=` — it looks like:
   ```
   d=xoxd-YxEZwglt0y2IFsUnLssPw3pP6bsJRtFGL76BrLc6VvHP5xuFcjqFJz%2FFgb9JN0LpweJSggE3hg7zMeG0Av8IzRzzIlhBzRMgOSQSKIVteixGV69wJfEhboeUe0yptvlME6EBek4tPGLqWz%2FuetH%2BxeS3nRj%2BIaSaP%2BIXBDUhZ1q7I%2BHL7X7CMlEvd6oUaGyOxlpti5wBwfFA8NknVLdsNSQGSItWJQ%3D%3D
   ```
6. **Copy the value** after `d=` (including the URL-encoded characters)

### 2. Store in Your 1Password Private Vault

Each team member does this in **their own vault**:

1. Open **1Password**
2. Go to your **Private** vault
3. Create a new **Secure Note**
4. **Title:** `Slack session token`
5. **Content:** Paste the full token (xoxd-...)
6. **Save**

That's it! ainfra will automatically fetch it from your vault when you run `ainfra install`.

### 3. Apply Changes

```bash
ainfra install
```

The Slack MCP server is now active in Claude Code. Each developer's own 1Password vault supplies their own token — **no sharing, no accidents**.

### Security Notes

- Your token is **encrypted in 1Password** — never stored in shell profiles or git
- ainfra uses your local `op` CLI to fetch the token — **you control the auth**
- Each developer's vault is separate — tokens are never mixed up

## Using It

Once installed, Claude Code has access to Slack tools. Examples:

- **Search messages:** "Search Slack for 'release notes' in #engineering"
- **List channels:** "Show me all channels I'm in"
- **Read threads:** "Get all messages from #announcements from today"
- **Add reactions:** "React with :thumbsup: to the last message in #general"

## Troubleshooting

### Token Not Working
- **Token expired?** Extract a new one and update 1Password
- **Using Slack web instead of desktop app?** Slack web cookies are different — try opening slack.com in a new tab

### MCP Server Won't Start
- Check that `slack-mcp-server` binary is on your PATH: `which slack-mcp-server`
- If not found, install it manually:
  ```bash
  brew install korotovsky/slack-mcp-server  # if available
  # or download from: https://github.com/korotovsky/slack-mcp-server/releases
  ```

### Scope Issues
- This approach uses **no OAuth scopes** — you don't need any Slack app permissions
- If you see "missing_scope" errors, something went wrong; check that you're using xoxd- token, not xoxp- or xoxb-

## Security

- Your session token is **stored locally** in your 1Password vault (encrypted)
- It's never shared with anyone, including your team's Claude Code instances
- Session tokens automatically expire; you can generate a new one anytime
- If you leave the team, just delete the token from 1Password

## More Info

- [Slack MCP Server GitHub](https://github.com/korotovsky/slack-mcp-server)
- [ainfra Documentation](../README.md)
