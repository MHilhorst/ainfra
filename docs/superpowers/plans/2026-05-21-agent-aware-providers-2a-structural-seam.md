# Agent-Aware Providers — Plan 2a: The Structural Seam

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `ainfra plan`/`apply`/`check` resolve the target agent and reconcile through that agent's provider set — with zero behavior change, since `agent` defaults to `claude-code`.

**Architecture:** The Phase 3 channel providers are regrouped by whether they are agent-specific: the eight Claude Code providers move to `internal/provider/claudecode/`, the agent-agnostic `cliTools` provider moves to `internal/provider/shared/`. A new `internal/provider/agentset` package exposes `ForAgent(agent.ID)`, which assembles the provider set. The `plan`/`apply`/`check` commands resolve the agent with `manifest.ResolveAgent` and call `ForAgent`. The `Provider` interface and the orchestrator are untouched.

**Tech Stack:** Go 1.25, standard library. Tests are standard `go test`. macOS `sed` (the `-i ''` form).

**Spec:** `docs/superpowers/specs/2026-05-21-agent-aware-providers-design.md`. This is Plan 2a of two; Plan 2b adds the Codex provider set.

**Deviation from the spec, intentional:** the spec §1 wrote the constructor as `provider.ForAgent(id, env)`. Two corrections: (1) it cannot live in package `provider` — `claudecode` imports `provider`, so `provider` importing `claudecode` is an import cycle; it lives in a sibling package `internal/provider/agentset`. (2) The `env` parameter is dropped — every channel provider is a zero-value struct needing no construction-time data (YAGNI). Final signature: `agentset.ForAgent(id agent.ID) ([]provider.Provider, error)`.

---

## File Structure

**Moved:**
- `internal/provider/channels/clitools.go` + `clitools_test.go` → `internal/provider/shared/` (package `shared`) — the agent-agnostic substrate provider.
- `internal/provider/channels/{mcp,hooks,commands,rules,skills,plugins,services,tools}.go` + their `_test.go` → `internal/provider/claudecode/` (package `claudecode`) — the eight Claude Code providers. The `internal/provider/channels/` directory disappears once empty.

**Created:**
- `internal/provider/agentset/agentset.go` — `ForAgent(agent.ID)`, the provider-set constructor. Imports `provider`, `claudecode`, `shared`, `agent`.
- `internal/provider/agentset/agentset_test.go` — tests for `ForAgent`.

**Modified:**
- `cmd/ainfra/reconcile.go` — `allProviders()` is replaced by `providersForDir(dir)`, which resolves the agent and calls `agentset.ForAgent`.
- `cmd/ainfra/reconcile_test.go` — the two `TestAllProviders_*` tests are replaced by one test of `providersForDir`.
- `cmd/ainfra/commands.go` — the three `NewOrchestrator(..., allProviders())` call sites become agent-aware.

---

## Task 1: Move the cliTools provider into a `shared` package

**Files:**
- Move: `internal/provider/channels/clitools.go` → `internal/provider/shared/clitools.go`
- Move: `internal/provider/channels/clitools_test.go` → `internal/provider/shared/clitools_test.go`
- Modify: `cmd/ainfra/reconcile.go`

- [ ] **Step 1: Move the two files with git**

Run from the repo root:
```bash
mkdir -p internal/provider/shared
git mv internal/provider/channels/clitools.go internal/provider/shared/clitools.go
git mv internal/provider/channels/clitools_test.go internal/provider/shared/clitools_test.go
```

- [ ] **Step 2: Rewrite the package clause and qualifiers in the moved files**

Run:
```bash
sed -i '' -e 's/^package channels$/package shared/' internal/provider/shared/clitools.go
sed -i '' \
  -e 's/^package channels_test$/package shared_test/' \
  -e 's#MHilhorst/ainfra/internal/provider/channels#MHilhorst/ainfra/internal/provider/shared#' \
  -e 's/channels\.CLITools/shared.CLITools/g' \
  internal/provider/shared/clitools_test.go
```

- [ ] **Step 3: Run the build to confirm it now fails**

Run: `go build ./...`
Expected: FAIL — `cmd/ainfra/reconcile.go` still references `channels.CLITools`, which no longer exists in the `channels` package.

- [ ] **Step 4: Update `reconcile.go` to use `shared.CLITools`**

In `cmd/ainfra/reconcile.go`, add the `shared` import and change the `cliTools` entry. The import block and `allProviders` become:

```go
import (
	"os"

	"github.com/MHilhorst/ainfra/internal/provider"
	"github.com/MHilhorst/ainfra/internal/provider/channels"
	"github.com/MHilhorst/ainfra/internal/provider/fetch"
	"github.com/MHilhorst/ainfra/internal/provider/shared"
)

// allProviders returns all nine channel providers in a stable order.
func allProviders() []provider.Provider {
	return []provider.Provider{
		channels.MCP{},
		channels.Hooks{},
		channels.Commands{},
		channels.Rules{},
		channels.Skills{},
		channels.Plugins{},
		shared.CLITools{},
		channels.Services{},
		channels.Tools{},
	}
}
```

Leave `buildEnv` unchanged.

- [ ] **Step 5: Build and test**

Run: `go build ./... && go test ./...`
Expected: PASS — every package, including the relocated `internal/provider/shared`.

- [ ] **Step 6: Commit**

```bash
git add internal/provider/shared internal/provider/channels cmd/ainfra/reconcile.go
git commit -m "Move the cliTools provider into a shared package"
```

---

## Task 2: Move the eight Claude Code providers into a `claudecode` package

**Files:**
- Move: `internal/provider/channels/{mcp,hooks,commands,rules,skills,plugins,services,tools}.go` and their `_test.go` files → `internal/provider/claudecode/`
- Modify: `internal/provider/claudecode/mcp.go` (package doc comment)
- Modify: `cmd/ainfra/reconcile.go`

- [ ] **Step 1: Move the sixteen files with git**

Run from the repo root:
```bash
mkdir -p internal/provider/claudecode
for n in mcp hooks commands rules skills plugins services tools; do
  git mv internal/provider/channels/$n.go internal/provider/claudecode/$n.go
  git mv internal/provider/channels/${n}_test.go internal/provider/claudecode/${n}_test.go
done
```

After this the `internal/provider/channels/` directory is empty (git does not track empty directories, so it simply disappears).

- [ ] **Step 2: Rewrite package clauses and qualifiers in the moved files**

Run:
```bash
sed -i '' -e 's/^package channels$/package claudecode/' internal/provider/claudecode/*.go
sed -i '' \
  -e 's/^package channels_test$/package claudecode_test/' \
  -e 's#MHilhorst/ainfra/internal/provider/channels#MHilhorst/ainfra/internal/provider/claudecode#' \
  -e 's/channels\./claudecode./g' \
  internal/provider/claudecode/*_test.go
```

The first `sed` also passes over the `*_test.go` files, but `^package channels$` does not match their `package channels_test` line, so only the production files' package clause changes. The second `sed` fixes the test files.

- [ ] **Step 3: Fix the package doc comment in `mcp.go`**

`internal/provider/claudecode/mcp.go` begins with a doc comment that still names the old package. Replace it. Change:

```go
// Package channels contains filesystem-channel providers for the ainfra
// reconciler. Each type implements provider.Provider for one channel kind.
package claudecode
```

to:

```go
// Package claudecode contains the Claude Code channel providers for the ainfra
// reconciler. Each type implements provider.Provider for one channel kind.
package claudecode
```

- [ ] **Step 4: Run the build to confirm it now fails**

Run: `go build ./...`
Expected: FAIL — `cmd/ainfra/reconcile.go` still references `channels.MCP`, `channels.Hooks`, etc., and the `channels` package no longer exists.

- [ ] **Step 5: Update `reconcile.go` to use `claudecode.*`**

In `cmd/ainfra/reconcile.go`, replace the `channels` import with `claudecode` and update `allProviders`. The import block and `allProviders` become:

```go
import (
	"os"

	"github.com/MHilhorst/ainfra/internal/provider"
	"github.com/MHilhorst/ainfra/internal/provider/claudecode"
	"github.com/MHilhorst/ainfra/internal/provider/fetch"
	"github.com/MHilhorst/ainfra/internal/provider/shared"
)

// allProviders returns all nine channel providers in a stable order.
func allProviders() []provider.Provider {
	return []provider.Provider{
		claudecode.MCP{},
		claudecode.Hooks{},
		claudecode.Commands{},
		claudecode.Rules{},
		claudecode.Skills{},
		claudecode.Plugins{},
		shared.CLITools{},
		claudecode.Services{},
		claudecode.Tools{},
	}
}
```

- [ ] **Step 6: Format, build, and test**

Run: `gofmt -w internal/provider/claudecode/ cmd/ainfra/reconcile.go && go build ./... && go test ./...`
Expected: PASS — every package, including the relocated `internal/provider/claudecode`.

- [ ] **Step 7: Commit**

```bash
git add internal/provider/claudecode internal/provider/channels cmd/ainfra/reconcile.go
git commit -m "Move the Claude Code channel providers into a claudecode package"
```

---

## Task 3: Add the `agentset` package with `ForAgent`

**Files:**
- Create: `internal/provider/agentset/agentset.go`
- Test: `internal/provider/agentset/agentset_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/provider/agentset/agentset_test.go`:

```go
package agentset_test

import (
	"testing"

	"github.com/MHilhorst/ainfra/internal/agent"
	"github.com/MHilhorst/ainfra/internal/provider/agentset"
)

func TestForAgentClaudeCodeReturnsEveryChannel(t *testing.T) {
	ps, err := agentset.ForAgent(agent.ClaudeCode)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string]bool{
		"mcpServers": true, "hooks": true, "commands": true, "rules": true,
		"skills": true, "plugins": true, "backgroundServices": true,
		"tools": true, "cliTools": true,
	}
	got := map[string]bool{}
	for _, p := range ps {
		ch := p.Channel()
		if got[ch] {
			t.Errorf("duplicate channel %q", ch)
		}
		got[ch] = true
	}
	if len(got) != len(want) {
		t.Fatalf("got %d distinct channels, want %d", len(got), len(want))
	}
	for ch := range want {
		if !got[ch] {
			t.Errorf("missing channel %q", ch)
		}
	}
}

func TestForAgentCodexNotYetAvailable(t *testing.T) {
	if _, err := agentset.ForAgent(agent.Codex); err == nil {
		t.Error("expected an error for the codex set (built in plan 2b), got nil")
	}
}

func TestForAgentUnknownErrors(t *testing.T) {
	if _, err := agentset.ForAgent(agent.ID("emacs-doctor")); err == nil {
		t.Error("expected an error for an unknown agent, got nil")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/provider/agentset/`
Expected: FAIL — build error, the `agentset` package does not exist yet.

- [ ] **Step 3: Write the implementation**

Create `internal/provider/agentset/agentset.go`:

```go
// Package agentset assembles the channel provider set for a target agent. It
// is the seam that makes reconciliation agent-aware: the plan/apply/check
// commands resolve the agent and call ForAgent to get the providers to run.
package agentset

import (
	"fmt"

	"github.com/MHilhorst/ainfra/internal/agent"
	"github.com/MHilhorst/ainfra/internal/provider"
	"github.com/MHilhorst/ainfra/internal/provider/claudecode"
	"github.com/MHilhorst/ainfra/internal/provider/shared"
)

// sharedProviders are the agent-agnostic providers every agent set includes.
func sharedProviders() []provider.Provider {
	return []provider.Provider{shared.CLITools{}}
}

// ForAgent returns the channel providers that reconcile config for the given
// agent: the agent-specific providers plus the shared, agent-agnostic ones.
// An agent with no provider set is an error; manifest validation rejects an
// unknown agent earlier, so this is a defence-in-depth backstop, never a
// silent empty set.
func ForAgent(id agent.ID) ([]provider.Provider, error) {
	switch id {
	case agent.ClaudeCode:
		return append([]provider.Provider{
			claudecode.MCP{},
			claudecode.Hooks{},
			claudecode.Commands{},
			claudecode.Rules{},
			claudecode.Skills{},
			claudecode.Plugins{},
			claudecode.Services{},
			claudecode.Tools{},
		}, sharedProviders()...), nil
	default:
		return nil, fmt.Errorf("no provider set for agent %q", id)
	}
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/provider/agentset/`
Expected: PASS — all three tests.

- [ ] **Step 5: Commit**

```bash
git add internal/provider/agentset
git commit -m "Add the agentset package and the ForAgent provider-set constructor"
```

---

## Task 4: Wire the commands to resolve the agent and use `ForAgent`

**Files:**
- Modify: `cmd/ainfra/reconcile.go`
- Modify: `cmd/ainfra/reconcile_test.go`
- Modify: `cmd/ainfra/commands.go`

- [ ] **Step 1: Replace the `reconcile_test.go` provider tests**

In `cmd/ainfra/reconcile_test.go`, delete the two functions `TestAllProviders_Count` and `TestAllProviders_ChannelNames` entirely, and add `TestProvidersForDir_DefaultsToClaudeCode`. Keep `TestBuildEnv_Fields` exactly as it is. The file becomes:

```go
package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProvidersForDir_DefaultsToClaudeCode(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte("version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	providers, err := providersForDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(providers) != 9 {
		t.Fatalf("providersForDir returned %d providers, want 9 (claude-code default)", len(providers))
	}
}

func TestBuildEnv_Fields(t *testing.T) {
	dir := t.TempDir()
	env := buildEnv(dir)

	if env.Root != dir {
		t.Errorf("Root = %q, want %q", env.Root, dir)
	}
	if env.FS == nil {
		t.Error("FS is nil, want non-nil")
	}
	if env.Runner == nil {
		t.Error("Runner is nil, want non-nil")
	}
	if env.Fetch == nil {
		t.Error("Fetch is nil, want non-nil")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./cmd/ainfra/ -run TestProvidersForDir_DefaultsToClaudeCode`
Expected: FAIL — build error, `providersForDir` is undefined.

- [ ] **Step 3: Replace `allProviders` with `providersForDir` in `reconcile.go`**

Rewrite `cmd/ainfra/reconcile.go` in full:

```go
package main

import (
	"os"

	"github.com/MHilhorst/ainfra/internal/agent"
	"github.com/MHilhorst/ainfra/internal/manifest"
	"github.com/MHilhorst/ainfra/internal/provider"
	"github.com/MHilhorst/ainfra/internal/provider/agentset"
	"github.com/MHilhorst/ainfra/internal/provider/fetch"
)

// providersForDir resolves the target agent from the manifest layers at dir
// and returns the channel provider set that reconciles config for that agent.
func providersForDir(dir string) ([]provider.Provider, error) {
	layers, err := manifest.LoadLayers(dir)
	if err != nil {
		return nil, err
	}
	id, _, _ := manifest.ResolveAgent(layers)
	return agentset.ForAgent(agent.ID(id))
}

// buildEnv constructs the provider.Env for a given repo root directory.
func buildEnv(dir string) provider.Env {
	home, _ := os.UserHomeDir()
	return provider.Env{
		FS:     provider.OSFilesystem{},
		Runner: provider.ExecRunner{},
		Fetch:  fetch.LocalFetcher{Root: dir},
		Root:   dir,
		Home:   home,
	}
}
```

- [ ] **Step 4: Update the three orchestrator call sites in `commands.go`**

In `cmd/ainfra/commands.go` there are three identical lines:

```go
	orch := provider.NewOrchestrator(dir, buildEnv(dir), allProviders())
```

one each in `runPlan`, `runApply`, and `runCheck`. Replace **each** of the three with this block (the `errColor` variable already exists in all three functions):

```go
	providers, err := providersForDir(dir)
	if err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}
	orch := provider.NewOrchestrator(dir, buildEnv(dir), providers)
```

In all three functions `err` is already declared earlier, and `providers` is new, so `:=` is valid. No import changes are needed in `commands.go` — `provider`, `ui`, and the rest are already imported.

- [ ] **Step 5: Build and run the cmd/ainfra tests**

Run: `go build ./... && go test ./cmd/ainfra/`
Expected: PASS — `TestProvidersForDir_DefaultsToClaudeCode`, `TestBuildEnv_Fields`, and the existing `plan`/`apply`/`check`/e2e command tests, which are unchanged because the default agent reproduces the previous nine-provider behavior.

- [ ] **Step 6: Commit**

```bash
git add cmd/ainfra/reconcile.go cmd/ainfra/reconcile_test.go cmd/ainfra/commands.go
git commit -m "Resolve the target agent in plan, apply, and check"
```

---

## Task 5: Full build, test, and behavior verification

**Files:** none — this task verifies the whole change.

- [ ] **Step 1: Build everything**

Run: `go build ./...`
Expected: no output, exit 0.

- [ ] **Step 2: Run the full test suite**

Run: `go test ./...`
Expected: every package reports `ok`, including `internal/provider/claudecode`, `internal/provider/shared`, and `internal/provider/agentset`. The `internal/provider/channels` package no longer exists and no test references it.

- [ ] **Step 3: Confirm the `channels` package is fully gone**

Run: `test ! -d internal/provider/channels && echo "channels package removed"`
Expected: prints `channels package removed`.

Run: `grep -rn "provider/channels" --include=*.go . ; echo "exit=$?"`
Expected: `exit=1` — no remaining reference to the old import path anywhere.

- [ ] **Step 4: Verify `plan` still works end to end on the showcase manifest**

Run:
```bash
go build -o /tmp/ainfra-2a ./cmd/ainfra
/tmp/ainfra-2a lock
/tmp/ainfra-2a plan
echo "exit=$?"
```
Expected: `lock` writes `ainfra.lock`; `plan` prints a diff (or "no changes") and exits 0. The repo-root showcase manifest declares no `agent`, so it resolves to `claude-code` and reconciles through the same nine providers as before this plan — behavior is unchanged.

- [ ] **Step 5: Clean up**

Run: `rm -f /tmp/ainfra-2a ainfra.lock ainfra.personal.lock`
Expected: no output. (Removes the build artifact and the lockfiles written by the Step 4 check so the working tree is clean.)

- [ ] **Step 6: Final commit (only if Steps 1-2 required any fix)**

If Steps 1-3 surfaced a failure that needed a fix, commit it:

```bash
git add -A
git commit -m "Fix build and test fallout from the provider regrouping"
```

If Steps 1-3 passed clean, there is nothing to commit — skip this step.

---

## Self-Review

**Spec coverage** (against `2026-05-21-agent-aware-providers-design.md`):
- §1 `Provider` unchanged; agent-awareness in the commands + a set constructor → Tasks 3, 4. The orchestrator is untouched, as the spec states.
- §2 package structure (`claudecode/`, `shared/`) → Tasks 1, 2. The `codex/` package is Plan 2b, out of scope here.
- §2 `ForAgent` constructor → Task 3 (in `agentset`, not `provider` — see the documented deviation).
- §3 data flow — command resolves agent, builds set, hands it to `NewOrchestrator` → Task 4.
- §6 Plan 2a = the structural seam, zero behavior change → the whole plan; verified in Task 5 Step 4.
- §8 testing for 2a — existing suites pass after the move; `ForAgent` per-agent tests; `plan` on a claude-code manifest unchanged → Tasks 3, 4, 5.
- §5 (Codex set), §2 `codex/` package → explicitly Plan 2b, not covered here.

**Placeholder scan:** none — every step has concrete code, an exact command, or an exact edit.

**Type consistency:** `agentset.ForAgent(agent.ID) ([]provider.Provider, error)` is defined in Task 3 and called in Task 4 with that exact signature. `providersForDir(dir string) ([]provider.Provider, error)` is defined in Task 4 Step 3 and called in Task 4 Steps 1 and 4 with that signature. `manifest.ResolveAgent` returns `(string, manifest.Layer, bool)` — Task 4 consumes the first value and converts it with `agent.ID(id)`. The provider type names (`claudecode.MCP`, `shared.CLITools`, etc.) are consistent between Tasks 1-2 (the move) and Task 3 (`ForAgent`).
