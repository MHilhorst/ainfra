# Phase 3 — Plan 1: Schema Completion Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Complete the manifest schema for the four unbuilt channels (skills, plugins, rules, tools), resolve them and inline MCP servers into the lockfile, and record the per-entry dependency edges and manifest hash that the provider layer needs.

**Architecture:** This is the first of six plans for Phase 3 (see `docs/superpowers/specs/2026-05-21-phase-3-channel-providers-design.md` §9). It finishes Part A of that spec — the schema work — without touching the provider layer. New channels resolve the same non-templated way `hooks` and `commands` already do: hash the declared config, record a `lockfile.Entry`, add `requires` graph edges. Each lock `Entry` also gains a `requires` field (node-refs) so a later plan can rebuild the dependency graph from the lockfile alone.

**Tech Stack:** Go 1.x, `gopkg.in/yaml.v3`, standard `testing`.

**Deviation from spec §2.4:** the spec proposed a single `Lock.ApplyOrder` slice. That would write personal-layer node refs into the committed `ainfra.lock`, which `splitByLayer` exists to prevent. This plan instead stores `requires` node-refs on each `Entry`; the orchestrator (Plan 6) rebuilds and topo-sorts the graph from the merged locks. The spec has been updated to match.

---

### Task 1: Manifest types for skills, plugins, rules, tools

**Files:**
- Modify: `internal/manifest/types.go`
- Test: `internal/manifest/types_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/manifest/types_test.go`:

```go
func TestUnmarshalNewChannels(t *testing.T) {
	src := `version: 1
skills:
  debug:
    source: "git+https://github.com/acme/skills.git@v1.4.0#debug"
    version: "1.4.0"
plugins:
  tvt:
    source: "npm:@acme/tvt-plugin@2.0.1"
    version: "2.0.1"
rules:
  team:
    target: CLAUDE.md
    source: ./rules/team.md
    version: "1"
tools:
  builtins:
    disabled: [WebFetch]
  permissions:
    allow: ["Bash(go test:*)"]
    deny: ["Bash(rm -rf:*)"]
`
	var m Manifest
	if err := yaml.Unmarshal([]byte(src), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m.Skills["debug"].Version != "1.4.0" {
		t.Errorf("skill version = %q", m.Skills["debug"].Version)
	}
	if m.Plugins["tvt"].Source != "npm:@acme/tvt-plugin@2.0.1" {
		t.Errorf("plugin source = %q", m.Plugins["tvt"].Source)
	}
	if m.Rules["team"].Target != "CLAUDE.md" {
		t.Errorf("rule target = %q", m.Rules["team"].Target)
	}
	if m.Tools == nil || len(m.Tools.Builtins.Disabled) != 1 || m.Tools.Builtins.Disabled[0] != "WebFetch" {
		t.Errorf("tools.builtins.disabled = %+v", m.Tools)
	}
	if m.Tools.Permissions.Deny[0] != "Bash(rm -rf:*)" {
		t.Errorf("tools.permissions.deny = %+v", m.Tools.Permissions.Deny)
	}
}
```

Ensure `internal/manifest/types_test.go` imports `"gopkg.in/yaml.v3"` and `"testing"`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/manifest/ -run TestUnmarshalNewChannels -v`
Expected: FAIL — `m.Skills` undefined (compile error).

- [ ] **Step 3: Add the types**

In `internal/manifest/types.go`, add these four fields to the `Manifest` struct, after `Commands`:

```go
	Skills             map[string]Skill             `yaml:"skills"`
	Plugins            map[string]Plugin            `yaml:"plugins"`
	Rules              map[string]Rule              `yaml:"rules"`
	Tools              *Tools                       `yaml:"tools"`
```

Then add the type definitions at the end of the file:

```go
// Skill is a Claude Code skill bundle (spec §10, channel 2).
type Skill struct {
	Source      string    `yaml:"source"`
	Version     string    `yaml:"version"`
	Requires    []Require `yaml:"requires"`
	Enabled     *bool     `yaml:"enabled"`
	Overridable bool      `yaml:"overridable"`
}

// Plugin is an installable Claude Code plugin bundle (spec §10, channel 3).
type Plugin struct {
	Source      string    `yaml:"source"`
	Version     string    `yaml:"version"`
	Requires    []Require `yaml:"requires"`
	Enabled     *bool     `yaml:"enabled"`
	Overridable bool      `yaml:"overridable"`
}

// Rule is a static context file — CLAUDE.md or similar (spec §10, channel 4).
type Rule struct {
	Target      string    `yaml:"target"`
	Source      string    `yaml:"source"`
	Version     string    `yaml:"version"`
	Requires    []Require `yaml:"requires"`
	Enabled     *bool     `yaml:"enabled"`
	Overridable bool      `yaml:"overridable"`
}

// Tools is the tools channel — built-in toggles and permission policy
// (spec §10, channel 5). One block per layer; a pointer so an absent block is
// distinguishable from an empty one.
type Tools struct {
	Builtins    ToolBuiltins    `yaml:"builtins"`
	Permissions ToolPermissions `yaml:"permissions"`
}

// ToolBuiltins lists built-in tools switched off team-wide.
type ToolBuiltins struct {
	Disabled []string `yaml:"disabled"`
}

// ToolPermissions is the allow/deny permission policy for tools.
type ToolPermissions struct {
	Allow []string `yaml:"allow"`
	Deny  []string `yaml:"deny"`
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/manifest/ -run TestUnmarshalNewChannels -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/manifest/types.go internal/manifest/types_test.go
git commit -m "Add manifest types for skills, plugins, rules, tools channels"
```

---

### Task 2: Validate the skills and plugins channels

**Files:**
- Modify: `internal/manifest/validate.go`
- Test: `internal/manifest/validate_test.go`

- [ ] **Step 1: Write the failing tests**

Add to `internal/manifest/validate_test.go`:

```go
func TestValidateRejectsRemoteSkillWithoutVersion(t *testing.T) {
	m := &Manifest{Version: 1, Skills: map[string]Skill{
		"s": {Source: "git+https://github.com/acme/skills.git@main#s"},
	}}
	d := asDiagnostic(t, Validate(m))
	if !strings.Contains(d.Summary, "pin an exact version") {
		t.Errorf("summary = %q", d.Summary)
	}
	if d.Path != "skills.s" {
		t.Errorf("path = %q", d.Path)
	}
}

func TestValidateAcceptsLocalSkillWithoutVersion(t *testing.T) {
	m := &Manifest{Version: 1, Skills: map[string]Skill{
		"s": {Source: "./skills/s"},
	}}
	if err := Validate(m); err != nil {
		t.Fatalf("local-path skill needs no version: %v", err)
	}
}

func TestValidateRejectsSkillWithoutSource(t *testing.T) {
	m := &Manifest{Version: 1, Skills: map[string]Skill{"s": {}}}
	d := asDiagnostic(t, Validate(m))
	if !strings.Contains(d.Summary, "source") {
		t.Errorf("summary = %q", d.Summary)
	}
}

func TestValidateRejectsRemotePluginWithoutVersion(t *testing.T) {
	m := &Manifest{Version: 1, Plugins: map[string]Plugin{
		"p": {Source: "npm:@acme/p"},
	}}
	d := asDiagnostic(t, Validate(m))
	if !strings.Contains(d.Summary, "pin an exact version") {
		t.Errorf("summary = %q", d.Summary)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/manifest/ -run 'Skill|Plugin' -v`
Expected: FAIL — the new diagnostics are not produced.

- [ ] **Step 3: Add a remote-source helper and the validation**

In `internal/manifest/validate.go`, add `"strings"` to the import block, then add this helper above `Validate`:

```go
// isRemoteSource reports whether a source string fetches from a remote
// registry (git or npm) and therefore must pin an exact version — the same
// drift-detection rule MCP servers follow (spec §5.1). A local path does not.
func isRemoteSource(src string) bool {
	return strings.HasPrefix(src, "git+") || strings.HasPrefix(src, "npm:")
}
```

Then, inside `Validate`, after the `m.Commands` loop and before `return nil`, add:

```go
	for _, id := range slices.Sorted(maps.Keys(m.Skills)) {
		s := m.Skills[id]
		if s.Source == "" {
			return &diag.Diagnostic{
				Summary: "skill declares no source",
				Path:    "skills." + id,
				Detail:  fmt.Sprintf("Skill %q has no source.", id),
				Hint:    "Add a source field — a local path, git+https://… ref, or npm: ref.",
			}
		}
		if isRemoteSource(s.Source) && s.Version == "" {
			return &diag.Diagnostic{
				Summary: "remote skill must pin an exact version",
				Path:    "skills." + id,
				Detail:  fmt.Sprintf("Skill %q fetches from a remote source but declares no version.", id),
				Hint:    `Add a version field, e.g.  version: "1.4.0"`,
			}
		}
	}
	for _, id := range slices.Sorted(maps.Keys(m.Plugins)) {
		p := m.Plugins[id]
		if p.Source == "" {
			return &diag.Diagnostic{
				Summary: "plugin declares no source",
				Path:    "plugins." + id,
				Detail:  fmt.Sprintf("Plugin %q has no source.", id),
				Hint:    "Add a source field — an npm: ref or a marketplace ref.",
			}
		}
		if isRemoteSource(p.Source) && p.Version == "" {
			return &diag.Diagnostic{
				Summary: "remote plugin must pin an exact version",
				Path:    "plugins." + id,
				Detail:  fmt.Sprintf("Plugin %q fetches from a remote source but declares no version.", id),
				Hint:    `Add a version field, e.g.  version: "2.0.1"`,
			}
		}
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/manifest/ -run 'Skill|Plugin' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/manifest/validate.go internal/manifest/validate_test.go
git commit -m "Validate skills and plugins channels"
```

---

### Task 3: Validate the rules and tools channels

**Files:**
- Modify: `internal/manifest/validate.go`
- Test: `internal/manifest/validate_test.go`

- [ ] **Step 1: Write the failing tests**

Add to `internal/manifest/validate_test.go`:

```go
func TestValidateRejectsRuleWithoutTarget(t *testing.T) {
	m := &Manifest{Version: 1, Rules: map[string]Rule{
		"r": {Source: "./rules/r.md"},
	}}
	d := asDiagnostic(t, Validate(m))
	if !strings.Contains(d.Summary, "target") {
		t.Errorf("summary = %q", d.Summary)
	}
}

func TestValidateRejectsRuleWithoutSource(t *testing.T) {
	m := &Manifest{Version: 1, Rules: map[string]Rule{
		"r": {Target: "CLAUDE.md"},
	}}
	d := asDiagnostic(t, Validate(m))
	if !strings.Contains(d.Summary, "source") {
		t.Errorf("summary = %q", d.Summary)
	}
}

func TestValidateRejectsEmptyDisabledBuiltin(t *testing.T) {
	m := &Manifest{Version: 1, Tools: &Tools{
		Builtins: ToolBuiltins{Disabled: []string{""}},
	}}
	d := asDiagnostic(t, Validate(m))
	if !strings.Contains(d.Summary, "empty") {
		t.Errorf("summary = %q", d.Summary)
	}
}

func TestValidateAcceptsValidNewChannels(t *testing.T) {
	m := &Manifest{Version: 1,
		Rules: map[string]Rule{"r": {Target: "CLAUDE.md", Source: "./rules/r.md"}},
		Tools: &Tools{
			Builtins:    ToolBuiltins{Disabled: []string{"WebFetch"}},
			Permissions: ToolPermissions{Allow: []string{"Bash(go test:*)"}},
		},
	}
	if err := Validate(m); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/manifest/ -run 'Rule|Builtin|NewChannels' -v`
Expected: FAIL — the new diagnostics are not produced.

- [ ] **Step 3: Add the validation**

In `internal/manifest/validate.go`, inside `Validate`, after the `m.Plugins` loop and before `return nil`, add:

```go
	for _, id := range slices.Sorted(maps.Keys(m.Rules)) {
		r := m.Rules[id]
		if r.Source == "" {
			return &diag.Diagnostic{
				Summary: "rule declares no source",
				Path:    "rules." + id,
				Detail:  fmt.Sprintf("Rule %q has no source file.", id),
				Hint:    "Add a source field pointing at the context file.",
			}
		}
		if r.Target == "" {
			return &diag.Diagnostic{
				Summary: "rule declares no target",
				Path:    "rules." + id,
				Detail:  fmt.Sprintf("Rule %q does not say where its file lands.", id),
				Hint:    "Add a target field, e.g.  target: CLAUDE.md",
			}
		}
		if isRemoteSource(r.Source) && r.Version == "" {
			return &diag.Diagnostic{
				Summary: "remote rule must pin an exact version",
				Path:    "rules." + id,
				Detail:  fmt.Sprintf("Rule %q fetches from a remote source but declares no version.", id),
				Hint:    `Add a version field, e.g.  version: "1"`,
			}
		}
	}
	if m.Tools != nil {
		check := func(field string, list []string) *diag.Diagnostic {
			for i, v := range list {
				if strings.TrimSpace(v) == "" {
					return &diag.Diagnostic{
						Summary: fmt.Sprintf("%s has an empty entry", field),
						Path:    fmt.Sprintf("%s[%d]", field, i),
						Detail:  "Every entry must be a non-empty string.",
						Hint:    "Remove the blank entry or fill it in.",
					}
				}
			}
			return nil
		}
		if d := check("tools.builtins.disabled", m.Tools.Builtins.Disabled); d != nil {
			return d
		}
		if d := check("tools.permissions.allow", m.Tools.Permissions.Allow); d != nil {
			return d
		}
		if d := check("tools.permissions.deny", m.Tools.Permissions.Deny); d != nil {
			return d
		}
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/manifest/ -v`
Expected: PASS — all manifest tests, including the existing ones.

- [ ] **Step 5: Commit**

```bash
git add internal/manifest/validate.go internal/manifest/validate_test.go
git commit -m "Validate rules and tools channels"
```

---

### Task 4: Lockfile schema for the new channels and per-entry requires

**Files:**
- Modify: `internal/lockfile/types.go`, `internal/lockfile/io.go`
- Test: `internal/lockfile/io_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/lockfile/io_test.go`:

```go
func TestReadInitialisesNewChannelMaps(t *testing.T) {
	l, err := Read(filepath.Join(t.TempDir(), "absent.lock"))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if l.Entries.Skills == nil || l.Entries.Plugins == nil ||
		l.Entries.Rules == nil || l.Entries.Tools == nil {
		t.Errorf("new channel maps must be non-nil: %+v", l.Entries)
	}
}

func TestEntryRoundTripsRequires(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ainfra.lock")
	in := &Lock{Version: 1, Entries: Entries{
		Skills: map[string]Entry{
			"s": {Layer: "repo", ContentHash: "sha256:x", Requires: []string{"cli:node"}},
		},
	}}
	if err := Write(path, in); err != nil {
		t.Fatalf("Write: %v", err)
	}
	out, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	got := out.Entries.Skills["s"].Requires
	if len(got) != 1 || got[0] != "cli:node" {
		t.Errorf("requires round-trip = %v", got)
	}
}
```

Confirm `internal/lockfile/io_test.go` imports `"path/filepath"` and `"testing"`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/lockfile/ -run 'NewChannel|Requires' -v`
Expected: FAIL — `Entries.Skills` and `Entry.Requires` undefined (compile error).

- [ ] **Step 3: Extend the lockfile types**

In `internal/lockfile/types.go`, add four fields to the `Entries` struct after `CLITools`:

```go
	Skills             map[string]Entry `yaml:"skills"`
	Plugins            map[string]Entry `yaml:"plugins"`
	Rules              map[string]Entry `yaml:"rules"`
	Tools              map[string]Entry `yaml:"tools"`
```

Add one field to the `Entry` struct, immediately before `ContentHash`:

```go
	Requires        []string       `yaml:"requires,omitempty"`
```

In `internal/lockfile/io.go`, extend `ensureMaps` with four more nil checks before `return l`:

```go
	if l.Entries.Skills == nil {
		l.Entries.Skills = map[string]Entry{}
	}
	if l.Entries.Plugins == nil {
		l.Entries.Plugins = map[string]Entry{}
	}
	if l.Entries.Rules == nil {
		l.Entries.Rules = map[string]Entry{}
	}
	if l.Entries.Tools == nil {
		l.Entries.Tools = map[string]Entry{}
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/lockfile/ -v`
Expected: PASS — all lockfile tests.

- [ ] **Step 5: Commit**

```bash
git add internal/lockfile/types.go internal/lockfile/io.go internal/lockfile/io_test.go
git commit -m "Add lockfile entries for new channels and per-entry requires"
```

---

### Task 5: Resolve skills, plugins, and rules in the lock pipeline

**Files:**
- Modify: `internal/resolve/pipeline.go`
- Test: `internal/resolve/pipeline_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/resolve/pipeline_test.go`:

```go
func TestLockPipelineResolvesSkillsPluginsRules(t *testing.T) {
	dir := t.TempDir()
	manifestYAML := `version: 1
cliTools:
  node: { versionConstraint: ">=20" }
skills:
  debug:
    source: ./skills/debug
    requires: [ { cliTool: node } ]
plugins:
  tvt: { source: "npm:@acme/tvt@2.0.1", version: "2.0.1" }
rules:
  team: { target: CLAUDE.md, source: ./rules/team.md }
`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(manifestYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RunLock(dir); err != nil {
		t.Fatalf("RunLock: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "ainfra.lock"))
	if err != nil {
		t.Fatalf("lock not written: %v", err)
	}
	out := string(data)
	for _, want := range []string{"skills:", "debug", "plugins:", "tvt", "rules:", "team", "cli:node"} {
		if !strings.Contains(out, want) {
			t.Errorf("lock missing %q\n---\n%s", want, out)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/resolve/ -run SkillsPluginsRules -v`
Expected: FAIL — the lock has no `skills:`/`plugins:`/`rules:` entries.

- [ ] **Step 3: Add a requireRefs helper and resolve the three channels**

In `internal/resolve/pipeline.go`, add this helper near `addRequireEdges`:

```go
// requireRefs converts an entry's requires edges into the node-ref strings the
// dependency graph uses ("cli:node", "svc:tunnel", "pre:internet"). The lock
// stores these per entry so plan/apply/check can rebuild the graph without
// re-reading the manifest.
func requireRefs(reqs []manifest.Require) []string {
	var refs []string
	for _, r := range reqs {
		switch {
		case r.Service != "":
			refs = append(refs, "svc:"+r.Service)
		case r.CLITool != "":
			refs = append(refs, "cli:"+r.CLITool)
		case r.Precondition != "":
			refs = append(refs, "pre:"+r.Precondition)
		}
	}
	return refs
}
```

Refactor `addRequireEdges` to reuse it, replacing its whole body:

```go
func addRequireEdges(g *graph.Graph, fromNode string, reqs []manifest.Require) {
	for _, ref := range requireRefs(reqs) {
		g.AddNode(ref)
		g.AddEdge(fromNode, ref)
	}
}
```

Then, inside `RunLock`, in the second layer loop (the one resolving `m.Hooks` and `m.Commands`), after the `m.Commands` block and still inside the `for _, layerName` loop, add:

```go
		for _, id := range slices.Sorted(maps.Keys(m.Skills)) {
			s := m.Skills[id]
			node := "skill:" + id
			g.AddNode(node)
			addRequireEdges(g, node, s.Requires)
			lock.Entries.Skills[id] = lockfile.Entry{
				Layer:    string(layerName),
				Version:  s.Version,
				Requires: requireRefs(s.Requires),
				ContentHash: lockfile.ContentHash(map[string]any{
					"source": s.Source, "version": s.Version, "target": "",
				}),
			}
		}
		for _, id := range slices.Sorted(maps.Keys(m.Plugins)) {
			p := m.Plugins[id]
			node := "plugin:" + id
			g.AddNode(node)
			addRequireEdges(g, node, p.Requires)
			lock.Entries.Plugins[id] = lockfile.Entry{
				Layer:    string(layerName),
				Version:  p.Version,
				Requires: requireRefs(p.Requires),
				ContentHash: lockfile.ContentHash(map[string]any{
					"source": p.Source, "version": p.Version,
				}),
			}
		}
		for _, id := range slices.Sorted(maps.Keys(m.Rules)) {
			r := m.Rules[id]
			node := "rule:" + id
			g.AddNode(node)
			addRequireEdges(g, node, r.Requires)
			lock.Entries.Rules[id] = lockfile.Entry{
				Layer:    string(layerName),
				Version:  r.Version,
				Requires: requireRefs(r.Requires),
				ContentHash: lockfile.ContentHash(map[string]any{
					"source": r.Source, "version": r.Version, "target": r.Target,
				}),
			}
		}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/resolve/ -run SkillsPluginsRules -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/resolve/pipeline.go internal/resolve/pipeline_test.go
git commit -m "Resolve skills, plugins, and rules channels into the lockfile"
```

---

### Task 6: Resolve the tools channel in the lock pipeline

**Files:**
- Modify: `internal/resolve/pipeline.go`
- Test: `internal/resolve/pipeline_test.go`

The `tools` block is a per-layer singleton; it is recorded in `lock.Entries.Tools` keyed by layer name so `splitByLayer` (Task 9) can route a personal-only `tools` block to the personal lock.

- [ ] **Step 1: Write the failing test**

Add to `internal/resolve/pipeline_test.go`:

```go
func TestLockPipelineResolvesTools(t *testing.T) {
	dir := t.TempDir()
	manifestYAML := `version: 1
tools:
  builtins:
    disabled: [WebFetch]
  permissions:
    allow: ["Bash(go test:*)"]
    deny: ["Bash(rm -rf:*)"]
`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(manifestYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RunLock(dir); err != nil {
		t.Fatalf("RunLock: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "ainfra.lock"))
	if err != nil {
		t.Fatalf("lock not written: %v", err)
	}
	out := string(data)
	for _, want := range []string{"tools:", "repo:", "contentHash:"} {
		if !strings.Contains(out, want) {
			t.Errorf("lock missing %q\n---\n%s", want, out)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/resolve/ -run ResolvesTools -v`
Expected: FAIL — the lock has no `tools:` section.

- [ ] **Step 3: Resolve the tools channel**

In `internal/resolve/pipeline.go`, inside `RunLock`, in the same second layer loop, after the `m.Rules` block, add:

```go
		if m.Tools != nil {
			node := "tools:" + string(layerName)
			g.AddNode(node)
			lock.Entries.Tools[string(layerName)] = lockfile.Entry{
				Layer: string(layerName),
				ContentHash: lockfile.ContentHash(map[string]any{
					"disabled": m.Tools.Builtins.Disabled,
					"allow":    m.Tools.Permissions.Allow,
					"deny":     m.Tools.Permissions.Deny,
				}),
			}
		}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/resolve/ -run ResolvesTools -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/resolve/pipeline.go internal/resolve/pipeline_test.go
git commit -m "Resolve the tools channel into the lockfile"
```

---

### Task 7: Resolve inline (non-templated) MCP servers

**Files:**
- Modify: `internal/resolve/pipeline.go`
- Test: `internal/resolve/pipeline_test.go`

`RunLock`'s first loop skips any `mcpServers` entry with no `template` (`if srv.Template == "" { continue }`). Those inline servers are never locked. This task records them in the second layer loop.

- [ ] **Step 1: Write the failing test**

Add to `internal/resolve/pipeline_test.go`:

```go
func TestLockPipelineResolvesInlineMCPServer(t *testing.T) {
	dir := t.TempDir()
	manifestYAML := `version: 1
mcpServers:
  github:
    command: npx
    args: ["-y", "@modelcontextprotocol/server-github"]
    version: "2025.4.0"
    transport: stdio
`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(manifestYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RunLock(dir); err != nil {
		t.Fatalf("RunLock: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "ainfra.lock"))
	if err != nil {
		t.Fatalf("lock not written: %v", err)
	}
	out := string(data)
	for _, want := range []string{"github", "version: 2025.4.0", "contentHash:"} {
		if !strings.Contains(out, want) {
			t.Errorf("lock missing %q\n---\n%s", want, out)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/resolve/ -run InlineMCPServer -v`
Expected: FAIL — the lock has no `github` entry.

- [ ] **Step 3: Resolve inline MCP servers**

In `internal/resolve/pipeline.go`, inside `RunLock`, in the second layer loop, before the `m.Skills` block added in Task 5, add:

```go
		for _, id := range slices.Sorted(maps.Keys(m.MCPServers)) {
			srv := m.MCPServers[id]
			if srv.Template != "" {
				continue // templated servers are resolved in the first loop
			}
			node := "mcp:" + id
			g.AddNode(node)
			addRequireEdges(g, node, srv.Requires)
			lock.Entries.MCPServers[id] = lockfile.Entry{
				Layer:    string(layerName),
				Version:  srv.Version,
				Requires: requireRefs(srv.Requires),
				ContentHash: lockfile.ContentHash(map[string]any{
					"command": srv.Command, "args": srv.Args,
					"version": srv.Version, "transport": srv.Transport,
					"env": toAnyMap(srv.Env),
				}),
			}
		}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/resolve/ -run InlineMCPServer -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/resolve/pipeline.go internal/resolve/pipeline_test.go
git commit -m "Resolve inline non-templated MCP servers into the lockfile"
```

---

### Task 8: Backfill requires on templated MCP, service, hook, and command entries

**Files:**
- Modify: `internal/resolve/pipeline.go`
- Test: `internal/resolve/pipeline_test.go`

The new `Entry.Requires` field is populated for skills/plugins/rules/inline-MCP, but the four pre-existing entry-creation sites still leave it empty. The orchestrator rebuilds the graph from these refs, so every entry must carry them.

- [ ] **Step 1: Write the failing test**

Add to `internal/resolve/pipeline_test.go`:

```go
func TestLockPipelineRecordsRequiresOnExistingChannels(t *testing.T) {
	dir := t.TempDir()
	manifestYAML := `version: 1
cliTools:
  node: { versionConstraint: ">=20" }
  ssh: { versionConstraint: ">=8" }
templates:
  tun:
    params: { host: { type: string, required: true } }
    resolved: { tunnelPort: { kind: allocated-port } }
    produces:
      mcpServer:
        command: npx
        version: "1.0.0"
        requires: [ { service: "${instance.id}-tunnel" } ]
      backgroundService:
        id: "${instance.id}-tunnel"
        kind: ssh-tunnel
        requires: [ { cliTool: ssh } ]
mcpServers:
  db-a: { template: tun, params: { host: a.example } }
hooks:
  guard: { event: Stop, command: "echo x", requires: [ { cliTool: node } ] }
commands:
  ship: { source: ./ship.md, requires: [ { cliTool: node } ] }
`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(manifestYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RunLock(dir); err != nil {
		t.Fatalf("RunLock: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "ainfra.lock"))
	if err != nil {
		t.Fatalf("lock not written: %v", err)
	}
	out := string(data)
	// Each existing channel must now carry its requires node-refs.
	for _, want := range []string{"svc:db-a-tunnel", "cli:ssh", "cli:node"} {
		if !strings.Contains(out, want) {
			t.Errorf("lock missing requires ref %q\n---\n%s", want, out)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/resolve/ -run RecordsRequiresOnExistingChannels -v`
Expected: FAIL — entries have no `requires:` lines.

- [ ] **Step 3: Populate Requires at the four existing sites**

In `internal/resolve/pipeline.go`, inside `RunLock`:

In the first loop, where the templated-MCP `entry` is built — inside `if out.MCPServer != nil { ... }` — add a line after `entry.ContentHash = ...`:

```go
			entry.Requires = requireRefs(out.MCPServer.Requires)
```

Still in the first loop, where the background service entry is created (`lock.Entries.BackgroundServices[out.Service.ID] = lockfile.Entry{...}`), add a `Requires` field to that struct literal:

```go
			lock.Entries.BackgroundServices[out.Service.ID] = lockfile.Entry{
				Layer: string(ti.layer), Resolved: resolved,
				Requires:    requireRefs(out.Service.Requires),
				ContentHash: lockfile.ContentHash(out.Service.Spec),
			}
```

In the second loop, in the `m.Hooks` block, add `Requires` to the `lockfile.Entry` literal:

```go
			lock.Entries.Hooks[id] = lockfile.Entry{
				Layer:    string(layerName),
				Requires: requireRefs(h.Requires),
				ContentHash: lockfile.ContentHash(map[string]any{
					"event": h.Event, "matcher": h.Matcher, "command": h.Command,
					"source": h.Source, "timeout": h.Timeout,
				}),
			}
```

In the second loop, in the `m.Commands` block, add `Requires` to the `lockfile.Entry` literal:

```go
			lock.Entries.Commands[id] = lockfile.Entry{
				Layer:    string(layerName),
				Version:  c.Version,
				Requires: requireRefs(c.Requires),
				ContentHash: lockfile.ContentHash(map[string]any{
					"source": c.Source, "description": c.Description, "version": c.Version,
				}),
			}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/resolve/ -run RecordsRequiresOnExistingChannels -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/resolve/pipeline.go internal/resolve/pipeline_test.go
git commit -m "Record requires node-refs on all existing lock entries"
```

---

### Task 9: Record the manifest hash and route new channels through splitByLayer

**Files:**
- Modify: `internal/resolve/pipeline.go`
- Test: `internal/resolve/pipeline_test.go`

`splitByLayer` builds the committed and personal locks but does not copy the new channel maps, and `ManifestHash` is never set. The committed lock hashes only the team+repo input; the personal lock hashes only the personal input — so a developer editing their personal layer never dirties the committed `ainfra.lock`.

- [ ] **Step 1: Write the failing test**

Add to `internal/resolve/pipeline_test.go`:

```go
func TestLockPipelineRecordsManifestHash(t *testing.T) {
	dir := t.TempDir()
	manifestYAML := `version: 1
rules:
  team: { target: CLAUDE.md, source: ./rules/team.md }
`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(manifestYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RunLock(dir); err != nil {
		t.Fatalf("RunLock: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "ainfra.lock"))
	if err != nil {
		t.Fatalf("lock not written: %v", err)
	}
	if !strings.Contains(string(data), "manifestHash: sha256:") {
		t.Errorf("ainfra.lock missing manifestHash\n---\n%s", data)
	}
	// The rules entry must survive the committed/personal split.
	if !strings.Contains(string(data), "team") {
		t.Errorf("ainfra.lock dropped the rules entry\n---\n%s", data)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/resolve/ -run RecordsManifestHash -v`
Expected: FAIL — `manifestHash:` is absent, and `splitByLayer` drops the rules entry.

- [ ] **Step 3: Hash the manifest and route the new channels**

In `internal/resolve/pipeline.go`, add this helper near `splitByLayer`:

```go
// manifestHash hashes only the named layers, so the committed lock's hash
// depends on team+repo input alone and a personal-layer edit never dirties
// the committed ainfra.lock.
func manifestHash(layers map[manifest.Layer]*manifest.Manifest, want ...manifest.Layer) string {
	subset := map[string]*manifest.Manifest{}
	for _, ln := range want {
		if m, ok := layers[ln]; ok {
			subset[string(ln)] = m
		}
	}
	return lockfile.ContentHash(subset)
}
```

In `splitByLayer`, extend the `mk()` closure's `Entries` literal with the four new maps:

```go
		return &lockfile.Lock{Version: 1, GeneratedAt: l.GeneratedAt, Entries: lockfile.Entries{
			MCPServers: map[string]lockfile.Entry{}, BackgroundServices: map[string]lockfile.Entry{},
			Hooks: map[string]lockfile.Entry{}, Commands: map[string]lockfile.Entry{},
			CLITools: map[string]lockfile.Entry{},
			Skills:   map[string]lockfile.Entry{}, Plugins: map[string]lockfile.Entry{},
			Rules:    map[string]lockfile.Entry{}, Tools: map[string]lockfile.Entry{}}}
```

Still in `splitByLayer`, after the existing four `route(...)` calls and before `return committed, personal`, add four more:

```go
	route(func(x *lockfile.Lock) map[string]lockfile.Entry { return x.Entries.Skills }, l.Entries.Skills)
	route(func(x *lockfile.Lock) map[string]lockfile.Entry { return x.Entries.Plugins }, l.Entries.Plugins)
	route(func(x *lockfile.Lock) map[string]lockfile.Entry { return x.Entries.Rules }, l.Entries.Rules)
	route(func(x *lockfile.Lock) map[string]lockfile.Entry { return x.Entries.Tools }, l.Entries.Tools)
```

Finally, in `RunLock`, replace the closing block that currently reads:

```go
	committed, personal := splitByLayer(lock)
	if err := lockfile.Write(filepath.Join(dir, "ainfra.lock"), committed); err != nil {
		return err
	}
	return lockfile.Write(filepath.Join(dir, "ainfra.personal.lock"), personal)
```

with:

```go
	committed, personal := splitByLayer(lock)
	committed.ManifestHash = manifestHash(layers, manifest.LayerTeam, manifest.LayerRepo)
	personal.ManifestHash = manifestHash(layers, manifest.LayerPersonal)
	if err := lockfile.Write(filepath.Join(dir, "ainfra.lock"), committed); err != nil {
		return err
	}
	return lockfile.Write(filepath.Join(dir, "ainfra.personal.lock"), personal)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/resolve/ -run RecordsManifestHash -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/resolve/pipeline.go internal/resolve/pipeline_test.go
git commit -m "Record manifest hash and route new channels through splitByLayer"
```

---

### Task 10: Full-suite verification and example regression check

**Files:**
- No source changes expected; this task verifies the whole plan.

- [ ] **Step 1: Run the full build and test suite**

Run: `go build ./... && go test ./...`
Expected: all packages build; all tests PASS, including the pre-existing suites for `manifest`, `lockfile`, `resolve`, `cli`, and `cmd/ainfra`.

- [ ] **Step 2: Confirm the worked example still resolves**

Run: `go run ./cmd/ainfra --chdir examples/multi-database lock`
Expected: prints the resolved-server summary and writes `ainfra.lock` + `ainfra.personal.lock` with no error. The committed lock now contains a `manifestHash:` line and `requires:` lines on entries that declare dependencies.

- [ ] **Step 3: Confirm validate still passes on the example**

Run: `go run ./cmd/ainfra --chdir examples/multi-database validate`
Expected: exits 0 with no diagnostics.

- [ ] **Step 4: Revert any incidental change to the example lockfiles**

The example's checked-in `examples/multi-database/ainfra.lock` and `ainfra.personal.lock` will have been rewritten with the new fields. Inspect the diff; if the only changes are the new `manifestHash:` and `requires:` lines, keep them — they are the correct new output. If anything else changed, investigate before committing.

Run: `git diff --stat examples/multi-database/`

- [ ] **Step 5: Commit the regenerated example lockfiles**

```bash
git add examples/multi-database/ainfra.lock examples/multi-database/ainfra.personal.lock
git commit -m "Regenerate multi-database example lockfiles with manifest hash and requires"
```

---

## Self-Review

**Spec coverage (spec §2, Part A):**
- §2.1 manifest types — Task 1.
- §2.2 validation — Tasks 2 (skills, plugins) and 3 (rules, tools).
- §2.3 resolution into the lockfile, including inline MCP servers — Tasks 5, 6, 7; new `Entries` maps — Task 4.
- §2.4 `ManifestHash` — Task 9. The `applyOrder` half of §2.4 is intentionally replaced by per-entry `requires` (Tasks 4, 5, 7, 8) — see the deviation note in the header; the spec was updated to match.

**Type consistency:** `Skill`, `Plugin`, `Rule`, `Tools`, `ToolBuiltins`, `ToolPermissions` are defined once (Task 1) and referenced unchanged afterward. `Entry.Requires []string` and the four `Entries` maps are defined in Task 4 and used in Tasks 5–9. `requireRefs` is defined in Task 5 and reused in Tasks 7 and 8. `manifestHash` is defined and used in Task 9.

**Out of scope (later plans):** the provider interface, `plan`/`apply`/`check` behaviour, `fsmerge`, and the orchestrator are Plans 2–6 and are not touched here.
