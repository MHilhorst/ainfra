---
layout: home

hero:
  name: ainfra
  text: Keep your team's AI tooling in sync.
  tagline: A package manager for your team's AI development setup — declarative manifest, content-hashed lockfile, the same verbs you already know from npm and brew.
  actions:
    - theme: brand
      text: Quick Start
      link: /quickstart
    - theme: alt
      text: View on GitHub
      link: https://github.com/MHilhorst/ainfra

features:
  - title: One manifest, every machine
    details: Declare MCP servers, hooks, skills, plugins, and CLAUDE.md in ainfra.yaml. Commit it. Anyone running ainfra install gets the same setup.
  - title: Lockfile catches drift
    details: Content-hashed lockfile pins exact versions. ainfra install --dry-run --strict in CI fails the build if a developer's machine has wandered.
  - title: Native files, no lock-in
    details: ainfra writes the same .mcp.json, .claude/, and CLAUDE.md your tools already read. Stop using ainfra tomorrow and everything it wrote still works.
  - title: Adopt an existing repo
    details: ainfra adopt scans your current .mcp.json, .claude/, and CLAUDE.md and reverse-engineers a manifest. No clean-slate migration.
  - title: Claude Code and Codex
    details: One tool for both. Same manifest, same lockfile, rendered to the right place for each agent.
  - title: Drift detection on session start
    details: A SessionStart hook tells you the moment your local config diverges from the committed manifest.
---

## Install

```sh
brew install MHilhorst/ainfra/ainfra
```

Or `go install github.com/MHilhorst/ainfra/cmd/ainfra@latest`.

## At a glance

```yaml
# ainfra.yaml — committed to your repo
version: 1

mcpServers:
  github:
    transport: stdio
    command: npx
    args: ["-y", "@modelcontextprotocol/server-github"]
    version: "0.6.2"
    secret:
      token: github-token

hooks:
  gofmt-after-edit:
    event: PostToolUse
    matcher: "Edit|Write"
    command: "gofmt -w ."
```

```sh
git clone <org/repo> && cd <repo>
ainfra install              # reconcile your machine to the manifest
ainfra install --dry-run    # preview without writing
```

Head to the [Quick Start](/quickstart) for the full walkthrough, or read the [design](/reference/design) for how it works under the hood.
