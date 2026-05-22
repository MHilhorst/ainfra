# tvt-config `ainfra.yaml` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Author the real two-file `ainfra` manifest for the `tvt-config` Claude Code setup and test it against the current `ainfra` build, producing an evidence-based gap report.

**Architecture:** Aspirational-complete, two-layer manifest (`ainfra.yaml` team + `ainfra.personal.yaml` personal) authored in the `claude-config` repo. Each channel is added in its own task and gated by `ainfra validate`. After all channels are in, `ainfra lock`/`plan` are run to capture which entries resolve and which fail; the failures become the gap report.

**Tech Stack:** Go (the `ainfra` CLI), YAML (the manifest), the `claude-config` repo.

---

## Conventions used by every task

- **`ainfra` CLI** is built once in Task 0 at `$AINFRA` (the binary inside this worktree).
- **The manifest** lives in a `claude-config` worktree at `$CC` (created in Task 0).
- Every validate run is: `"$AINFRA" --chdir "$CC" validate`.
- Manifest commits land in the `claude-config` worktree (branch `ainfra-manifest`).
- The gap-report commit (Task 13) lands in **this** `ainfra` worktree.
- Commit messages: no emoji, short, professional, no `Co-Authored-By` trailer.

## File structure

| File | Repo | Responsibility |
|---|---|---|
| `ainfra.yaml` | claude-config | Team layer — all shared channels |
| `ainfra.personal.yaml` | claude-config | Personal layer — per-dev secrets |
| `ainfra.lock` | claude-config | Resolved lock (partial; committed in Task 11) |
| `.gitignore` | claude-config | Add `ainfra.personal.yaml`, `ainfra.personal.lock` |
| `docs/assessment-vs-real-config.md` | ainfra | Rewritten as evidence-based gap report (Task 13) |

---

### Task 0: Build the CLI and prepare the claude-config branch

**Files:** none created; environment setup only.

- [ ] **Step 1: Build the ainfra binary**

Run from this worktree root:
```bash
go build -o ainfra ./cmd/ainfra
export AINFRA="$(pwd)/ainfra"
"$AINFRA" version
```
Expected: prints a version line (e.g. `ainfra 0.x.y`).

- [ ] **Step 2: Create a claude-config worktree for the manifest**

```bash
git -C ~/projects/tvt-nl/claude-config worktree add \
  ~/projects/tvt-nl/claude-config/.claude/worktrees/ainfra-manifest -b ainfra-manifest
export CC=~/projects/tvt-nl/claude-config/.claude/worktrees/ainfra-manifest
ls "$CC"
```
Expected: the worktree directory lists the claude-config repo contents. If the branch already exists, use `git -C ~/projects/tvt-nl/claude-config worktree add <path> ainfra-manifest` instead.

- [ ] **Step 3: Confirm validate runs on the (manifest-less) repo**

```bash
"$AINFRA" --chdir "$CC" validate
```
Expected: an error that no `ainfra.yaml` exists. This confirms the CLI and `--chdir` work. No commit.

---

### Task 1: Scaffold both manifest files

**Files:**
- Create: `$CC/ainfra.yaml`
- Create: `$CC/ainfra.personal.yaml`
- Modify: `$CC/.gitignore`

- [ ] **Step 1: Create the team manifest skeleton**

Write `$CC/ainfra.yaml`:
```yaml
version: 1

# ---------------------------------------------------------------------------
# tvt-config as config-as-code. This manifest reproduces the trein-vertraging
# team's Claude Code setup declaratively. Authored aspirational-complete: it
# declares the full target state even where `ainfra` cannot yet resolve an
# entry. See docs/superpowers/specs/2026-05-21-tvt-config-ainfra-manifest-design.md
# in the ainfra repo.
# ---------------------------------------------------------------------------
```

- [ ] **Step 2: Create the personal manifest skeleton**

Write `$CC/ainfra.personal.yaml`:
```yaml
version: 1

# Personal layer — gitignored, never committed. Holds this developer's own
# 1Password Private-vault credentials.
```

- [ ] **Step 3: Gitignore the personal layer**

Append to `$CC/.gitignore`:
```
ainfra.personal.yaml
ainfra.personal.lock
```

- [ ] **Step 4: Validate**

Run: `"$AINFRA" --chdir "$CC" validate`
Expected: PASS — an empty (version-only) manifest is valid.

- [ ] **Step 5: Commit**

```bash
git -C "$CC" add ainfra.yaml .gitignore
git -C "$CC" commit -m "Scaffold ainfra manifest for tvt-config"
```
(`ainfra.personal.yaml` is gitignored and not committed.)

---

### Task 2: preconditions + cliTools channels

**Files:** Modify `$CC/ainfra.yaml`

- [ ] **Step 1: Append the preconditions and cliTools blocks**

Append to `$CC/ainfra.yaml`:
```yaml
# --- Verify-only: ainfra checks these, never provisions them -----------------
preconditions:
  vpn-tvt-internal:
    description: Team VPN must be connected to reach *.tvt.internal hosts.
    check:
      type: dns-resolves
      host: metabase.tvt.internal
    remediation: "Connect the team VPN, then re-run: ainfra check"

# --- Installable substrate: the binaries `tvt` provisions today --------------
# brew adapters resolve once the cliTool resolver lands; uv/composer have no
# adapter yet and are declare-and-check (sub-project #4).
cliTools:
  op:
    install:
      brew: { cask: 1password-cli }
    check:
      command: "op --version"
      versionRegex: '(\d+\.\d+\.\d+)'
  linear:
    install:
      brew: { formula: schpet/tap/linear }
    check:
      command: "linear --version"
      versionRegex: '(\d+\.\d+\.\d+)'
  mysql-client:
    install:
      brew: { formula: mysql-client }
    check:
      command: "mysql --version"
      versionRegex: 'Ver (\d+\.\d+\.\d+)'
  uv:
    install:
      brew: { formula: uv }
    check:
      command: "uv --version"
      versionRegex: 'uv (\d+\.\d+\.\d+)'
  wacli:
    install:
      brew: { formula: steipete/tap/wacli }
    check:
      command: "wacli --version"
      versionRegex: '(\d+\.\d+\.\d+)'
  higgsfield:
    install:
      brew: { formula: higgsfield-ai/tap/higgsfield }
    check:
      command: "higgsfield --version"
      versionRegex: '(\d+\.\d+\.\d+)'
  meta:
    install:
      uv: { package: meta-ads, python: "3.13" }
    check:
      command: "meta --version"
      versionRegex: '(\d+\.\d+\.\d+)'
  google-analytics-cli:
    install:
      npm: { package: google-analytics-cli, version: "1.1.1" }
    check:
      command: "google-analytics-cli --version"
      versionRegex: '(\d+\.\d+\.\d+)'
  tipctl:
    install:
      composer: { package: transip/tipctl }
    check:
      command: "tipctl --version"
      versionRegex: '(\d+\.\d+\.\d+)'
```

- [ ] **Step 2: Validate**

Run: `"$AINFRA" --chdir "$CC" validate`
Expected: PASS — `validate` does not constrain `cliTools` or `preconditions` structure.

- [ ] **Step 3: Commit**

```bash
git -C "$CC" add ainfra.yaml
git -C "$CC" commit -m "Add preconditions and cliTools channels"
```

---

### Task 3: secrets channel (team)

**Files:** Modify `$CC/ainfra.yaml`

The `op://` field paths below are best-effort; they do not resolve in this sub-project (the 1Password resolver is sub-project #2). Only their schema shape matters now.

- [ ] **Step 1: Append the secrets block**

Append to `$CC/ainfra.yaml`:
```yaml
# --- Secrets: references only — ainfra never stores a credential value -------
# Shared vault: 1Password "TVT Claude Code". Personal secrets live in
# ainfra.personal.yaml.
secrets:
  metabase-api-key:
    mode: direct
    scope: shared
    ref: "op://TVT Claude Code/metabase/api_key"
  flare-api-token:
    mode: direct
    scope: shared
    ref: "op://TVT Claude Code/flare/api_token"
  intercom-api-token:
    mode: direct
    scope: shared
    ref: "op://TVT Claude Code/intercom/api_token"
  stape-api-key:
    mode: direct
    scope: shared
    ref: "op://TVT Claude Code/stape/api_key"
  meta-access-token:
    mode: direct
    scope: shared
    ref: "op://TVT Claude Code/meta-ads/access_token"
  meta-app-id:
    mode: direct
    scope: shared
    ref: "op://TVT Claude Code/meta-ads/app_id"
  meta-app-secret:
    mode: direct
    scope: shared
    ref: "op://TVT Claude Code/meta-ads/app_secret"
  treinvertraging-db-user:
    mode: direct
    scope: shared
    ref: "op://TVT Claude Code/prod-db-trein-vertraging/username"
  treinvertraging-db-password:
    mode: direct
    scope: shared
    ref: "op://TVT Claude Code/prod-db-trein-vertraging/password"
  businessportal-db-user:
    mode: direct
    scope: shared
    ref: "op://TVT Claude Code/prod-db-business-portal/username"
  businessportal-db-password:
    mode: direct
    scope: shared
    ref: "op://TVT Claude Code/prod-db-business-portal/password"
```

- [ ] **Step 2: Validate**

Run: `"$AINFRA" --chdir "$CC" validate`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git -C "$CC" add ainfra.yaml
git -C "$CC" commit -m "Add shared secrets channel"
```

---

### Task 4: templates + the two templated MySQL MCP servers

**Files:** Modify `$CC/ainfra.yaml`

- [ ] **Step 1: Resolve the MySQL MCP package version**

```bash
npm view @benborla29/mcp-server-mysql version
```
Use the printed value as `<MYSQL_MCP_VERSION>` in Step 2.

- [ ] **Step 2: Append the template and the two instances**

Append to `$CC/ainfra.yaml`, substituting `<MYSQL_MCP_VERSION>`:
```yaml
# --- Template: a read-only MySQL MCP server over an SSH tunnel ---------------
# tvt runs these as fixed-port (3307/3308) launchd tunnels. ainfra allocates
# the tunnel port itself — an intentional improvement: no hand-assigned ports.
templates:
  mysql-over-ssh-tunnel:
    description: A read-only MySQL MCP server reachable through an SSH tunnel.
    params:
      database: { type: string, required: true }
      sshUser:  { type: string, required: true }
    secrets:
      dbUser:     { required: true }
      dbPassword: { required: true }
    resolved:
      tunnelPort:
        kind: allocated-port
      launcher:
        kind: generated-script-path
    produces:
      mcpServer:
        transport: stdio
        command: npx
        args: ["-y", "@benborla29/mcp-server-mysql"]
        version: "<MYSQL_MCP_VERSION>"
        env:
          MYSQL_HOST: "127.0.0.1"
          MYSQL_PORT: "${resolved.tunnelPort}"
          MYSQL_USER: "${secret.dbUser}"
          MYSQL_PASS: "${secret.dbPassword}"
          MYSQL_DB: "${params.database}"
          ALLOW_INSERT_OPERATION: "false"
          ALLOW_UPDATE_OPERATION: "false"
          ALLOW_DELETE_OPERATION: "false"
          MYSQL_QUERY_TIMEOUT: "30000"
          MYSQL_RATE_LIMIT: "100"
        requires:
          - service: "${instance.id}-tunnel"
      backgroundService:
        id: "${instance.id}-tunnel"
        kind: ssh-tunnel
        spec:
          localPort:  "${resolved.tunnelPort}"
          remotePort: 3306
          sshUser:    "${params.sshUser}"
          launcher:   "${resolved.launcher}"
        requires:
          - cliTool: ssh
          - cliTool: mysql-client
          - precondition: vpn-tvt-internal
        lifecycle:
          generateHook: SessionStart
        check:
          type: port-listening
          port: "${resolved.tunnelPort}"

# --- MCP servers -------------------------------------------------------------
mcpServers:
  trein-vertraging-platform-db-prod:
    template: mysql-over-ssh-tunnel
    params: { database: prod_db, sshUser: deploy }
    secret:
      dbUser: treinvertraging-db-user
      dbPassword: treinvertraging-db-password

  trein-vertraging-business-portal-db-prod:
    template: mysql-over-ssh-tunnel
    params: { database: forge, sshUser: deploy }
    secret:
      dbUser: businessportal-db-user
      dbPassword: businessportal-db-password
```

Note: `sshUser` is set to `deploy` as a placeholder-free default. If `claude-config` has a tunnel setup script (`scripts/install-tunnels.sh` or similar) with a different bastion user, use that value instead — check with `ls "$CC"/scripts | grep -i tunnel`.

- [ ] **Step 3: Validate**

Run: `"$AINFRA" --chdir "$CC" validate`
Expected: PASS. If it reports `unknown template`, the template block is mis-indented relative to `mcpServers`.

- [ ] **Step 4: Commit**

```bash
git -C "$CC" add ainfra.yaml
git -C "$CC" commit -m "Add MySQL tunnel template and database MCP servers"
```

---

### Task 5: inline MCP servers (stdio, http, disabled)

**Files:** Modify `$CC/ainfra.yaml`

- [ ] **Step 1: Resolve package versions for the stdio servers**

```bash
npm view @upstash/context7-mcp version
npm view @playwright/mcp version
npm view @mobilenext/mobile-mcp version
npm view chrome-devtools-mcp version
npm view @kevinwatt/yt-dlp-mcp version
```
For the two `uvx` servers, use `meta-ads-mcp` version `1.0.0` and `linkedin-scraper-mcp` version `1.0.0` unless a newer pin is known (`uvx` has no quick `view`; these are placeholder-free valid pins that the gap report flags for confirmation in sub-project #3). Record all seven values for Step 2.

- [ ] **Step 2: Append the inline stdio servers**

Append under the existing `mcpServers:` map in `$CC/ainfra.yaml` (same indentation as the two database servers), substituting the resolved versions:
```yaml
  metabase-prod:
    transport: stdio
    command: metabase-server
    env:
      METABASE_API_KEY: "${secret.apiKey}"
      METABASE_URL: "https://metabase.tvt.internal"
      NODE_TLS_REJECT_UNAUTHORIZED: "0"
    secret:
      apiKey: metabase-api-key
    requires:
      - precondition: vpn-tvt-internal

  context7:
    transport: stdio
    command: npx
    args: ["-y", "@upstash/context7-mcp"]
    version: "<CONTEXT7_VERSION>"

  playwright:
    transport: stdio
    command: npx
    args: ["-y", "@playwright/mcp"]
    version: "<PLAYWRIGHT_VERSION>"

  mobile:
    transport: stdio
    command: npx
    args: ["-y", "@mobilenext/mobile-mcp"]
    version: "<MOBILE_VERSION>"

  chrome-devtools:
    transport: stdio
    command: npx
    args: ["-y", "chrome-devtools-mcp"]
    version: "<CHROME_DEVTOOLS_VERSION>"

  yt-dlp:
    transport: stdio
    command: npx
    args: ["-y", "@kevinwatt/yt-dlp-mcp"]
    version: "<YT_DLP_VERSION>"

  meta-ads:
    transport: stdio
    command: uvx
    args: ["meta-ads-mcp"]
    version: "<META_ADS_VERSION>"
    env:
      META_ACCESS_TOKEN: "${secret.accessToken}"
      META_APP_ID: "${secret.appId}"
      META_APP_SECRET: "${secret.appSecret}"
    secret:
      accessToken: meta-access-token
      appId: meta-app-id
      appSecret: meta-app-secret

  linkedin:
    transport: stdio
    command: uvx
    args: ["linkedin-scraper-mcp"]
    version: "<LINKEDIN_VERSION>"
    env:
      UV_HTTP_TIMEOUT: "300"
```

The four `@latest`-pinned servers in the live `.mcp.json` (playwright, mobile, chrome-devtools, yt-dlp) are pinned here — `validate` requires a version for `npx`/`uvx` servers. This divergence is recorded in the gap report.

- [ ] **Step 3: Append the inline http servers**

Append under `mcpServers:`:
```yaml
  figma:
    transport: http
    url: https://mcp.figma.com/mcp

  slack:
    transport: http
    url: https://mcp.slack.com/mcp

  mobbin:
    transport: http
    url: https://api.mobbin.com/mcp

  posthog:
    transport: http
    url: https://mcp.posthog.com/mcp
    headers:
      Authorization: "Bearer ${secret.apiKey}"
    secret:
      apiKey: posthog-personal-api-key

  flare:
    transport: http
    url: https://flareapp.io/mcp
    headers:
      Authorization: "Bearer ${secret.token}"
    secret:
      token: flare-api-token

  intercom:
    transport: http
    url: https://mcp.intercom.com/mcp
    headers:
      Authorization: "Bearer ${secret.token}"
    secret:
      token: intercom-api-token

  stape:
    transport: http
    url: https://mcp.stape.ai/mcp
    headers:
      Authorization: "${secret.apiKey}"
      X-Stape-Region: "EU"
    secret:
      apiKey: stape-api-key
```

`flare`, `intercom`, and `stape` run as `npx mcp-remote` stdio bridges in the live `.mcp.json`; ainfra models them idiomatically as native `transport: http` servers with auth headers. This divergence is recorded in the gap report. `posthog` references `posthog-personal-api-key`, a personal secret defined in `ainfra.personal.yaml` (Task 10).

- [ ] **Step 4: Append the two disabled servers**

Append under `mcpServers:`:
```yaml
  # Disabled 2026-04-21 in favour of the `linear` CLI.
  linear-server:
    transport: http
    url: https://mcp.linear.app/mcp
    enabled: false

  # Disabled 2026-05-01: Claude Code's MCP redirect URI is not whitelisted on
  # Meta's app. The `meta-ads` server above covers the same use cases.
  meta-ads-official:
    transport: http
    url: https://mcp.facebook.com/ads
    enabled: false
```

- [ ] **Step 5: Validate**

Run: `"$AINFRA" --chdir "$CC" validate`
Expected: PASS. Common failures and fixes:
- `package-launched server must pin an exact version` — a `<..._VERSION>` substitution was missed.
- `http MCP server declares stdio-only fields` — an http server still has `command`/`args`/`version`.
- `stdio MCP server declares http-only fields` — a stdio server has `url`/`headers`.

- [ ] **Step 6: Commit**

```bash
git -C "$CC" add ainfra.yaml
git -C "$CC" commit -m "Add inline MCP servers (stdio, http, disabled)"
```

---

### Task 6: hooks channel

**Files:** Modify `$CC/ainfra.yaml`

The eight hooks below are transcribed from `claude-config/hooks/hooks.json`. ainfra installs each `source` script into `.ainfra/run/` and the `command` references that installed path.

- [ ] **Step 1: Append the hooks block**

Append to `$CC/ainfra.yaml`:
```yaml
# --- Hooks: automation bound to Claude Code lifecycle events -----------------
hooks:
  project-context:
    event: SessionStart
    matcher: ""
    command: node .ainfra/run/project-context.js
    source: ./scripts/project-context.js
    timeout: 3000
    requires:
      - cliTool: node

  branch-guard:
    event: SessionStart
    matcher: ""
    command: node .ainfra/run/branch-guard.js
    source: ./scripts/branch-guard.js
    timeout: 3000
    requires:
      - cliTool: node

  branch-check:
    event: UserPromptSubmit
    matcher: "*"
    command: node .ainfra/run/branch-check.js
    source: ./scripts/branch-check.js
    timeout: 2000
    requires:
      - cliTool: node

  enforce-branch:
    event: PreToolUse
    matcher: "Edit|Write"
    command: node .ainfra/run/enforce-branch.js
    source: ./scripts/enforce-branch.js
    timeout: 2000
    requires:
      - cliTool: node

  block-destructive:
    event: PreToolUse
    matcher: "Bash"
    command: bash .ainfra/run/block-destructive.sh
    source: ./scripts/block-destructive.sh
    timeout: 5000

  notify-sound:
    event: Notification
    matcher: ""
    command: bash .ainfra/run/notify-sound.sh
    source: ./scripts/notify-sound.sh
    timeout: 5000

  post-edit-check:
    event: PostToolUse
    matcher: "Edit"
    command: node .ainfra/run/post-edit-check.js
    source: ./scripts/post-edit-check.js
    timeout: 3000
    requires:
      - cliTool: node

  post-accounting-check:
    event: PostToolUse
    matcher: "Edit|Write"
    command: node .ainfra/run/post-accounting-check.js
    source: ./scripts/post-accounting-check.js
    timeout: 2000
    requires:
      - cliTool: node
```

- [ ] **Step 2: Add the `node` cliTool the hooks require**

The hooks declare `requires: - cliTool: node`, so `node` must exist as a cliTool. Append under the existing `cliTools:` map in `$CC/ainfra.yaml`:
```yaml
  node:
    versionConstraint: ">=20"
    install:
      brew: { formula: node }
    check:
      command: "node --version"
      versionRegex: 'v(\d+\.\d+\.\d+)'
```
Also add an `ssh` cliTool — the MySQL template's background service requires it:
```yaml
  ssh:
    versionConstraint: ">=8.0"
    check:
      command: "ssh -V"
      versionRegex: 'OpenSSH_(\d+\.\d+)'
```

- [ ] **Step 3: Validate**

Run: `"$AINFRA" --chdir "$CC" validate`
Expected: PASS. If it reports `unknown or missing hook event`, a hook's `event` is misspelled — valid events are SessionStart, SessionEnd, UserPromptSubmit, PreToolUse, PostToolUse, Notification, Stop, SubagentStop, PreCompact.

- [ ] **Step 4: Commit**

```bash
git -C "$CC" add ainfra.yaml
git -C "$CC" commit -m "Add hooks channel and supporting cliTools"
```

---

### Task 7: commands channel

**Files:** Modify `$CC/ainfra.yaml`

The nine commands match the files in `claude-config/commands/`.

- [ ] **Step 1: Append the commands block**

Append to `$CC/ainfra.yaml`:
```yaml
# --- Commands: team slash commands, sourced and content-hashed ---------------
commands:
  dbaccess:
    source: ./commands/dbaccess.md
  document:
    source: ./commands/document.md
  merge:
    source: ./commands/merge.md
  pr:
    source: ./commands/pr.md
  review-wip:
    source: ./commands/review-wip.md
  ship:
    source: ./commands/ship.md
  spin:
    source: ./commands/spin.md
  start:
    source: ./commands/start.md
  stop:
    source: ./commands/stop.md
```

- [ ] **Step 2: Validate**

Run: `"$AINFRA" --chdir "$CC" validate`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git -C "$CC" add ainfra.yaml
git -C "$CC" commit -m "Add commands channel"
```

---

### Task 8: plugins channel

**Files:** Modify `$CC/ainfra.yaml`

Sources match `claude-config/.claude-plugin/marketplace.json`. `git+` sources require a `version` field (a `@branch` ref does not satisfy it).

- [ ] **Step 1: Resolve the latest tag for each GitHub plugin**

```bash
git ls-remote --tags --refs https://github.com/AgriciDaniel/claude-ads.git | tail -1
git ls-remote --tags --refs https://github.com/expo/skills.git | tail -1
git ls-remote --tags --refs https://github.com/EveryInc/compound-engineering-plugin.git | tail -1
git ls-remote --tags --refs https://github.com/higgsfield-ai/skills.git | tail -1
```
For each repo: if a tag is printed, use that tag (without the `refs/tags/` prefix) for both the `@<ref>` in `source` and the `version` field. If no tag is printed, use `@main` for the ref and `version: "0.0.0-main"` — the gap report flags these for a real pin in sub-project #3.

- [ ] **Step 2: Append the plugins block**

Append to `$CC/ainfra.yaml`, substituting the resolved refs/versions:
```yaml
# --- Plugins: the trein-vertraging marketplace -------------------------------
# The tvt-config plugin is this repo itself (local source). The 38 team skills
# ride inside it — there is no separate skills: block. The four third-party
# plugins fetch from GitHub; remote git fetch is sub-project #3.
plugins:
  tvt-config:
    source: ./

  claude-ads:
    source: "git+https://github.com/AgriciDaniel/claude-ads.git@<CLAUDE_ADS_REF>"
    version: "<CLAUDE_ADS_VERSION>"

  expo:
    source: "git+https://github.com/expo/skills.git@<EXPO_REF>#plugins/expo"
    version: "<EXPO_VERSION>"

  compound-engineering:
    source: "git+https://github.com/EveryInc/compound-engineering-plugin.git@<CE_REF>#plugins/compound-engineering"
    version: "<CE_VERSION>"

  higgsfield:
    source: "git+https://github.com/higgsfield-ai/skills.git@<HIGGSFIELD_REF>"
    version: "<HIGGSFIELD_VERSION>"
```

- [ ] **Step 3: Validate**

Run: `"$AINFRA" --chdir "$CC" validate`
Expected: PASS. If it reports `remote plugin must pin an exact version`, a `version` field is missing or empty.

- [ ] **Step 4: Commit**

```bash
git -C "$CC" add ainfra.yaml
git -C "$CC" commit -m "Add plugins channel"
```

---

### Task 9: rules + tools channels

**Files:** Modify `$CC/ainfra.yaml`

- [ ] **Step 1: Locate the team CLAUDE.md file**

```bash
ls "$CC"/templates
```
Identify the team `CLAUDE.md` source (e.g. `templates/CLAUDE.md` or `templates/CLAUDE.md.template`). Use its repo-relative path as `<CLAUDE_MD_PATH>` below. If multiple exist, pick the team-wide (non-personal) one.

- [ ] **Step 2: Append the rules and tools blocks**

Append to `$CC/ainfra.yaml`, substituting `<CLAUDE_MD_PATH>`. The `tools` permission lists are copied verbatim from `claude-config/.claude-plugin/settings.json`:
```yaml
# --- Rules: the team CLAUDE.md ----------------------------------------------
# The live file is templated per developer ({{FULL_NAME}} ...). Per-developer
# rendering is sub-project #5; this entry references the source file as-is.
rules:
  team-claude-md:
    target: "~/.claude/CLAUDE.md"
    source: ./<CLAUDE_MD_PATH>
    version: "1"

# --- Tools: three-tier permission policy ------------------------------------
tools:
  permissions:
    deny:
      - "Read(**/.env*)"
      - "Read(**/secrets/**)"
      - "Read(**/*.pem)"
      - "Read(**/*.key)"
      - "Read(~/.ssh/**)"
      - "Read(~/.aws/**)"
    ask:
      - "Bash(rm -rf *)"
      - "Bash(git push --force *)"
      - "Bash(git reset --hard *)"
      - "Bash(git checkout -- *)"
      - "Bash(git clean *)"
      - "Bash(git branch -D *)"
      - "Bash(chmod 777 *)"
    allow:
      - "mcp__plugin_tvt-config_trein-vertraging-business-portal-db-prod__mysql_query"
      - "mcp__plugin_tvt-config_trein-vertraging-platform-db-prod__mysql_query"
      - "Bash(ddev *)"
      - "Bash(make *)"
      - "Bash(git status)"
      - "Bash(git status *)"
      - "Bash(git diff *)"
      - "Bash(git log *)"
      - "Bash(git show *)"
      - "Bash(git checkout *)"
      - "Bash(git branch *)"
      - "Bash(git switch *)"
      - "Bash(git push *)"
      - "Bash(git pull *)"
      - "Bash(git stash *)"
      - "Bash(git fetch *)"
      - "Bash(git add *)"
      - "Bash(git commit *)"
      - "Bash(git rebase *)"
      - "Bash(git merge *)"
      - "Bash(git worktree *)"
      - "Bash(git cherry-pick *)"
      - "Bash(git tag *)"
      - "Bash(git remote *)"
      - "Bash(git rev-parse *)"
      - "Bash(composer *)"
      - "Bash(npm *)"
      - "Bash(npx *)"
      - "Bash(./vendor/bin/pint *)"
      - "Bash(./vendor/bin/phpunit *)"
      - "Bash(php artisan test *)"
      - "Bash(php artisan *)"
      - "Bash(gh *)"
      - "Bash(aws *)"
      - "Bash(kubectl *)"
      - "Bash(ls *)"
      - "Bash(ls)"
      - "Bash(pwd)"
      - "Bash(cat *)"
      - "Bash(head *)"
      - "Bash(tail *)"
      - "Bash(wc *)"
      - "Bash(find *)"
      - "Bash(grep *)"
      - "Bash(sort *)"
      - "Bash(uniq *)"
      - "Bash(diff *)"
      - "Bash(sed *)"
      - "Bash(awk *)"
      - "Bash(xargs *)"
      - "Bash(tee *)"
      - "Bash(mkdir *)"
      - "Bash(cp *)"
      - "Bash(mv *)"
      - "Bash(touch *)"
      - "Bash(echo *)"
      - "Bash(curl *)"
      - "Bash(which *)"
      - "Bash(date *)"
      - "Bash(ssh trein-vertraging-platform-prod:*)"
      - "Bash(ssh trein-vertraging-business-portal-prod:*)"
```

- [ ] **Step 3: Validate**

Run: `"$AINFRA" --chdir "$CC" validate`
Expected: PASS. If it reports `rule declares no source`, `<CLAUDE_MD_PATH>` was not substituted.

- [ ] **Step 4: Commit**

```bash
git -C "$CC" add ainfra.yaml
git -C "$CC" commit -m "Add rules and tools channels"
```

---

### Task 10: personal layer content

**Files:** Modify `$CC/ainfra.personal.yaml`

- [ ] **Step 1: Append the personal secrets**

Append to `$CC/ainfra.personal.yaml`:
```yaml
secrets:
  linear-personal-api-key:
    mode: direct
    scope: personal
    ref: "op://Private/linear-personal-api-key/credential"
  x-oauth:
    mode: direct
    scope: personal
    ref: "op://Private/x-oauth/credential"
  posthog-personal-api-key:
    mode: direct
    scope: personal
    ref: "op://Private/posthog-personal-api-key/credential"
```

- [ ] **Step 2: Validate both layers together**

Run: `"$AINFRA" --chdir "$CC" validate`
Expected: PASS — `validate` loads `ainfra.yaml` and `ainfra.personal.yaml` together. The `posthog` server's `secret.apiKey: posthog-personal-api-key` reference (Task 5) is now satisfied by the personal layer.

- [ ] **Step 3: Commit the team file only**

`ainfra.personal.yaml` is gitignored — there is nothing to commit for this task. Confirm:
```bash
git -C "$CC" status --short
```
Expected: clean (no `ainfra.personal.yaml` listed).

---

### Task 11: run `ainfra lock` and capture evidence

**Files:** Possibly create `$CC/ainfra.lock`

- [ ] **Step 1: Run lock and capture output**

```bash
"$AINFRA" --chdir "$CC" lock 2>&1 | tee /tmp/ainfra-lock-output.txt
```
A partial failure is expected. Read the output carefully.

- [ ] **Step 2: If lock aborts on the first error, collect all errors iteratively**

If `lock` stops at the first unresolvable entry rather than reporting all of them: comment out that entry in `ainfra.yaml` (prefix its lines with `#` or temporarily remove the block), re-run `lock`, append the new error to `/tmp/ainfra-lock-output.txt`, and repeat until `lock` completes. Then restore every commented-out block. Keep a written list of which entry produced which error.

- [ ] **Step 3: Commit whatever locked**

If an `ainfra.lock` file was produced:
```bash
git -C "$CC" add ainfra.lock
git -C "$CC" commit -m "Add ainfra.lock (partial resolution)"
```
If no lock file was produced because nothing resolved, skip the commit and note that in Step 4's evidence.

- [ ] **Step 4: Save the evidence**

Keep `/tmp/ainfra-lock-output.txt` and the entry-to-error list — Task 13 consumes them.

---

### Task 12: run `ainfra plan` and capture evidence

**Files:** none.

- [ ] **Step 1: Run plan and capture output**

```bash
"$AINFRA" --chdir "$CC" plan 2>&1 | tee /tmp/ainfra-plan-output.txt
```
Expected: a diff for whatever resolved in Task 11, or a clear error if `plan` requires a complete lock. Either outcome is valid evidence.

- [ ] **Step 2: Save the evidence**

Keep `/tmp/ainfra-plan-output.txt` for Task 13.

---

### Task 13: rewrite the gap report

**Files:** Modify `docs/assessment-vs-real-config.md` (in **this** ainfra worktree, not `$CC`)

- [ ] **Step 1: Rewrite the assessment as an evidence-based gap report**

Edit `docs/assessment-vs-real-config.md`. Convert the paper "Coverage map" and "Gaps still open" sections into evidence-based ones:
- For each channel, state whether `ainfra validate` passed (it should, for all) and whether `ainfra lock` resolved it.
- For every entry that failed at `lock`, quote the real error text from `/tmp/ainfra-lock-output.txt` and map it to its sub-project: 1Password resolution → #2, remote plugin git fetch → #3, cliTool resolution → #4.
- Record the three intentional divergences from the live config: allocated tunnel ports vs. fixed 3307/3308; `@latest` servers pinned to explicit versions; `flare`/`intercom`/`stape` modelled as native http instead of `npx mcp-remote` stdio bridges.
- Note that the manifest now lives at the `claude-config` repo root (on branch `ainfra-manifest`) and reference its path.

- [ ] **Step 2: Commit (in the ainfra worktree)**

```bash
git add docs/assessment-vs-real-config.md
git commit -m "Rewrite tvt-config assessment as evidence-based gap report"
```

- [ ] **Step 3: Final verification**

```bash
"$AINFRA" --chdir "$CC" validate
```
Expected: PASS. This confirms the committed manifest is well-formed end to end.

---

## Self-review notes

- **Spec coverage:** Every mapping-table row in the design spec has a task — preconditions/cliTools (T2), secrets (T3, T10), templates+MySQL MCP (T4), inline MCP (T5), hooks (T6), commands (T7), plugins (T8), rules+tools (T9). Test procedure: validate (every task), lock (T11), plan (T12), gap report (T13).
- **Deliberate omissions verified:** no `scheduledJobs` task (no schema field — sub-project #6); no Cursor/IDE task (out of scope).
- **Cross-channel references checked:** `node`/`ssh` cliTools (T6) back the `requires` edges in hooks (T6) and the MySQL template (T4); `posthog-personal-api-key` is referenced in T5 and defined in T10; the MySQL template `id` `${instance.id}-tunnel` matches the `requires: - service:` edge in the same template body.
