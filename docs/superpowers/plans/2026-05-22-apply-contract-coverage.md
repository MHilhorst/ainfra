# Apply Contract & Fixture Coverage Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the apply-path test gap — add a broad representative end-to-end test and a render↔channel contract test, so a renderer/provider payload mismatch is caught by a failing test instead of shipping behind a green `validate`/`lock`/`plan`.

**Architecture:** Two independent test additions, no production code change. (1) A `testdata/` fixture manifest that exercises many channels at once, driven through the real `lock → plan → apply → check → plan` cycle by a new e2e test. (2) A table-driven contract test that runs the real `resolve.RenderResources` and feeds each rendered resource straight into its channel provider's `Apply`, asserting the payload is consumed (not silently dropped by a bad type assertion).

**Tech Stack:** Go 1.25, standard library. Tests use the `run()` CLI harness (`cmd/ainfra`), `resolve.RenderResources`, `agentset.ForAgent`, and the `MemFilesystem`/`FakeRunner`/`FakeFetcher` fakes.

**Context:** This is item #2 of `docs/superpowers/specs/2026-05-22-apply-hardening-design.md`. Items #5 and #1 are already merged. The motivating history: two bugs (a `cliTools` payload type assertion, a `rules` target bug) shipped because `validate`/`lock`/`plan` never exercise `apply`. Today `cmd/ainfra/e2e_test.go` has three e2e tests covering only hooks+commands, tools, and codex(mcp+rules). No e2e covers `cliTools`, an inline `mcpServers` entry on the claude-code agent, a templated MCP server, `skills`, `backgroundServices`, a `rules` entry with a non-default `target:`, or a hook with a `requires:` edge.

---

### Task 1: Representative fixture and broad end-to-end test

Add a `testdata/` manifest that exercises many channels in one apply, and an e2e test that drives it through the full pipeline. The test uses `apply --yes --no-install` so the `cliTools` channel is exercised (rendered, planned, applied) without running a real `brew`/`npm`. `marketplaces` and `plugins` are intentionally excluded — their `Apply` shells out to the `claude` CLI, which is not available in a test; they keep their existing unit-test coverage.

**Files:**
- Create: `cmd/ainfra/testdata/representative/ainfra.yaml`
- Create: `cmd/ainfra/testdata/representative/commands/greet.md`
- Create: `cmd/ainfra/testdata/representative/hooks/audit.sh`
- Create: `cmd/ainfra/testdata/representative/rules/context.md`
- Test: `cmd/ainfra/e2e_test.go` (append)

- [ ] **Step 1: Write the failing test**

Append to `cmd/ainfra/e2e_test.go` (its imports — `bytes`, `encoding/json`, `os`, `path/filepath`, `strings`, `testing` — already cover this):

```go
// copyTestdata recursively copies a testdata fixture directory into dst so the
// test can run lock/apply against a writable working tree.
func copyTestdata(t *testing.T, src, dst string) {
	t.Helper()
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatalf("read fixture %s: %v", src, err)
	}
	for _, e := range entries {
		s := filepath.Join(src, e.Name())
		d := filepath.Join(dst, e.Name())
		if e.IsDir() {
			if err := os.MkdirAll(d, 0o755); err != nil {
				t.Fatal(err)
			}
			copyTestdata(t, s, d)
			continue
		}
		data, err := os.ReadFile(s)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(d, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// TestE2ERepresentative drives a deliberately broad manifest — cliTools, an
// inline MCP server, a templated MCP server, a non-default-target rule, a hook
// with a requires: edge, and a command — through the full reconcile cycle.
// apply runs with --no-install so the cliTool is exercised without a real
// package manager.
func TestE2ERepresentative(t *testing.T) {
	dir := t.TempDir()
	copyTestdata(t, filepath.Join("testdata", "representative"), dir)

	// lock
	{
		var out, errOut bytes.Buffer
		if code := run([]string{"--chdir", dir, "lock"}, &out, &errOut); code != 0 {
			t.Fatalf("lock: code=%d out=%q err=%q", code, out.String(), errOut.String())
		}
	}
	// plan — must show pending changes
	{
		var out, errOut bytes.Buffer
		if code := run([]string{"--chdir", dir, "plan"}, &out, &errOut); code != 0 {
			t.Fatalf("plan: code=%d out=%q err=%q", code, out.String(), errOut.String())
		}
		if strings.Contains(out.String()+errOut.String(), "No changes") {
			t.Errorf("plan: expected pending changes, got 'No changes'")
		}
	}
	// apply --yes --no-install
	{
		var out, errOut bytes.Buffer
		if code := run([]string{"--chdir", dir, "apply", "--yes", "--no-install"}, &out, &errOut); code != 0 {
			t.Fatalf("apply: code=%d out=%q err=%q", code, out.String(), errOut.String())
		}
	}

	// Command file written.
	if _, err := os.Stat(filepath.Join(dir, ".claude", "commands", "greet.md")); err != nil {
		t.Errorf("command file not written: %v", err)
	}
	// Hook written into settings.json.
	settings, err := os.ReadFile(filepath.Join(dir, ".claude", "settings.json"))
	if err != nil {
		t.Fatalf("settings.json not written: %v", err)
	}
	if !strings.Contains(string(settings), "PreToolUse") {
		t.Errorf("settings.json missing the hook event: %s", settings)
	}
	// .mcp.json contains both the inline and the templated server.
	mcpRaw, err := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	if err != nil {
		t.Fatalf(".mcp.json not written: %v", err)
	}
	var mcpDoc struct {
		MCPServers map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal(mcpRaw, &mcpDoc); err != nil {
		t.Fatalf(".mcp.json invalid: %v", err)
	}
	for _, id := range []string{"linear", "local-fs"} {
		if _, ok := mcpDoc.MCPServers[id]; !ok {
			t.Errorf(".mcp.json missing server %q: %s", id, mcpRaw)
		}
	}
	// Non-default-target rule: the target file exists and imports the fragment.
	target, err := os.ReadFile(filepath.Join(dir, "docs", "agent-context.md"))
	if err != nil {
		t.Errorf("rule target docs/agent-context.md not written: %v", err)
	} else if !strings.Contains(string(target), "team-context") {
		t.Errorf("rule target missing the fragment import: %s", target)
	}

	// check — no drift.
	{
		var out, errOut bytes.Buffer
		if code := run([]string{"--chdir", dir, "check"}, &out, &errOut); code != 0 {
			t.Fatalf("check: code=%d out=%q err=%q", code, out.String(), errOut.String())
		}
		if !strings.Contains(out.String()+errOut.String(), "No drift") {
			t.Errorf("check: expected 'No drift'")
		}
	}
	// second plan — no changes.
	{
		var out, errOut bytes.Buffer
		if code := run([]string{"--chdir", dir, "plan"}, &out, &errOut); code != 0 {
			t.Fatalf("plan 2: code=%d out=%q err=%q", code, out.String(), errOut.String())
		}
		if !strings.Contains(out.String()+errOut.String(), "No changes") {
			t.Errorf("second plan: expected 'No changes'")
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./cmd/ainfra/ -run TestE2ERepresentative -v`
Expected: FAIL — `testdata/representative/` does not exist, so `copyTestdata` calls `t.Fatalf` on the missing fixture directory.

- [ ] **Step 3: Create the fixture manifest**

Create `cmd/ainfra/testdata/representative/ainfra.yaml`:

```yaml
version: 1
agent: claude-code

cliTools:
  jq:
    install:
      brew:
        formula: jq

mcpServers:
  linear:
    transport: http
    url: https://mcp.linear.app/sse
  local-fs:
    template: stdio-fs
    params:
      rootDir: /tmp

templates:
  stdio-fs:
    params:
      rootDir:
        type: string
        required: true
    produces:
      mcpServer:
        transport: stdio
        command: npx
        args: ["-y", "@modelcontextprotocol/server-filesystem"]
        version: "2025.1.0"
        env:
          FS_ROOT: "${params.rootDir}"

hooks:
  audit-tools:
    event: PreToolUse
    source: hooks/audit.sh
    timeout: 5000
    requires:
      - cliTool: jq

commands:
  greet:
    source: commands/greet.md
    description: Greet the user.

rules:
  team-context:
    target: docs/agent-context.md
    source: rules/context.md

tools:
  builtins:
    disabled:
      - WebSearch
  permissions:
    allow:
      - "Read(*)"
```

Create `cmd/ainfra/testdata/representative/commands/greet.md`:

```markdown
# greet

Greet the user by name.
```

Create `cmd/ainfra/testdata/representative/hooks/audit.sh`:

```bash
#!/usr/bin/env bash
echo "audit: $*"
```

Create `cmd/ainfra/testdata/representative/rules/context.md`:

```markdown
# Team context

Always run the test suite before committing.
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./cmd/ainfra/ -run TestE2ERepresentative -v`
Expected: PASS.

If it fails on a specific channel, the failure is real — investigate whether the fixture is malformed or a renderer/provider genuinely mishandles that channel. Do not weaken an assertion to make it pass; if a real bug surfaces, report it as DONE_WITH_CONCERNS with the detail.

- [ ] **Step 5: Run the whole cmd/ainfra suite**

Run: `go test ./cmd/ainfra/ -v`
Expected: all PASS, including the three pre-existing e2e tests.

- [ ] **Step 6: Commit**

```bash
git add cmd/ainfra/testdata/representative cmd/ainfra/e2e_test.go
git commit -m "Add representative multi-channel apply e2e test"
```

---

### Task 2: Render↔channel contract test

Add one table-driven test that runs the real `resolve.RenderResources` and feeds every rendered resource straight into its channel provider's `Apply`. It asserts each rendered payload is *consumed* — `Apply` returns no error, records no `Failed` entry, and reports the change in `Applied`. This is the test class that would have caught the `map[string]map[string]any` payload-type bug immediately: a renderer/provider key or type mismatch makes a channel's change silently drop out of `Applied`, failing the test.

**Files:**
- Test: `cmd/ainfra/contract_test.go` (new)

- [ ] **Step 1: Write the failing test**

Create `cmd/ainfra/contract_test.go`:

```go
package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/MHilhorst/ainfra/internal/provider"
	"github.com/MHilhorst/ainfra/internal/resolve"
)

// TestRenderChannelContract renders a broad manifest and feeds every rendered
// resource into its channel provider's Apply. A renderer/provider payload
// mismatch makes the change silently drop from ApplyResult.Applied — this test
// fails when that happens. cliTools runs under NoInstall so no real package
// manager is invoked; marketplaces and plugins are covered by their own unit
// tests (their Apply shells out to the `claude` CLI).
func TestRenderChannelContract(t *testing.T) {
	dir := t.TempDir()
	copyTestdata(t, filepath.Join("testdata", "representative"), dir)

	rendered, err := resolve.RenderResources(dir)
	if err != nil {
		t.Fatalf("RenderResources: %v", err)
	}

	providers, err := providersForDir(dir)
	if err != nil {
		t.Fatalf("providersForDir: %v", err)
	}
	byChannel := map[string]provider.Provider{}
	for _, p := range providers {
		byChannel[p.Channel()] = p
	}

	// Channels whose Apply writes files or runs no command under NoInstall.
	// marketplaces and plugins are excluded — their Apply requires the `claude`
	// CLI; they are covered by internal/provider/claudecode unit tests.
	contractChannels := []string{
		"cliTools", "mcpServers", "hooks", "commands", "rules", "tools",
	}

	for _, ch := range contractChannels {
		resources := rendered[ch]
		if len(resources) == 0 {
			t.Errorf("channel %q rendered no resources; fixture should exercise it", ch)
			continue
		}
		p, ok := byChannel[ch]
		if !ok {
			t.Errorf("no provider registered for channel %q", ch)
			continue
		}
		for _, r := range resources {
			t.Run(ch+"/"+r.ID, func(t *testing.T) {
				env := provider.Env{
					FS:        provider.NewMemFilesystem(),
					Runner:    provider.NewFakeRunner(),
					Root:      dir,
					Home:      filepath.Join(dir, "home"),
					NoInstall: true,
				}
				plan := provider.ChannelPlan{
					Channel: ch,
					Changes: []provider.Change{{
						Kind:     provider.ChangeCreate,
						ID:       r.ID,
						Resource: r,
					}},
				}
				res, err := p.Apply(env, plan)
				if err != nil {
					t.Fatalf("%s/%s: Apply returned error: %v", ch, r.ID, err)
				}
				if len(res.Failed) != 0 {
					t.Errorf("%s/%s: Apply reported failures: %+v", ch, r.ID, res.Failed)
				}
				if len(res.Applied) != 1 {
					t.Errorf("%s/%s: rendered payload not consumed — Applied=%d, want 1 (renderer/provider contract mismatch?)",
						ch, r.ID, len(res.Applied))
				}
			})
		}
	}
}

var _ = os.Stat // keep "os" import stable if assertions are trimmed
```

- [ ] **Step 2: Run the test to verify the starting state**

Run: `go test ./cmd/ainfra/ -run TestRenderChannelContract -v`
Expected: it compiles and runs. If every channel's contract holds it PASSES; if a channel's payload is mishandled it FAILS with a clear "rendered payload not consumed" message. Record the outcome.

Note: this test depends on `copyTestdata` and the `testdata/representative` fixture from Task 1 — Task 2 must be executed after Task 1.

- [ ] **Step 3: Resolve the `os` import**

The `var _ = os.Stat` line in Step 1 is a placeholder so the file compiles before you confirm whether `os` is needed. After Step 2 runs, the test does not use `os` — remove both the placeholder line `var _ = os.Stat // ...` and `"os"` from the import block.

Run: `go test ./cmd/ainfra/ -run TestRenderChannelContract -v` again.
Expected: PASS (or a genuine, reported contract failure).

- [ ] **Step 4: Run the whole suite**

Run: `go test ./... && go vet ./...`
Expected: all packages PASS, vet clean.

- [ ] **Step 5: Commit**

```bash
git add cmd/ainfra/contract_test.go
git commit -m "Add render-to-channel contract test"
```

---

## Self-review notes

- **Spec coverage.** Item #2 asks for a representative `testdata/` fixture + an
  e2e test (Task 1) and a render↔channel contract test (Task 2). Both are
  delivered. `marketplaces` and `plugins` are deliberately out of scope for
  both tests — their `Apply` shells out to the `claude` CLI, which cannot run
  in a hermetic test; they retain their existing unit tests. `skills` and
  `backgroundServices` are not in the fixture (skills needs a fetchable bundle,
  backgroundServices only arises from a template that produces one); extending
  the fixture to them is a reasonable follow-up but not required to close the
  motivating gap.
- **No production code change.** Both tasks add tests and testdata only. If a
  task surfaces a real renderer/provider bug, that is the test doing its job —
  report it (DONE_WITH_CONCERNS) rather than weakening the assertion.
- **Task ordering.** Task 2 reuses `copyTestdata` and the fixture from Task 1;
  execute Task 1 first.
- **Type consistency.** The contract test asserts `len(res.Applied) == 1` per
  rendered change — the providers append a non-noop applied `Change` to
  `ApplyResult.Applied`, the field name used across `provider.go`,
  `orchestrator.go`, and the channel providers.
