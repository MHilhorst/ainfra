# Problem Space: AI Coding-Agent Configuration Management

## Executive Summary

Engineering teams adopting Claude Code, Cursor, Codex, and similar AI coding agents have outrun the tooling available to manage those agents' configuration. The core breakdown is the same one that motivated Terraform for cloud infrastructure: each developer's environment drifts from teammates', secrets end up in version-controlled plaintext, upstream tools change silently under pinned workflows, and there is no declarative manifest an org can treat as authoritative. The problems are not theoretical. GitHub issues with dozens of upvotes, published CVEs, a Register headline about ignored ignore-rules, and GitGuardian's count of 428 committed `settings.local.json` files on npm alone demonstrate that real teams are getting burned today. MCP-specific issues dominate the evidence; Skills and hooks issues are secondary but real and growing.

---

## The Twelve Pain Points

### 1. Drift Between Teammates

**Pain:** Developers on the same codebase get materially different Claude behavior because their local MCP servers, skills, hooks, and `CLAUDE.md` are out of sync.

> "Each repository in a multi-repo workspace can have its own `.claude/settings.json`, but as the workspace grows, the same configuration ends up duplicated across repos and diverges over time."
> — https://www.iamraghuveer.com/posts/shared-claude-settings-across-repos/

> "Wildcards don't match compound commands — `Bash(git:*)` fails to match `git add file && git commit -m 'message'`. The `*` only works within single simple commands. Critically broken since Claude constantly generates compound commands."
> — GitHub issue #30519, anthropics/claude-code https://github.com/anthropics/claude-code/issues/30519

The permissions issue compounds drift: a developer who has allowlisted `rg` in their `settings.local.json` has a different working Claude than the colleague who has not. Because `settings.local.json` is gitignored by default and `settings.json` accumulates organically, two teammates on the same project can have 50-entry allowlists that share fewer than half the same entries. The GitHub issue tracker documents 30+ open reports about permission matching across seven months with no upstream resolution milestone.

**Severity:** Confirmed, active, multi-repo. The vendor has acknowledged it but not resolved it.

---

### 2. Manual Setup Pain When Joining a Team

**Pain:** Onboarding a new developer to a team's Claude setup requires manual, undocumented steps that take hours to days.

> "When you hand Claude Code to a new developer, the real bottleneck usually is not installation, but explaining what matters in the repository. The onboarding takes weeks, and most of it is just learning how your team works through random Slack messages and pairing sessions."
> — https://www.buildthisnow.com/blog/guide/mechanics/team-onboarding

> "Deploying Claude Code with a shared claude.json cuts team onboarding from two hours to under 15 minutes — every developer gets the same MCP tools and system prompt automatically."
> — https://markaicode.com/howto/how-to-deploy-claude-code-in-teams/

The gap between "two hours" (with an explicit shared config) and "weeks" (without one) is telling. Anthropic shipped a `/team-onboarding` command in Claude Code v2.1.101 (April 2026) that generates a markdown guide from the last 30 days of session history. Its existence confirms the vendor recognizes the problem; the workaround-nature of the solution (generate a doc from one person's history, hand it to the new hire) confirms the root cause — no authoritative, machine-readable description of what a team's Claude setup actually is — remains unsolved.

**Severity:** Confirmed. Vendor has acknowledged it with a partial mitigation.

---

### 3. Rug-Pulls and Silent Upstream Changes

**Pain:** MCP servers invoked via `npx -y` pull the latest version on every startup, meaning tool behavior changes without any team action.

> "502 configurations use npx without version pinning, pulling whatever is latest on every agent startup… Without version pinning, your agent's capabilities change every time it restarts."
> — https://dev.to/0x711/mcp-has-a-supply-chain-problem-1nb8

> "The MCP spec still has no built-in mechanism for pinning or versioning tool descriptions, and if you're running MCP servers in production, you are trusting whatever the server sends at runtime. Editing a description can cause your AI agent to start behaving differently, even without schema changes."
> — https://nordicapis.com/the-weak-point-in-mcp-nobodys-talking-about-api-versioning/

> "A changed description can instruct the model to include sensitive data in responses, tell it to always call this tool before others, embed instructions to ignore user preferences."
> — https://medium.com/@binarEx/your-mcp-servers-tool-descriptions-changed-last-night-nobody-noticed-e3ad93cf6bc7

The supply-chain incident timeline is extensive. The September 2025 npm attack compromised `debug`, `chalk`, `ansi-styles`, and 15 other packages totalling 2 billion downloads per week — packages that were indirect dependencies of the official MCP TypeScript SDK, meaning every MCP server built from the SDK was potentially exposed. In October 2025, a path-traversal bug in Smithery's hosting infrastructure allowed exfiltration of Fly.io API tokens controlling 3,000+ hosted MCP servers. In April 2026, OX Security disclosed "the Mother of All AI Supply Chains" — a systemic STDIO transport flaw affecting 150+ million downloads with 30+ disclosures and 10+ CVEs.

**Severity:** Critical, documented with real CVEs and real incidents.

---

### 4. Secret Handling Chaos

**Pain:** GitHub tokens, API keys, and database credentials end up committed in plaintext in `.mcp.json`, `claude_desktop_config.json`, or `settings.local.json`.

> "Claude Code had created `.claude/settings.local.json`, and this file contained the leaked key reported by the security system, plus several other API tokens and test keys. Because `.claude/` is not gitignored by default, `settings.local.json` was committed and pushed to a public repository without any warning from Claude Code."
> — GitHub issue #13106, anthropics/claude-code https://github.com/anthropics/claude-code/issues/13106

> "Among about 46,500 monitored packages, 428 contained a `.claude/settings.local.json` file, and of those, 33 files across 30 packages included credentials. Exposed material included npm authentication tokens, plaintext npm login credentials, GitHub personal access tokens, Telegram bot tokens, and Hugging Face API tokens."
> — https://securitybrief.asia/story/claude-code-can-leak-secrets-in-public-npm-packages

> "It's a plaintext secret waiting to leak. Push that repo to GitHub, sync it to a teammate, or forget to `.gitignore` it, and your API key's gone."
> — https://1password.com/blog/securing-mcp-servers-with-1password-stop-credential-exposure-in-your-agent

The Register confirmed in January 2026 that Claude Code ignores `.claudeignore` and `.gitignore` rules designed to block access to `.env` files, and reproduces the issue against Claude Code v2.1.12. GitGuardian's 2026 State of Secrets Sprawl report counted 1.27 million AI-related credentials leaked across public GitHub in 2025. A GitHub feature request for `envFile` support in `.mcp.json` (issue #28942) to avoid inline credential embedding had no response at time of research.

**Severity:** Documented with real numbers, confirmed by The Register and GitGuardian. Active CVE exposure in the MCP ecosystem.

---

### 5. Fragmentation Across Tools

**Pain:** Teams using Claude Code, Cursor, Copilot, and Codex must maintain separate, overlapping rule files in incompatible formats.

> "The problem is fragmentation. Claude Code wants CLAUDE.md, Cursor wants .cursorrules, Copilot wants .github/copilot-instructions.md… Rewriting rules every time you switch tools is wasteful. This fragmentation creates a digital Tower of Babel in your repository."
> — https://www.deployhq.com/blog/ai-coding-config-files-guide

> "The downside is maintaining three files with potentially overlapping content… When standards need updating across your codebase, you must modify three separate files. A maintenance headache waiting to happen."
> — https://www.rulesync.dev/blog/claude-code-vs-cursor-rules-comparison

Active formats as of mid-2026: `CLAUDE.md` (Claude Code), `.cursor/rules/` (Cursor), `.github/copilot-instructions.md` (Copilot), `AGENTS.md` (proposed universal standard agreed by Google, OpenAI, Sourcegraph, Cursor, and Factory in 2026), `GEMINI.md` (Gemini CLI), `.windsurfrules` (Windsurf). In 2026, AGENTS.md emerged as a proposed universal standard, but adoption is incomplete and the existing installed base of `.cursorrules` and `CLAUDE.md` files means teams are managing a migration problem on top of the format problem.

**Severity:** Confirmed, structural. Third-party tools (RuleSync, manual generation scripts) have emerged to paper over it.

---

### 6. Skills and Commands Sharing

**Pain:** There is no standard mechanism for distributing custom slash commands or skills across a team; teams improvise with git submodules, dotfiles repos, or manual copy-paste.

> "To share Claude Code skills with your team, place skills in a `.claude/skills/` directory at the repository's root, then commit and push to Git… You can create a separate repo for your team skills and include it as a submodule: `git submodule add https://github.com/your-org/team-skills.git .claude/skills`"
> — https://www.agensi.io/learn/how-to-share-claude-code-skills-with-team

> "Submodules give a way to pin repo versions together but don't provide cross-repo coordination, task routing, or parallel agent execution, and don't compose well with worktrees essential for parallel agent work."
> — https://karun.me/blog/2026/03/26/structuring-claude-code-for-multi-repo-workspaces/

The community has converged on git submodules or dotfiles repos (e.g., `elizabethfuentes12/claude-code-dotfiles`, `zircote/.claude`, `atxtechbro/dotfiles`) as the de facto distribution mechanism. These work for global personal skills but break down at the team boundary: submodules require every project repo to be updated when skills change, `~/.claude` dotfiles are per-developer and not project-scoped, and there is no equivalent of an npm package registry for team-internal skills with versioning and access control.

**Severity:** Confirmed, with active workarounds. No vendor-first solution exists for team-scoped skills distribution with versioning.

---

### 7. Tool and Permission Allowlist Sprawl

**Pain:** The `settings.json` allowlist grows organically and inconsistently; different developers have different allowlists for the same project, and the matching engine itself is broken.

> "Most teams' settings.json grows organically. The first version is empty. The second version has `Bash(git status)` and `Bash(npm test:*)` because those are the two most-prompted commands. The fifth version has fifty entries and starts looking like a permission policy."
> — https://yaw.sh/claude-code-in-production/claude-code-settings/

> "Clicking 'Always Allow' on `git commit -m 'fix typo'` saves that exact string verbatim. Never matches again because it's too specific. `settings.local.json` accumulates hundreds of one-off dead rules while wildcard patterns remain unused."
> — GitHub issue #30519, anthropics/claude-code https://github.com/anthropics/claude-code/issues/30519

The VS Code extension additionally does not respect the allowlist from `settings.json` (issue #13788), meaning a developer switching between CLI and IDE gets prompted for tools they already approved. The core matching engine has 30+ open reports with one workaround suggestion from Anthropic in September 2025 and no resolution milestone.

**Severity:** Confirmed by issue volume and The Register coverage. The core mechanism that would allow teams to standardize permissions is itself unreliable.

---

### 8. MCP Server Proliferation and Quality

**Pain:** The MCP server ecosystem scaled from ~100 servers at launch to 16,670+ in under a year with inconsistent quality, no naming standards, and multiple competing registries.

> "There were only around 100 servers when MCP first arrived, but one MCP directory lists as many as 16,670 MCP servers as of September 2025 — a 16,000% increase in less than two years."
> — https://nordicapis.com/7-mcp-registries-worth-checking-out/

> "A fake Oura MCP project distributed via public MCP registries with fabricated credibility delivered StealC malware, harvesting developer credentials, browser passwords, API keys, and cryptocurrency wallets."
> — https://authzed.com/blog/timeline-mcp-breaches (February 2026)

> "OX Security successfully poisoned 9 of 11 MCP registries with a test payload and confirmed command execution on 6 live production platforms."
> — https://www.ox.security/blog/mcp-supply-chain-advisory-rce-vulnerabilities-across-the-ai-ecosystem/

The official MCP Registry launched in preview in September 2025 with automated uptime verification and community moderation. GitHub added internal MCP registry and allowlist controls for VS Code in November 2025. These are signals that the ecosystem recognized the problem; they are not evidence the problem is solved — the fake Oura attack occurred four months after the official registry launched.

**Severity:** Confirmed, active attack surface. Vendor and GitHub responding but incidents continuing into 2026.

---

### 9. Hook Portability

**Pain:** Hooks that shell out to `rg`, `gh`, `jq`, or bash scripts fail silently on teammates' machines when those tools are absent, or fail loudly on Windows where `/bin/bash` paths are invalid.

> "Plugin hooks that reference script files fail on Windows because Claude Code's /bin/bash cannot resolve Windows file paths in any format and cannot find files at forward-slash Windows paths."
> — GitHub issue #18610, anthropics/claude-code https://github.com/anthropics/claude-code/issues/18610

> "No hooks are working on Windows… I've tested out several combinations of hooks, but none of them works. Status: Closed as not planned."
> — GitHub issue #10450, anthropics/claude-code https://github.com/anthropics/claude-code/issues/10450

> "In April 2026, a one-line fix unlocked PowerShell on macOS and Linux — cross-platform support was always there, just never turned on."
> — GitHub issue #45963, anthropics/claude-code https://github.com/anthropics/claude-code/issues/45963

The workaround is to write hooks as `.mjs` files invoked via `node` rather than shell scripts. This requires every teammate to have Node.js available (guaranteed for Claude Code, since it requires Node), but it also requires teams to know this workaround, rewrite existing hooks, and ensure new hooks follow the pattern. None of this is enforced or documented as the default recommendation.

**Severity:** Confirmed on Windows (closed as not planned), partially mitigated. A shared hook that assumes bash/rg/gh will silently break for Windows teammates or anyone who has not installed those tools.

---

### 10. No Authoritative "What Is Installed" View

**Pain:** Developers cannot easily enumerate the MCP servers, skills, hooks, and commands that are active in their current session, with versions.

> "Run `/mcp` to see every configured server, its connection status, and whether you have approved it for the current project… If a server shows as connected but lists zero tools, run `claude --debug mcp` to see the server's stderr output."
> — https://code.claude.com/docs/en/debug-your-config

> "[BUG] `/mcp` command reports 'No MCP servers configured' despite valid configuration files in `~/.claude/`."
> — GitHub issue #12314, anthropics/claude-code https://github.com/anthropics/claude-code/issues/12314

Claude Code has `/mcp`, `/status`, `/doctor`, and `/context` commands that provide partial views. What they do not provide: a unified, machine-readable manifest that includes versions (most MCP servers are invoked via npx without pinning, so "version" is whatever landed on the last startup), the complete settings hierarchy showing which layer each rule came from, or a diff against a team baseline. The `/doctor` command is a diagnostic tool, not an inventory. Issue #12314 shows the diagnostic can itself be wrong.

**Severity:** Partially mitigated by built-in commands, but no version inventory and no team-baseline diff exist.

---

### 11. Existing Attempts to Solve This

Several tools and patterns have emerged, each covering a subset of the problem:

**Dotfiles repos** (`elizabethfuentes12/claude-code-dotfiles`, `atxtechbro/dotfiles`, `zircote/.claude`): Version-control `~/.claude` and sync it across machines via a shell wrapper. Works for individual developers. Does not handle team-scoped configs, project-specific overrides, secret injection, or versioned MCP servers.

**RuleSync** (`rulesync.dev`): Centrally manage rule files and generate tool-specific formats (CLAUDE.md, .cursorrules, copilot-instructions.md) from one canonical source. Solves fragmentation (pain point 5) only. Does not touch MCP, hooks, permissions, or secrets.

**`waynesutton/claude-code-sync`** (GitHub): Small script to propagate `settings.shared.json` across repos. Solves basic settings drift. No MCP server management, no lockfile, no secret handling.

**Desktop Extensions (`.mcpb`)**: Anthropic's own solution to the non-technical-user distribution problem. Bundles an MCP server and its dependencies into a single installable package. Does not solve the team config drift problem for developers, does not provide versioning or lockfiles.

**Managed settings (`managed-mcp.json`)**: Enterprise-tier Claude Code admin feature that lets admins define required/prohibited MCP servers. Closest to ainfra's target. Limited to Claude Code, requires Anthropic enterprise subscription, no cross-tool coverage.

**`mcp-scan` (Snyk Labs)**: CLI for detecting prompt injection in MCP tool descriptions. Security tool only, not a config-management tool.

Where they all stop short: none produces a declarative, lockfile-backed manifest that reconciles across Claude Code, Cursor, and Codex simultaneously. None handles secret injection. None surfaces version drift. None runs in CI.

---

### 12. Subscriber and Non-Engineer Distribution

**Pain:** Non-technical users (sales, support, operations) who use Claude Desktop cannot receive MCP server updates without manual JSON editing or IT intervention.

> "The previous MCP installation process created significant barriers for non-technical users: it required developer tools, manual JSON configuration, dependency management, and creates update complexity. These friction points meant that MCP servers, despite their power, remained largely inaccessible to non-technical users."
> — https://www.anthropic.com/engineering/desktop-extensions

> "Team and Enterprise plan owners can manage team access to desktop extensions by enabling or disabling public extensions, uploading custom extensions for one-click install, and transitioning to allowlists for stricter control."
> — Anthropic Desktop Extensions documentation

Anthropic's Desktop Extensions (`.mcpb` format) largely solve the installation UX problem for non-technical users. The gap that remains is the IT-managed update pipeline: how does an admin push an updated internal MCP server to 200 Claude Desktop instances across a company? The current answer is "re-publish the `.mcpb` and tell users to reinstall," which is manual. There is no MDM/endpoint-management integration documented.

**Severity:** Partially mitigated by Desktop Extensions. The update-and-rollout pipeline for enterprise-at-scale remains manual.

---

## Cross-Cutting Themes

**The manifest gap is foundational.** Every pain point above traces back to the same root: there is no canonical, versioned, machine-readable description of what a team's AI agent environment should look like. `settings.json` approximates this for permissions; `.mcp.json` approximates it for MCP servers. Neither is a lockfile, neither is cross-tool, and neither is authoritative — they're each a partial view maintained by convention, not enforced by tooling.

**MCP-specific issues dominate.** Supply-chain attacks, credential leakage, silent tool-description changes, registry proliferation, and version-pinning failures are all MCP-specific. Skills, hooks, and CLAUDE.md issues are real but secondary and mostly stem from lack of distribution tooling, not active security incidents.

**The permissions system is structurally broken.** The 30+ open issues on permission matching with no resolution timeline mean that even if a team writes a perfect `settings.json`, the runtime enforcement is unreliable. This makes the drift problem worse: team members cannot trust that a shared allowlist actually produces consistent behavior.

**Windows is a second-class citizen.** Hooks, bash-based MCP invocations, and path handling all break on Windows in documented ways. Teams with mixed OS environments face a hard choice between lowest-common-denominator hooks or platform-specific divergence.

---

## What Is NOT a Real Problem Yet

**"Different model versions per developer"** is commonly cited as a source of drift, but the evidence for this being a routine team problem is thin. Model selection in Claude Code is managed at the organization billing level and defaults consistently. Individual model overrides are possible but not commonly configured by accident.

**Skills/agents persona drift** is theoretically concerning but the evidence of teams actually being burned by divergent skill files is sparse. The ecosystem is young and most teams are still discovering skills exist. This is a forward-looking risk, not a current pain.

**Cross-agent orchestration config** (e.g., one team member using Claude as orchestrator, another using Cursor's agent mode) is discussed in architecture circles but there are no documented incidents of teams failing because of this. Premature scope for ainfra.

---

## Adjacent Tools and Gaps

| Tool | What It Covers | Where It Stops |
|---|---|---|
| RuleSync | Format translation: one source to CLAUDE.md / .cursorrules / copilot-instructions | No MCP, hooks, secrets, or lockfile |
| claude-code-dotfiles repos | Sync `~/.claude` across a developer's own machines | Per-developer, not team-scoped or project-scoped |
| waynesutton/claude-code-sync | Merge `settings.shared.json` across repos | No MCP, secrets, versions, or CI |
| Desktop Extensions (.mcpb) | Non-technical user MCP install UX | No update pipeline, dev-team config not addressed |
| managed-mcp.json | Enterprise admin can require/prohibit MCP servers | Claude Code only, enterprise tier, no lockfile |
| mcp-scan (Snyk) | Detect prompt injection in tool descriptions | Security audit, not config management |
| Ansible / Chef / Puppet | General software provisioning | Not MCP-aware, no Claude-specific concepts |

None of these tools produces a reproducible, lockfile-backed snapshot of a team's full AI agent environment that a new developer can apply in one command, that CI can validate, and that security can audit.

---

## Primary Evidence: A Team-Transition Thread (March 2026)

A March 2026 r/ClaudeCode thread — a solo power-user told to "make this work for a 5-person team" (51 upvotes, 42 comments) — is unusually clean primary evidence for this problem space, because it is practitioners describing what they actually built, not vendors or analysts describing what they sell.

> "How are you actually using Claude Code as a team? … if I learn something important in my Claude Code session, how does that knowledge get to everyone else? … How do you keep things consistent? Like making sure Claude gives similar quality output for everyone on the team and not just the one person who knows how to prompt it well."
> — u/Azrael_666 (OP)
> https://www.reddit.com/r/ClaudeCode/comments/1rhswxk/how_are_you_actually_using_claude_code_as_a_team/

**What it confirms.** The single most-repeated answer in the thread is ainfra's pain point 6 (team-scoped skills/commands distribution with versioning). Multiple teams describe independently hand-building a subset of ainfra:

> "We built an internal Claude plugin based on our development standards docs + a few popular plugins (eg superpowers), and accept PRs to it for adding new skills for workflows, and also keep a Claude.md alongside the readme.md for all repos."
> — u/boatsnbros

> "A single skills.md repo with 'this is how we do auth', 'this is how you should write test reports' etc., and a sync script helps enforce consistency across the sdlc."
> — u/italian-sausage-nerd

> "Push plugins out onto the private marketplace you have available on the Team account. Plugins combine commands, skills, and hooks as needed and can be versioned by the admin."
> — u/mbcoalson

The thread also independently surfaces the personal-vs-team layer split (an ainfra feature not currently itemized as a pain point above):

> "Isolating personal preferences from project level architecture… maintain a CLAUDE.local.md for your personal preferences while having the versioned Claude.md for the team."
> — u/HaagNDaazer

**What it challenges in this document.** Two of our framings look weaker against this evidence:

- The "What Is NOT a Real Problem Yet" section calls skills/persona drift "a forward-looking risk, not a current pain." The thread shows multiple teams distributing and version-reviewing skills *today* and treating consistency across that distribution as their primary problem. The distribution half of this is a current pain, not a forward-looking one; only the "personas silently diverge and burn someone" failure mode is still forward-looking.
- Pain point 2 (onboarding) is framed here as machine provisioning ("2 hours → 15 minutes"). The thread's dominant onboarding answer is human pairing ("pair sessions beat docs… watching, not reading"). ainfra reproduces a machine in one command but does not address the teaching half — this is a scope boundary worth stating explicitly rather than implying onboarding is solved.

**What it surfaces that this document does not cover.** Two pains appear in the thread that are absent from the twelve above:

- *Decision history vs. standing rules.* "A skill encodes the current right answer but doesn't tell you it used to be a different answer six weeks ago, or which work was built on the old assumption. Most teams I've seen … hit that wall around month three." — u/Substantial_Doubt139. A content-hashed lockfile versions config *state*, not the *rationale and dependency trail* behind a changed decision. Currently unaddressed by ainfra.
- *Multi-agent runtime coordination.* "The bottleneck shifted from 'how fast can one person code' to 'how do multiple agents stay coherent' … git conflicts and deploy races when multiple agents pushed to main." — u/ultrathink-art. This document already disclaims orchestration as "premature scope"; the thread confirms it is a live, upvoted pain, so the disclaimer is a deliberate boundary, not an evidence gap.

**Emphasis note.** Notably, *none* of the thread's 42 comments mentions MCP supply-chain attacks, secret leakage, or version-pinning (pain points 3, 4, 8 — the "Critical" severity items here). This audience asks for team enablement (sharing, consistency, onboarding, coordination); the security pains are real and well-sourced but are not what this persona leads with. The two framings meet at pain point 6.

---

## Sources

- https://www.reddit.com/r/ClaudeCode/comments/1rhswxk/how_are_you_actually_using_claude_code_as_a_team/
- https://www.iamraghuveer.com/posts/shared-claude-settings-across-repos/
- https://github.com/anthropics/claude-code/issues/59368
- https://github.com/anthropics/claude-code/issues/13106
- https://github.com/anthropics/claude-code/issues/10450
- https://github.com/anthropics/claude-code/issues/18610
- https://github.com/anthropics/claude-code/issues/45963
- https://github.com/anthropics/claude-code/issues/12314
- https://github.com/anthropics/claude-code/issues/30519
- https://github.com/anthropics/claude-code/issues/13788
- https://github.com/anthropics/claude-code/issues/28942
- https://github.com/anthropics/claude-code/issues/2142
- https://github.com/anthropics/claude-code/issues/29910
- https://www.theregister.com/2026/01/28/claude_code_ai_secrets_files/
- https://securitybrief.asia/story/claude-code-can-leak-secrets-in-public-npm-packages
- https://1password.com/blog/securing-mcp-servers-with-1password-stop-credential-exposure-in-your-agent
- https://authzed.com/blog/timeline-mcp-breaches
- https://dev.to/stacklok/examining-the-impact-of-npm-supply-chain-attacks-on-mcp-edo
- https://www.ox.security/blog/mcp-supply-chain-advisory-rce-vulnerabilities-across-the-ai-ecosystem/
- https://www.securityweek.com/by-design-flaw-in-mcp-could-enable-widespread-ai-supply-chain-attacks/
- https://labs.snyk.io/resources/detect-tool-poisoning-mcp-server-security/
- https://medium.com/@binarEx/your-mcp-servers-tool-descriptions-changed-last-night-nobody-noticed-e3ad93cf6bc7
- https://nordicapis.com/the-weak-point-in-mcp-nobodys-talking-about-api-versioning/
- https://nordicapis.com/7-mcp-registries-worth-checking-out/
- https://blog.modelcontextprotocol.io/posts/2025-09-08-mcp-registry-preview/
- https://dev.to/0x711/mcp-has-a-supply-chain-problem-1nb8
- https://www.anthropic.com/engineering/desktop-extensions
- https://www.deployhq.com/blog/ai-coding-config-files-guide
- https://www.rulesync.dev/blog/claude-code-vs-cursor-rules-comparison
- https://thepromptshelf.dev/blog/cursorrules-vs-claude-md/
- https://www.agensi.io/learn/how-to-share-claude-code-skills-with-team
- https://karun.me/blog/2026/03/26/structuring-claude-code-for-multi-repo-workspaces/
- https://github.com/elizabethfuentes12/claude-code-dotfiles
- https://github.com/waynesutton/claude-code-sync
- https://www.buildthisnow.com/blog/guide/mechanics/team-onboarding
- https://medium.com/@joe.njenga/i-tested-claude-code-team-onboarding-and-it-fixes-team-setup-chaos-1111b15f2f18
- https://markaicode.com/howto/how-to-deploy-claude-code-in-teams/
- https://yaw.sh/claude-code-in-production/claude-code-settings/
- https://code.claude.com/docs/en/debug-your-config
