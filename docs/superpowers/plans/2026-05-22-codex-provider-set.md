# Codex Provider Set Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the Codex channel provider set so a manifest with `agent: codex` reconciles its MCP servers into `~/.codex/config.toml` and its rules into `AGENTS.md`.

**Architecture:** A new `internal/provider/codex/` package with two providers (`MCP`, `Rules`), backed by two new `internal/provider/fsmerge` helpers — `MergeTOMLTables` (own-the-tables TOML round-trip) and `MergeManagedRegion` (a delimited, per-rule region in `AGENTS.md`). `agentset.ForAgent` gains a `codex` case. The `Provider` interface and the orchestrator are untouched.

**Tech Stack:** Go 1.25, `github.com/BurntSushi/toml` (new dependency), standard library. Tests are standard `go test`.

**Spec:** `docs/superpowers/specs/2026-05-22-codex-provider-set-design.md`.

---

## File Structure

**Created:**
- `internal/provider/fsmerge/toml.go` — `MergeTOMLTables`, the TOML analogue of `MergeJSONKeys`.
- `internal/provider/fsmerge/toml_test.go` — its tests (package `fsmerge`, reuses the `newMemFS` helper from `fsmerge_test.go`).
- `internal/provider/fsmerge/region.go` — `MergeManagedRegion` and `ManagedRegionIDs`, the `AGENTS.md` managed-region helpers.
- `internal/provider/fsmerge/region_test.go` — their tests (package `fsmerge`).
- `internal/provider/codex/mcp.go` — the `codex.MCP` provider (package `codex`).
- `internal/provider/codex/mcp_test.go` — its tests.
- `internal/provider/codex/rules.go` — the `codex.Rules` provider.
- `internal/provider/codex/rules_test.go` — its tests.

**Modified:**
- `go.mod` / `go.sum` — add `github.com/BurntSushi/toml`.
- `internal/provider/agentset/agentset.go` — add the `case agent.Codex`.
- `internal/provider/agentset/agentset_test.go` — replace the "codex not yet available" test with a real codex-set test.

---

## Task 1: The `MergeTOMLTables` fsmerge helper

**Files:**
- Modify: `go.mod`, `go.sum` (add the dependency)
- Create: `internal/provider/fsmerge/toml.go`
- Test: `internal/provider/fsmerge/toml_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/provider/fsmerge/toml_test.go`:

```go
package fsmerge

import (
	"strings"
	"testing"
)

func TestMergeTOMLTablesCreatesFile(t *testing.T) {
	fs := newMemFS()
	err := MergeTOMLTables(fs, "/config.toml", "mcp_servers",
		map[string]any{"github": map[string]any{"command": "npx"}},
		[]string{"github"})
	if err != nil {
		t.Fatal(err)
	}
	out := string(fs.files["/config.toml"])
	if !strings.Contains(out, "[mcp_servers.github]") {
		t.Errorf("expected [mcp_servers.github] table, got:\n%s", out)
	}
	if !strings.Contains(out, `command = "npx"`) {
		t.Errorf("expected command key, got:\n%s", out)
	}
}

func TestMergeTOMLTablesPreservesForeignContent(t *testing.T) {
	fs := newMemFS()
	fs.files["/config.toml"] = []byte(
		"model = \"gpt-5\"\n\n[mcp_servers.other]\ncommand = \"other-cmd\"\n\n[mcp_servers.old]\ncommand = \"old-cmd\"\n")

	err := MergeTOMLTables(fs, "/config.toml", "mcp_servers",
		map[string]any{"github": map[string]any{"command": "npx"}},
		[]string{"old", "github"}) // owned: old (removed), github (set)
	if err != nil {
		t.Fatal(err)
	}
	out := string(fs.files["/config.toml"])
	if !strings.Contains(out, `model = "gpt-5"`) {
		t.Errorf("foreign top-level key 'model' was dropped:\n%s", out)
	}
	if !strings.Contains(out, "[mcp_servers.other]") {
		t.Errorf("foreign server 'other' was dropped:\n%s", out)
	}
	if !strings.Contains(out, "[mcp_servers.github]") {
		t.Errorf("desired server 'github' missing:\n%s", out)
	}
	if strings.Contains(out, "[mcp_servers.old]") {
		t.Errorf("owned-but-undesired server 'old' should have been removed:\n%s", out)
	}
}

func TestMergeTOMLTablesRejectsMalformed(t *testing.T) {
	fs := newMemFS()
	fs.files["/config.toml"] = []byte("this is = = not valid toml [[[")
	err := MergeTOMLTables(fs, "/config.toml", "mcp_servers",
		map[string]any{"x": map[string]any{}}, []string{"x"})
	if err == nil {
		t.Error("expected an error for malformed TOML, got nil")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/provider/fsmerge/ -run TestMergeTOMLTables`
Expected: FAIL — build error, `MergeTOMLTables` and the toml import do not exist yet.

- [ ] **Step 3: Add the `BurntSushi/toml` dependency**

Run from the repo root:
```bash
go get github.com/BurntSushi/toml
go mod tidy
```
Expected: `go.mod` gains a `require github.com/BurntSushi/toml vX.Y.Z` line and `go.sum` is updated. (This needs network access. If the sandbox blocks it, report BLOCKED — the controller will run it.)

- [ ] **Step 4: Write the implementation**

Create `internal/provider/fsmerge/toml.go`:

```go
package fsmerge

import (
	"errors"
	iofs "io/fs"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// MergeTOMLTables performs a three-way merge of a single TOML table key into
// the file at path. It reads the file (treating a missing file as empty),
// ensures the topKey table exists, removes every key listed in ownedKeys,
// sets every entry from desired, then writes the document back as TOML.
//
// Foreign keys — those present in the file but absent from ownedKeys — are
// preserved as data. Comments and exact formatting are not preserved: the
// document is re-serialised. A file that is not valid TOML is a hard error.
func MergeTOMLTables(fs FS, path, topKey string, desired map[string]any, ownedKeys []string) error {
	raw, err := fs.ReadFile(path)
	if errors.Is(err, iofs.ErrNotExist) {
		raw = []byte{}
	} else if err != nil {
		return err
	}

	doc := map[string]any{}
	if len(raw) > 0 {
		if err := toml.Unmarshal(raw, &doc); err != nil {
			return err
		}
	}

	top, ok := doc[topKey].(map[string]any)
	if !ok {
		top = map[string]any{}
	}

	for _, k := range ownedKeys {
		delete(top, k)
	}
	for k, v := range desired {
		top[k] = v
	}

	if len(top) == 0 {
		delete(doc, topKey)
	} else {
		doc[topKey] = top
	}

	out, err := toml.Marshal(doc)
	if err != nil {
		return err
	}

	if err := fs.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return fs.WriteFile(path, out, 0o644)
}
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./internal/provider/fsmerge/ -run TestMergeTOMLTables`
Expected: PASS — all three tests.

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum internal/provider/fsmerge/toml.go internal/provider/fsmerge/toml_test.go
git commit -m "Add the MergeTOMLTables fsmerge helper"
```

---

## Task 2: The managed-region fsmerge helpers

**Files:**
- Create: `internal/provider/fsmerge/region.go`
- Test: `internal/provider/fsmerge/region_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/provider/fsmerge/region_test.go`:

```go
package fsmerge

import (
	"reflect"
	"strings"
	"testing"
)

func TestMergeManagedRegionCreatesRegionPreservingUserContent(t *testing.T) {
	fs := newMemFS()
	fs.files["/AGENTS.md"] = []byte("# My project\n\nHand-written guidance.\n")

	err := MergeManagedRegion(fs, "/AGENTS.md",
		map[string]string{"incident-response": "Page the on-call."},
		[]string{"incident-response"})
	if err != nil {
		t.Fatal(err)
	}
	out := string(fs.files["/AGENTS.md"])
	if !strings.Contains(out, "# My project") || !strings.Contains(out, "Hand-written guidance.") {
		t.Errorf("user content not preserved:\n%s", out)
	}
	if !strings.Contains(out, "<!-- ainfra:begin -->") || !strings.Contains(out, "<!-- ainfra:end -->") {
		t.Errorf("region markers missing:\n%s", out)
	}
	if !strings.Contains(out, "<!-- ainfra:rule incident-response -->") {
		t.Errorf("rule marker missing:\n%s", out)
	}
	if !strings.Contains(out, "Page the on-call.") {
		t.Errorf("rule content missing:\n%s", out)
	}
}

func TestManagedRegionIDsRoundTrip(t *testing.T) {
	fs := newMemFS()
	if err := MergeManagedRegion(fs, "/AGENTS.md",
		map[string]string{"a": "Content A", "b": "Content B"}, []string{"a", "b"}); err != nil {
		t.Fatal(err)
	}
	ids, err := ManagedRegionIDs(fs, "/AGENTS.md")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ids, []string{"a", "b"}) {
		t.Errorf("ids = %v, want [a b]", ids)
	}
}

func TestManagedRegionIDsMissingFile(t *testing.T) {
	fs := newMemFS()
	ids, err := ManagedRegionIDs(fs, "/nope.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 0 {
		t.Errorf("ids = %v, want empty", ids)
	}
}

func TestMergeManagedRegionUpdatesAndRemoves(t *testing.T) {
	fs := newMemFS()
	if err := MergeManagedRegion(fs, "/AGENTS.md",
		map[string]string{"a": "old A", "b": "Content B"}, []string{"a", "b"}); err != nil {
		t.Fatal(err)
	}
	// Update a, remove b.
	if err := MergeManagedRegion(fs, "/AGENTS.md",
		map[string]string{"a": "new A"}, []string{"a", "b"}); err != nil {
		t.Fatal(err)
	}
	out := string(fs.files["/AGENTS.md"])
	if !strings.Contains(out, "new A") || strings.Contains(out, "old A") {
		t.Errorf("rule 'a' not updated:\n%s", out)
	}
	if strings.Contains(out, "Content B") || strings.Contains(out, "ainfra:rule b") {
		t.Errorf("rule 'b' not removed:\n%s", out)
	}
}

func TestMergeManagedRegionRemovesEmptyRegion(t *testing.T) {
	fs := newMemFS()
	fs.files["/AGENTS.md"] = []byte("# Title\n")
	if err := MergeManagedRegion(fs, "/AGENTS.md",
		map[string]string{"a": "A"}, []string{"a"}); err != nil {
		t.Fatal(err)
	}
	// Remove the only rule — the region must disappear entirely.
	if err := MergeManagedRegion(fs, "/AGENTS.md",
		map[string]string{}, []string{"a"}); err != nil {
		t.Fatal(err)
	}
	out := string(fs.files["/AGENTS.md"])
	if strings.Contains(out, "ainfra:begin") || strings.Contains(out, "ainfra:end") {
		t.Errorf("region should be gone:\n%s", out)
	}
	if !strings.Contains(out, "# Title") {
		t.Errorf("user content lost:\n%s", out)
	}
}

func TestMergeManagedRegionRejectsUnterminatedRegion(t *testing.T) {
	fs := newMemFS()
	fs.files["/AGENTS.md"] = []byte("<!-- ainfra:begin -->\nno end marker here\n")
	err := MergeManagedRegion(fs, "/AGENTS.md", map[string]string{"a": "A"}, []string{"a"})
	if err == nil {
		t.Error("expected an error for a region with no end marker, got nil")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/provider/fsmerge/ -run "Region"`
Expected: FAIL — build error, `MergeManagedRegion` and `ManagedRegionIDs` do not exist yet.

- [ ] **Step 3: Write the implementation**

Create `internal/provider/fsmerge/region.go`:

```go
package fsmerge

import (
	"errors"
	iofs "io/fs"
	"path/filepath"
	"sort"
	"strings"
)

const (
	regionBegin = "<!-- ainfra:begin -->"
	regionEnd   = "<!-- ainfra:end -->"
	ruleOpen    = "<!-- ainfra:rule "
	ruleClose   = " -->"
)

// splitRegion divides file content into the text before the ainfra-managed
// region, the per-rule blocks inside it, and the text after. found reports
// whether a region was present. A begin marker with no end marker is an error.
func splitRegion(content string) (before string, rules map[string]string, after string, found bool, err error) {
	bi := strings.Index(content, regionBegin)
	if bi < 0 {
		return content, map[string]string{}, "", false, nil
	}
	ei := strings.Index(content, regionEnd)
	if ei < 0 || ei < bi {
		return "", nil, "", false, errors.New("fsmerge: managed region has a begin marker but no end marker")
	}
	before = content[:bi]
	after = content[ei+len(regionEnd):]
	inner := content[bi+len(regionBegin) : ei]
	return before, parseRuleBlocks(inner), after, true, nil
}

// parseRuleBlocks parses the inside of a managed region into id->content.
// A rule's content runs from its marker line to the next marker (or the end).
func parseRuleBlocks(inner string) map[string]string {
	rules := map[string]string{}
	id := ""
	var body []string
	flush := func() {
		if id != "" {
			rules[id] = strings.Trim(strings.Join(body, "\n"), "\n")
		}
	}
	for _, line := range strings.Split(inner, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, ruleOpen) && strings.HasSuffix(t, ruleClose) {
			flush()
			id = strings.TrimSpace(t[len(ruleOpen) : len(t)-len(ruleClose)])
			body = nil
			continue
		}
		if id != "" {
			body = append(body, line)
		}
	}
	flush()
	return rules
}

// renderRegion renders the managed region for id->content, ids sorted.
func renderRegion(rules map[string]string) string {
	ids := make([]string, 0, len(rules))
	for id := range rules {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	var b strings.Builder
	b.WriteString(regionBegin + "\n")
	for _, id := range ids {
		b.WriteString(ruleOpen + id + ruleClose + "\n")
		b.WriteString(rules[id] + "\n")
	}
	b.WriteString(regionEnd)
	return b.String()
}

// ManagedRegionIDs returns the sorted ids of the rules in the ainfra-managed
// region of the file at path. A missing file or absent region returns no ids.
func ManagedRegionIDs(fs FS, path string) ([]string, error) {
	raw, err := fs.ReadFile(path)
	if errors.Is(err, iofs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	_, rules, _, _, err := splitRegion(string(raw))
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(rules))
	for id := range rules {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, nil
}

// MergeManagedRegion updates the ainfra-managed region in the file at path: it
// removes every id in ownedIDs, then sets every id->content pair in blocks.
// Content outside the region is preserved. When the region would become empty
// it is removed entirely, markers and all. A missing file is created.
func MergeManagedRegion(fs FS, path string, blocks map[string]string, ownedIDs []string) error {
	raw, err := fs.ReadFile(path)
	if errors.Is(err, iofs.ErrNotExist) {
		raw = []byte{}
	} else if err != nil {
		return err
	}

	before, rules, after, found, err := splitRegion(string(raw))
	if err != nil {
		return err
	}
	for _, id := range ownedIDs {
		delete(rules, id)
	}
	for id, content := range blocks {
		rules[id] = content
	}

	var out string
	if len(rules) == 0 {
		// No region content: the file is just the user's text.
		head := strings.TrimRight(before, "\n")
		tail := strings.TrimLeft(after, "\n")
		out = head
		if tail != "" {
			if out != "" {
				out += "\n"
			}
			out += tail
		}
	} else {
		region := renderRegion(rules)
		head := strings.TrimRight(before, "\n")
		tail := strings.TrimLeft(after, "\n")
		if !found {
			// No region yet: append after all existing content.
			head = strings.TrimRight(string(raw), "\n")
			tail = ""
		}
		out = region
		if head != "" {
			out = head + "\n\n" + region
		}
		if tail != "" {
			out += "\n\n" + tail
		}
	}
	if out != "" && !strings.HasSuffix(out, "\n") {
		out += "\n"
	}

	if err := fs.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return fs.WriteFile(path, []byte(out), 0o644)
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/provider/fsmerge/ -run "Region"`
Expected: PASS — all six tests.

- [ ] **Step 5: Commit**

```bash
git add internal/provider/fsmerge/region.go internal/provider/fsmerge/region_test.go
git commit -m "Add the managed-region fsmerge helpers for AGENTS.md"
```

---

## Task 3: The `codex.MCP` provider

**Files:**
- Create: `internal/provider/codex/mcp.go`
- Test: `internal/provider/codex/mcp_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/provider/codex/mcp_test.go`:

```go
package codex_test

import (
	"strings"
	"testing"

	"github.com/MHilhorst/ainfra/internal/provider"
	"github.com/MHilhorst/ainfra/internal/provider/codex"
)

func TestMCPChannel(t *testing.T) {
	if got := (codex.MCP{}).Channel(); got != "mcpServers" {
		t.Fatalf("Channel() = %q, want mcpServers", got)
	}
}

func TestMCPObserveEmpty(t *testing.T) {
	mem := provider.NewMemFilesystem()
	env := provider.Env{FS: mem, Home: "/home"}
	got, err := (codex.MCP{}).Observe(env)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("Observe: got %d resources, want 0", len(got))
	}
}

func TestMCPObserveWithServers(t *testing.T) {
	mem := provider.NewMemFilesystem()
	mem.WriteFile("/home/.codex/config.toml",
		[]byte("[mcp_servers.a]\ncommand = \"cmd-a\"\n\n[mcp_servers.foreign]\ncommand = \"cmd-f\"\n"), 0o644)
	env := provider.Env{FS: mem, Home: "/home"}
	got, err := (codex.MCP{}).Observe(env)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	ids := map[string]bool{}
	for _, r := range got {
		ids[r.ID] = true
		if r.Channel != "mcpServers" {
			t.Errorf("resource %q: Channel = %q", r.ID, r.Channel)
		}
	}
	if !ids["a"] || !ids["foreign"] {
		t.Errorf("ids = %v, want a and foreign", ids)
	}
}

func TestMCPApplyCreate(t *testing.T) {
	mem := provider.NewMemFilesystem()
	env := provider.Env{FS: mem, Home: "/home"}
	plan := provider.ChannelPlan{
		Channel: "mcpServers",
		Changes: []provider.Change{{
			Kind: provider.ChangeCreate,
			ID:   "github",
			Resource: provider.Resource{
				ID:      "github",
				Channel: "mcpServers",
				Payload: map[string]any{
					"command":   "npx",
					"args":      []any{"-y", "server-github"},
					"env":       map[string]any{"TOKEN": "x"},
					"transport": "stdio",
				},
			},
		}},
	}
	result, err := (codex.MCP{}).Apply(env, plan)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(result.Applied) != 1 {
		t.Fatalf("Applied = %d, want 1", len(result.Applied))
	}
	out := string(mem.Files["/home/.codex/config.toml"])
	if !strings.Contains(out, "[mcp_servers.github]") {
		t.Errorf("missing table:\n%s", out)
	}
	if !strings.Contains(out, `command = "npx"`) {
		t.Errorf("missing command:\n%s", out)
	}
	if strings.Contains(out, "transport") || strings.Contains(out, "stdio") {
		t.Errorf("transport must not be written for codex:\n%s", out)
	}
}

func TestMCPApplyDryRunWritesNothing(t *testing.T) {
	mem := provider.NewMemFilesystem()
	env := provider.Env{FS: mem, Home: "/home", DryRun: true}
	plan := provider.ChannelPlan{
		Channel: "mcpServers",
		Changes: []provider.Change{{
			Kind:     provider.ChangeCreate,
			ID:       "github",
			Resource: provider.Resource{ID: "github", Channel: "mcpServers", Payload: map[string]any{"command": "npx"}},
		}},
	}
	if _, err := (codex.MCP{}).Apply(env, plan); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if _, ok := mem.Files["/home/.codex/config.toml"]; ok {
		t.Error("DryRun must not write the file")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/provider/codex/ -run TestMCP`
Expected: FAIL — build error, the `codex` package does not exist yet.

- [ ] **Step 3: Write the implementation**

Create `internal/provider/codex/mcp.go`:

```go
// Package codex contains the Codex channel providers for the ainfra
// reconciler. Each type implements provider.Provider for one channel kind,
// rendering into the config files the Codex CLI reads.
package codex

import (
	"errors"
	iofs "io/fs"
	"path/filepath"

	"github.com/MHilhorst/ainfra/internal/provider"
	"github.com/MHilhorst/ainfra/internal/provider/fsmerge"
	"github.com/BurntSushi/toml"
)

// MCP reconciles MCP servers into ~/.codex/config.toml under the
// [mcp_servers.<id>] tables.
type MCP struct{}

// Channel returns the channel name this provider manages.
func (MCP) Channel() string { return "mcpServers" }

func configPath(env provider.Env) string {
	return filepath.Join(env.Home, ".codex", "config.toml")
}

// Observe reads config.toml and returns a Resource for each key under
// [mcp_servers]. A missing file is treated as no resources. ContentHash is
// left empty; the orchestrator backfills it from the ledger.
func (MCP) Observe(env provider.Env) ([]provider.Resource, error) {
	raw, err := env.FS.ReadFile(configPath(env))
	if errors.Is(err, iofs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	doc := map[string]any{}
	if len(raw) > 0 {
		if err := toml.Unmarshal(raw, &doc); err != nil {
			return nil, err
		}
	}

	servers, ok := doc["mcp_servers"].(map[string]any)
	if !ok {
		return nil, nil
	}

	resources := make([]provider.Resource, 0, len(servers))
	for key := range servers {
		resources = append(resources, provider.Resource{ID: key, Channel: "mcpServers"})
	}
	return resources, nil
}

// Apply executes the channel plan against config.toml. When env.DryRun is
// true, the result is computed but the file is not written.
func (MCP) Apply(env provider.Env, plan provider.ChannelPlan) (provider.ApplyResult, error) {
	desired := map[string]any{}
	ownedKeys := make([]string, 0, len(plan.Changes))
	var applied []provider.Change

	for _, c := range plan.Changes {
		if c.Kind == provider.ChangeNoop {
			continue
		}
		ownedKeys = append(ownedKeys, c.ID)
		applied = append(applied, c)
		if c.Kind == provider.ChangeCreate || c.Kind == provider.ChangeUpdate {
			desired[c.ID] = buildCodexServerTable(c.Resource.Payload)
		}
		// ChangeDelete: in ownedKeys, not in desired — the merge removes it.
	}

	if len(ownedKeys) == 0 {
		return provider.ApplyResult{Channel: "mcpServers"}, nil
	}

	if !env.DryRun {
		if err := fsmerge.MergeTOMLTables(env.FS, configPath(env), "mcp_servers", desired, ownedKeys); err != nil {
			return provider.ApplyResult{}, err
		}
	}

	return provider.ApplyResult{Channel: "mcpServers", Applied: applied}, nil
}

// buildCodexServerTable constructs the [mcp_servers.<id>] table from a resource
// payload. Codex MCP servers are command-launched; the payload's transport
// field is not written. Nil or missing optional fields are omitted.
func buildCodexServerTable(payload map[string]any) map[string]any {
	table := map[string]any{}
	if cmd, ok := payload["command"]; ok && cmd != nil && cmd != "" {
		table["command"] = cmd
	}
	if args, ok := payload["args"]; ok && args != nil {
		table["args"] = args
	}
	if env, ok := payload["env"]; ok && env != nil {
		table["env"] = env
	}
	return table
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/provider/codex/ -run TestMCP`
Expected: PASS — all five tests.

- [ ] **Step 5: Commit**

```bash
git add internal/provider/codex/mcp.go internal/provider/codex/mcp_test.go
git commit -m "Add the Codex MCP provider"
```

---

## Task 4: The `codex.Rules` provider

**Files:**
- Create: `internal/provider/codex/rules.go`
- Test: `internal/provider/codex/rules_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/provider/codex/rules_test.go`:

```go
package codex_test

import (
	"strings"
	"testing"

	"github.com/MHilhorst/ainfra/internal/provider"
	"github.com/MHilhorst/ainfra/internal/provider/codex"
)

func TestRulesChannel(t *testing.T) {
	if got := (codex.Rules{}).Channel(); got != "rules" {
		t.Fatalf("Channel() = %q, want rules", got)
	}
}

func TestRulesObserveEmpty(t *testing.T) {
	mem := provider.NewMemFilesystem()
	env := provider.Env{FS: mem, Root: "/repo"}
	got, err := (codex.Rules{}).Observe(env)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("Observe: got %d resources, want 0", len(got))
	}
}

func TestRulesApplyCreatePreservesUserContent(t *testing.T) {
	mem := provider.NewMemFilesystem()
	mem.WriteFile("/repo/AGENTS.md", []byte("# Hand-written\n\nMy own notes.\n"), 0o644)
	env := provider.Env{FS: mem, Root: "/repo"}
	plan := provider.ChannelPlan{
		Channel: "rules",
		Changes: []provider.Change{{
			Kind: provider.ChangeCreate,
			ID:   "incident-response",
			Resource: provider.Resource{
				ID:      "incident-response",
				Channel: "rules",
				Payload: map[string]any{"content": "Page the on-call engineer.", "target": "CLAUDE.md"},
			},
		}},
	}
	result, err := (codex.Rules{}).Apply(env, plan)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(result.Applied) != 1 {
		t.Fatalf("Applied = %d, want 1", len(result.Applied))
	}
	out := string(mem.Files["/repo/AGENTS.md"])
	if !strings.Contains(out, "My own notes.") {
		t.Errorf("user content lost:\n%s", out)
	}
	if !strings.Contains(out, "<!-- ainfra:rule incident-response -->") {
		t.Errorf("rule marker missing:\n%s", out)
	}
	if !strings.Contains(out, "Page the on-call engineer.") {
		t.Errorf("rule content missing:\n%s", out)
	}
}

func TestRulesObserveAfterApply(t *testing.T) {
	mem := provider.NewMemFilesystem()
	env := provider.Env{FS: mem, Root: "/repo"}
	plan := provider.ChannelPlan{
		Channel: "rules",
		Changes: []provider.Change{{
			Kind:     provider.ChangeCreate,
			ID:       "r1",
			Resource: provider.Resource{ID: "r1", Channel: "rules", Payload: map[string]any{"content": "Rule one."}},
		}},
	}
	if _, err := (codex.Rules{}).Apply(env, plan); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	got, err := (codex.Rules{}).Observe(env)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if len(got) != 1 || got[0].ID != "r1" || got[0].Channel != "rules" {
		t.Fatalf("Observe = %+v, want one rules resource id r1", got)
	}
}

func TestRulesApplyDelete(t *testing.T) {
	mem := provider.NewMemFilesystem()
	env := provider.Env{FS: mem, Root: "/repo"}
	create := provider.ChannelPlan{Channel: "rules", Changes: []provider.Change{{
		Kind:     provider.ChangeCreate,
		ID:       "r1",
		Resource: provider.Resource{ID: "r1", Channel: "rules", Payload: map[string]any{"content": "Rule one."}},
	}}}
	if _, err := (codex.Rules{}).Apply(env, create); err != nil {
		t.Fatalf("Apply create: %v", err)
	}
	del := provider.ChannelPlan{Channel: "rules", Changes: []provider.Change{{
		Kind:     provider.ChangeDelete,
		ID:       "r1",
		Resource: provider.Resource{ID: "r1", Channel: "rules"},
	}}}
	if _, err := (codex.Rules{}).Apply(env, del); err != nil {
		t.Fatalf("Apply delete: %v", err)
	}
	out := string(mem.Files["/repo/AGENTS.md"])
	if strings.Contains(out, "ainfra:rule r1") || strings.Contains(out, "Rule one.") {
		t.Errorf("rule not removed:\n%s", out)
	}
}

func TestRulesApplyDryRunWritesNothing(t *testing.T) {
	mem := provider.NewMemFilesystem()
	env := provider.Env{FS: mem, Root: "/repo", DryRun: true}
	plan := provider.ChannelPlan{Channel: "rules", Changes: []provider.Change{{
		Kind:     provider.ChangeCreate,
		ID:       "r1",
		Resource: provider.Resource{ID: "r1", Channel: "rules", Payload: map[string]any{"content": "Rule one."}},
	}}}
	if _, err := (codex.Rules{}).Apply(env, plan); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if _, ok := mem.Files["/repo/AGENTS.md"]; ok {
		t.Error("DryRun must not write the file")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/provider/codex/ -run TestRules`
Expected: FAIL — build error, `codex.Rules` does not exist yet.

- [ ] **Step 3: Write the implementation**

Create `internal/provider/codex/rules.go`:

```go
package codex

import (
	"path/filepath"

	"github.com/MHilhorst/ainfra/internal/provider"
	"github.com/MHilhorst/ainfra/internal/provider/fsmerge"
)

// Rules reconciles rule content into an ainfra-managed region of the
// repository's AGENTS.md file — the instruction file the Codex CLI reads.
type Rules struct{}

// Channel returns the channel name this provider manages.
func (Rules) Channel() string { return "rules" }

func agentsPath(env provider.Env) string {
	return filepath.Join(env.Root, "AGENTS.md")
}

// Observe reads AGENTS.md and returns a Resource for each rule in the
// ainfra-managed region. A missing file or absent region is treated as no
// resources. ContentHash is left empty; the orchestrator backfills it.
func (Rules) Observe(env provider.Env) ([]provider.Resource, error) {
	ids, err := fsmerge.ManagedRegionIDs(env.FS, agentsPath(env))
	if err != nil {
		return nil, err
	}
	resources := make([]provider.Resource, 0, len(ids))
	for _, id := range ids {
		resources = append(resources, provider.Resource{ID: id, Channel: "rules"})
	}
	return resources, nil
}

// Apply executes the channel plan against the managed region of AGENTS.md.
// When env.DryRun is true the result is computed but the file is not written.
func (Rules) Apply(env provider.Env, plan provider.ChannelPlan) (provider.ApplyResult, error) {
	blocks := map[string]string{}
	ownedIDs := make([]string, 0, len(plan.Changes))
	var applied []provider.Change

	for _, c := range plan.Changes {
		if c.Kind == provider.ChangeNoop {
			continue
		}
		ownedIDs = append(ownedIDs, c.ID)
		applied = append(applied, c)
		if c.Kind == provider.ChangeCreate || c.Kind == provider.ChangeUpdate {
			content, _ := c.Resource.Payload["content"].(string)
			blocks[c.ID] = content
		}
		// ChangeDelete: in ownedIDs, not in blocks — the merge removes it.
	}

	if len(ownedIDs) == 0 {
		return provider.ApplyResult{Channel: "rules"}, nil
	}

	if !env.DryRun {
		if err := fsmerge.MergeManagedRegion(env.FS, agentsPath(env), blocks, ownedIDs); err != nil {
			return provider.ApplyResult{}, err
		}
	}

	return provider.ApplyResult{Channel: "rules", Applied: applied}, nil
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/provider/codex/ -run TestRules`
Expected: PASS — all six tests.

- [ ] **Step 5: Commit**

```bash
git add internal/provider/codex/rules.go internal/provider/codex/rules_test.go
git commit -m "Add the Codex rules provider"
```

---

## Task 5: Wire the Codex set into `agentset.ForAgent`

**Files:**
- Modify: `internal/provider/agentset/agentset.go`
- Modify: `internal/provider/agentset/agentset_test.go`

- [ ] **Step 1: Replace the codex test**

In `internal/provider/agentset/agentset_test.go`, replace the function `TestForAgentCodexNotYetAvailable` with:

```go
func TestForAgentCodexReturnsItsChannels(t *testing.T) {
	ps, err := agentset.ForAgent(agent.Codex)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string]bool{"mcpServers": true, "rules": true, "cliTools": true}
	got := map[string]bool{}
	for _, p := range ps {
		got[p.Channel()] = true
	}
	if len(got) != len(want) {
		t.Fatalf("got %d distinct channels %v, want %d %v", len(got), got, len(want), want)
	}
	for ch := range want {
		if !got[ch] {
			t.Errorf("missing channel %q", ch)
		}
	}
}
```

Leave `TestForAgentClaudeCodeReturnsEveryChannel` and `TestForAgentUnknownErrors` unchanged.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/provider/agentset/ -run TestForAgentCodex`
Expected: FAIL — `ForAgent(agent.Codex)` currently returns an error, so `err != nil` fires `t.Fatalf`.

- [ ] **Step 3: Add the codex case to `ForAgent`**

In `internal/provider/agentset/agentset.go`, add the `codex` import and a `case agent.Codex` to the switch. The import block gains the `codex` package:

```go
import (
	"fmt"

	"github.com/MHilhorst/ainfra/internal/agent"
	"github.com/MHilhorst/ainfra/internal/provider"
	"github.com/MHilhorst/ainfra/internal/provider/claudecode"
	"github.com/MHilhorst/ainfra/internal/provider/codex"
	"github.com/MHilhorst/ainfra/internal/provider/shared"
)
```

and the switch in `ForAgent` gains, between the `agent.ClaudeCode` case and `default`:

```go
	case agent.Codex:
		return append([]provider.Provider{
			codex.MCP{},
			codex.Rules{},
		}, sharedProviders()...), nil
```

Leave the `agent.ClaudeCode` case, the `default` case, and `sharedProviders` unchanged.

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/provider/agentset/`
Expected: PASS — `TestForAgentCodexReturnsItsChannels`, `TestForAgentClaudeCodeReturnsEveryChannel`, and `TestForAgentUnknownErrors`.

- [ ] **Step 5: Commit**

```bash
git add internal/provider/agentset/agentset.go internal/provider/agentset/agentset_test.go
git commit -m "Wire the Codex provider set into ForAgent"
```

---

## Task 6: Full build, test, and end-to-end verification

**Files:** none — this task verifies the whole change.

- [ ] **Step 1: Build everything**

Run: `go build ./...`
Expected: no output, exit 0.

- [ ] **Step 2: Run the full test suite**

Run: `go test ./...`
Expected: every package reports `ok`, including `internal/provider/codex` and `internal/provider/fsmerge`.

- [ ] **Step 3: gofmt and vet**

Run: `gofmt -l . && go vet ./...`
Expected: `gofmt -l .` prints nothing (every file is formatted); `go vet` prints nothing and exits 0.

- [ ] **Step 4: End-to-end — reconcile an `agent: codex` manifest**

The Codex MCP provider observes `$HOME/.codex/config.toml`, so the check runs
under an isolated, empty `HOME` (`os.UserHomeDir` honours `$HOME`) to keep the
result deterministic and to never read or write the real home directory.

Run:
```bash
go build -o /tmp/ainfra-2b ./cmd/ainfra
mkdir -p /tmp/ainfra-codex-check/rules /tmp/ainfra-codex-home
cat > /tmp/ainfra-codex-check/ainfra.yaml <<'YAML'
version: 1
agent: codex
mcpServers:
  filesystem:
    transport: stdio
    command: npx
    args: ["-y", "@modelcontextprotocol/server-filesystem", "."]
    version: "0.6.2"
rules:
  team-conventions:
    source: ./rules/team.md
YAML
echo "Follow the team conventions." > /tmp/ainfra-codex-check/rules/team.md
HOME=/tmp/ainfra-codex-home /tmp/ainfra-2b --chdir /tmp/ainfra-codex-check lock && echo "lock OK"
HOME=/tmp/ainfra-codex-home /tmp/ainfra-2b --chdir /tmp/ainfra-codex-check plan; echo "plan exit=$?"
```
Expected: `lock` succeeds; `plan` exits 0 and shows the `mcpServers.filesystem` and `rules.team-conventions` entries as additions. The `hooks`/`skills`/`plugins`/`tools`/`commands` channels are absent from the manifest, so capability gating (Plan 1) raises no error.

- [ ] **Step 5: Clean up**

Run: `rm -rf /tmp/ainfra-2b /tmp/ainfra-codex-check /tmp/ainfra-codex-home`
Expected: no output.

- [ ] **Step 6: Final commit (only if Steps 1-3 required any fix)**

If Steps 1-3 surfaced a failure that needed a fix, commit it:

```bash
git add -A
git commit -m "Fix build and test fallout from the Codex provider set"
```

If Steps 1-3 passed clean, there is nothing to commit — skip this step.

---

## Self-Review

**Spec coverage** (against `2026-05-22-codex-provider-set-design.md`):
- §1 `codex/` package, two providers, `ForAgent` codex case → Tasks 3, 4, 5.
- §2 Codex MCP provider, `BurntSushi/toml` dependency, `MergeTOMLTables`, own-the-tables round-trip, `transport` ignored → Tasks 1, 3.
- §3 Codex rules provider, `AGENTS.md` managed region with per-rule markers, `MergeManagedRegion` → Tasks 2, 4.
- §4 data flow — unchanged; the providers slot into the existing orchestrator via `ForAgent` → Task 5.
- §5 error handling — missing files treated as empty (Observe returns nil; Apply creates); malformed TOML and unterminated regions are hard errors → Tasks 1, 2, 3, 4 (tests `TestMergeTOMLTablesRejectsMalformed`, `TestMergeManagedRegionRejectsUnterminatedRegion`).
- §6 testing — fsmerge helper tests, provider tests against the `MemFilesystem` fake, `ForAgent` codex test, end-to-end → Tasks 1-6.
- §7 non-goals — no `backgroundServices` gating, no new orchestrator/interface changes: respected; this plan adds only the `codex` package, two `fsmerge` files, and the `ForAgent` case.

**Placeholder scan:** none — every step has concrete code or an exact command.

**Type consistency:** `fsmerge.MergeTOMLTables(FS, string, string, map[string]any, []string) error`, `fsmerge.MergeManagedRegion(FS, string, map[string]string, []string) error`, and `fsmerge.ManagedRegionIDs(FS, string) ([]string, error)` are defined in Tasks 1-2 and called with those exact signatures in Tasks 3-4. `codex.MCP` and `codex.Rules` are zero-value structs implementing `provider.Provider` (`Channel`/`Observe`/`Apply`), defined in Tasks 3-4 and constructed in Task 5's `ForAgent`. `provider.Env` fields `FS`, `Home`, `Root`, `DryRun` are used as they exist on the current struct. The payload keys (`command`, `args`, `env`, `transport`, `content`, `target`) match what `resolve.RenderResources` populates for the mcpServers and rules channels.
