# Phase 3 — Plan 2: Provider Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Build the `internal/provider` package — the `Provider` contract, the injected `Env` (filesystem + command runner with fakes), the shared three-way diff, the `fsmerge` helpers, the applied-state ledger, and an orchestrator skeleton — so Plans 3-5 can add concrete providers and Plan 6 can wire the commands.

**Architecture:** A `Provider` reconciles one channel and implements only `Observe` (read machine) and `Apply` (mutate machine). The three-way diff (prior vs desired vs observed) is channel-agnostic and lives in one shared `DiffResources` function. `Env` injects a `Filesystem` and `CommandRunner` so providers are unit-testable against in-memory fakes. The orchestrator loads the locks, the `.ainfra/applied.lock` ledger, rebuilds the dependency graph from per-entry `requires`, and walks providers in topo order.

**Tech Stack:** Go, `gopkg.in/yaml.v3`, standard `testing`.

**Spec deviation:** spec §3.1 put `Diff` on the `Provider` interface. Diff is channel-agnostic (compares `ContentHash` by `ID`), so it is a shared `DiffResources` function instead — tested once, thoroughly. Providers implement `Observe` + `Apply` only. The spec has been updated.

---

### Task 1: Core provider types

**Files:** Create `internal/provider/provider.go`; Test `internal/provider/provider_test.go`

- [ ] **Step 1: Write the failing test** — in `provider_test.go`:

```go
package provider

import "testing"

func TestChannelPlanEmpty(t *testing.T) {
	empty := ChannelPlan{Channel: "skills", Changes: []Change{{Kind: ChangeNoop, ID: "a"}}}
	if !empty.Empty() {
		t.Error("a plan of only noop changes must be Empty")
	}
	busy := ChannelPlan{Channel: "skills", Changes: []Change{{Kind: ChangeCreate, ID: "a"}}}
	if busy.Empty() {
		t.Error("a plan with a create must not be Empty")
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/provider/ -run ChannelPlanEmpty -v` — FAIL (package does not compile).

- [ ] **Step 3: Implement** `internal/provider/provider.go`:

```go
// Package provider defines the channel reconciliation contract and the shared
// machinery (diff, environment, orchestration) every channel provider uses.
package provider

// ChangeKind classifies a single planned mutation.
type ChangeKind int

const (
	ChangeNoop ChangeKind = iota
	ChangeCreate
	ChangeUpdate
	ChangeDelete
)

// String renders a ChangeKind as the one-character plan symbol.
func (k ChangeKind) String() string {
	switch k {
	case ChangeCreate:
		return "+"
	case ChangeUpdate:
		return "~"
	case ChangeDelete:
		return "-"
	default:
		return " "
	}
}

// Resource is one channel entry in a provider-neutral shape. Desired resources
// come from the lockfile; observed resources are built by a provider's Observe.
type Resource struct {
	ID          string
	Channel     string
	Layer       string
	ContentHash string
	Requires    []string
	Payload     map[string]any
}

// Change is one planned mutation of a single resource.
type Change struct {
	Kind   ChangeKind
	ID     string
	Detail string
}

// ChannelPlan is the set of changes one provider would make.
type ChannelPlan struct {
	Channel string
	Changes []Change
}

// Empty reports whether the plan would change nothing.
func (p ChannelPlan) Empty() bool {
	for _, c := range p.Changes {
		if c.Kind != ChangeNoop {
			return false
		}
	}
	return true
}

// ApplyResult records what a provider's Apply actually did.
type ApplyResult struct {
	Channel string
	Applied []Change
}

// Provider reconciles one channel. Observe reads machine state; Apply mutates
// it. The diff between desired and observed is channel-agnostic and is computed
// by the shared DiffResources function, not by the provider.
type Provider interface {
	Channel() string
	Observe(env Env) ([]Resource, error)
	Apply(env Env, plan ChannelPlan) (ApplyResult, error)
}
```

- [ ] **Step 4: Run** the test — PASS.
- [ ] **Step 5: Commit** `Add core provider types`.

---

### Task 2: The shared three-way diff

**Files:** Create `internal/provider/diff.go`; Test `internal/provider/diff_test.go`

- [ ] **Step 1: Write failing tests** — in `diff_test.go`, cover: a desired resource absent from observed → Create; present but ContentHash differs → Update; present and equal → Noop; in prior but not desired → Delete; observed-only and not in prior → ignored (no change).

```go
package provider

import "testing"

func res(id, hash string) Resource { return Resource{ID: id, ContentHash: hash} }

func find(p ChannelPlan, id string) (Change, bool) {
	for _, c := range p.Changes {
		if c.ID == id {
			return c, true
		}
	}
	return Change{}, false
}

func TestDiffResources(t *testing.T) {
	desired := []Resource{res("keep", "h1"), res("changed", "h2new"), res("new", "h3")}
	observed := []Resource{res("keep", "h1"), res("changed", "h2old"), res("foreign", "hX")}
	prior := []Resource{res("keep", "h1"), res("changed", "h2old"), res("gone", "h4")}

	p := DiffResources("skills", desired, observed, prior)

	want := map[string]ChangeKind{
		"keep": ChangeNoop, "changed": ChangeUpdate, "new": ChangeCreate, "gone": ChangeDelete,
	}
	for id, kind := range want {
		c, ok := find(p, id)
		if !ok {
			t.Errorf("%s: no change emitted", id)
			continue
		}
		if c.Kind != kind {
			t.Errorf("%s: kind = %v, want %v", id, c.Kind, kind)
		}
	}
	if _, ok := find(p, "foreign"); ok {
		t.Error("a resource owned by neither prior nor desired must be left alone")
	}
}
```

- [ ] **Step 2: Run** — FAIL.
- [ ] **Step 3: Implement** `internal/provider/diff.go`:

```go
package provider

import "sort"

// DiffResources computes the channel-agnostic three-way diff: desired (from the
// lockfile), observed (from the machine), prior (from the applied-state ledger).
// A resource in prior but no longer desired is a Delete; a desired resource
// missing from or differing on the machine is a Create or Update; a resource
// the tool never recorded as its own (in neither prior nor desired) is left
// untouched. Changes are returned sorted by ID for deterministic plan output.
func DiffResources(channel string, desired, observed, prior []Resource) ChannelPlan {
	byID := func(rs []Resource) map[string]Resource {
		m := map[string]Resource{}
		for _, r := range rs {
			m[r.ID] = r
		}
		return m
	}
	d, o, pr := byID(desired), byID(observed), byID(prior)

	plan := ChannelPlan{Channel: channel}
	for id := range pr {
		if _, stillWanted := d[id]; !stillWanted {
			plan.Changes = append(plan.Changes, Change{
				Kind: ChangeDelete, ID: id,
				Detail: channel + " " + id + " removed from manifest",
			})
		}
	}
	for id, want := range d {
		got, onMachine := o[id]
		switch {
		case !onMachine:
			plan.Changes = append(plan.Changes, Change{
				Kind: ChangeCreate, ID: id, Detail: channel + " " + id + " not present",
			})
		case got.ContentHash != want.ContentHash:
			plan.Changes = append(plan.Changes, Change{
				Kind: ChangeUpdate, ID: id, Detail: channel + " " + id + " differs from lockfile",
			})
		default:
			plan.Changes = append(plan.Changes, Change{
				Kind: ChangeNoop, ID: id, Detail: channel + " " + id + " up to date",
			})
		}
	}
	sort.Slice(plan.Changes, func(i, j int) bool { return plan.Changes[i].ID < plan.Changes[j].ID })
	return plan
}
```

- [ ] **Step 4: Run** — PASS.
- [ ] **Step 5: Commit** `Add the shared three-way resource diff`.

---

### Task 3: Env, Filesystem, CommandRunner and their real implementations

**Files:** Create `internal/provider/env.go`; Test `internal/provider/env_test.go`

- [ ] **Step 1: Write the failing test** — verify `OSFilesystem` round-trips a file in a temp dir and `MkdirAll` works:

```go
package provider

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOSFilesystemRoundTrip(t *testing.T) {
	fs := OSFilesystem{}
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "f.txt")
	if err := fs.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := fs.WriteFile(path, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := fs.ReadFile(path)
	if err != nil || string(got) != "hi" {
		t.Fatalf("read = %q, %v", got, err)
	}
	if _, err := fs.Stat(path); err != nil {
		t.Fatalf("stat: %v", err)
	}
	if err := fs.Remove(path); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := fs.Stat(path); !os.IsNotExist(err) {
		t.Errorf("stat after remove = %v, want not-exist", err)
	}
}
```

- [ ] **Step 2: Run** — FAIL.
- [ ] **Step 3: Implement** `internal/provider/env.go`:

```go
package provider

import (
	"os"
	"os/exec"
)

// Filesystem is the file I/O surface a provider may use. Production code uses
// OSFilesystem; tests use MemFilesystem so Observe/Apply are testable without
// touching the real disk.
type Filesystem interface {
	ReadFile(path string) ([]byte, error)
	WriteFile(path string, data []byte, perm os.FileMode) error
	Remove(path string) error
	Stat(path string) (os.FileInfo, error)
	MkdirAll(path string, perm os.FileMode) error
}

// CommandRunner runs an external command and returns its combined output.
type CommandRunner interface {
	Run(name string, args ...string) ([]byte, error)
}

// Env is the injected environment a provider observes and applies against.
type Env struct {
	FS     Filesystem
	Runner CommandRunner
	Home   string // Claude Code config root (e.g. the user's home directory)
	Root   string // the repo root the manifest was resolved from
	DryRun bool
}

// OSFilesystem is the real-disk Filesystem.
type OSFilesystem struct{}

func (OSFilesystem) ReadFile(p string) ([]byte, error)            { return os.ReadFile(p) }
func (OSFilesystem) WriteFile(p string, d []byte, m os.FileMode) error { return os.WriteFile(p, d, m) }
func (OSFilesystem) Remove(p string) error                        { return os.Remove(p) }
func (OSFilesystem) Stat(p string) (os.FileInfo, error)           { return os.Stat(p) }
func (OSFilesystem) MkdirAll(p string, m os.FileMode) error       { return os.MkdirAll(p, m) }

// ExecRunner is the real CommandRunner.
type ExecRunner struct{}

// Run executes name with args and returns combined stdout+stderr.
func (ExecRunner) Run(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).CombinedOutput()
}
```

- [ ] **Step 4: Run** — PASS.
- [ ] **Step 5: Commit** `Add provider Env with OS filesystem and command runner`.

---

### Task 4: In-memory test fakes

**Files:** Create `internal/provider/fakes.go`; Test `internal/provider/fakes_test.go`

- [ ] **Step 1: Write failing tests** — `MemFilesystem` round-trips a write/read/remove and reports `os.IsNotExist` for an absent file; `FakeRunner` returns a scripted result and records the call.

```go
package provider

import (
	"os"
	"testing"
)

func TestMemFilesystem(t *testing.T) {
	fs := NewMemFilesystem()
	if _, err := fs.Stat("/x"); !os.IsNotExist(err) {
		t.Errorf("absent stat = %v, want not-exist", err)
	}
	if err := fs.MkdirAll("/a/b", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := fs.WriteFile("/a/b/f", []byte("v"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := fs.ReadFile("/a/b/f")
	if err != nil || string(got) != "v" {
		t.Fatalf("read = %q %v", got, err)
	}
	if err := fs.Remove("/a/b/f"); err != nil {
		t.Fatal(err)
	}
	if _, err := fs.ReadFile("/a/b/f"); !os.IsNotExist(err) {
		t.Errorf("read after remove = %v, want not-exist", err)
	}
}

func TestFakeRunner(t *testing.T) {
	r := NewFakeRunner()
	r.Script["brew --version"] = FakeResult{Output: []byte("Homebrew 4.0")}
	out, err := r.Run("brew", "--version")
	if err != nil || string(out) != "Homebrew 4.0" {
		t.Fatalf("run = %q %v", out, err)
	}
	if len(r.Calls) != 1 || r.Calls[0] != "brew --version" {
		t.Errorf("calls = %v", r.Calls)
	}
	if _, err := r.Run("unknown"); err == nil {
		t.Error("an unscripted command must error")
	}
}
```

- [ ] **Step 2: Run** — FAIL.
- [ ] **Step 3: Implement** `internal/provider/fakes.go`:

```go
package provider

import (
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"
	"time"
)

// MemFilesystem is an in-memory Filesystem for tests.
type MemFilesystem struct {
	Files map[string][]byte
	Dirs  map[string]bool
}

// NewMemFilesystem returns an empty in-memory filesystem.
func NewMemFilesystem() *MemFilesystem {
	return &MemFilesystem{Files: map[string][]byte{}, Dirs: map[string]bool{}}
}

func (m *MemFilesystem) ReadFile(p string) ([]byte, error) {
	d, ok := m.Files[p]
	if !ok {
		return nil, &os.PathError{Op: "open", Path: p, Err: os.ErrNotExist}
	}
	return append([]byte(nil), d...), nil
}

func (m *MemFilesystem) WriteFile(p string, d []byte, _ os.FileMode) error {
	m.Files[p] = append([]byte(nil), d...)
	return nil
}

func (m *MemFilesystem) Remove(p string) error {
	if _, ok := m.Files[p]; !ok {
		return &os.PathError{Op: "remove", Path: p, Err: os.ErrNotExist}
	}
	delete(m.Files, p)
	return nil
}

func (m *MemFilesystem) MkdirAll(p string, _ os.FileMode) error {
	m.Dirs[p] = true
	return nil
}

// memFileInfo is the minimal os.FileInfo MemFilesystem.Stat returns.
type memFileInfo struct {
	name string
	size int64
	dir  bool
}

func (i memFileInfo) Name() string       { return i.name }
func (i memFileInfo) Size() int64        { return i.size }
func (i memFileInfo) Mode() os.FileMode  { return 0o644 }
func (i memFileInfo) ModTime() time.Time { return time.Time{} }
func (i memFileInfo) IsDir() bool        { return i.dir }
func (i memFileInfo) Sys() any           { return nil }

func (m *MemFilesystem) Stat(p string) (os.FileInfo, error) {
	if d, ok := m.Files[p]; ok {
		return memFileInfo{name: p, size: int64(len(d))}, nil
	}
	if m.Dirs[p] {
		return memFileInfo{name: p, dir: true}, nil
	}
	return nil, &os.PathError{Op: "stat", Path: p, Err: os.ErrNotExist}
}

var _ fs.FileInfo = memFileInfo{}

// FakeResult is one scripted command outcome.
type FakeResult struct {
	Output []byte
	Err    error
}

// FakeRunner is a scripted CommandRunner that records every call.
type FakeRunner struct {
	Script map[string]FakeResult
	Calls  []string
}

// NewFakeRunner returns an empty scripted runner.
func NewFakeRunner() *FakeRunner {
	return &FakeRunner{Script: map[string]FakeResult{}}
}

// Run records the call and returns the scripted result, or errors if the
// command was not scripted.
func (r *FakeRunner) Run(name string, args ...string) ([]byte, error) {
	key := strings.TrimSpace(name + " " + strings.Join(args, " "))
	r.Calls = append(r.Calls, key)
	res, ok := r.Script[key]
	if !ok {
		return nil, fmt.Errorf("fake runner: unscripted command %q", key)
	}
	return res.Output, res.Err
}

// SortedCalls returns the recorded calls sorted, for stable assertions.
func (r *FakeRunner) SortedCalls() []string {
	out := append([]string(nil), r.Calls...)
	sort.Strings(out)
	return out
}
```

- [ ] **Step 4: Run** — PASS.
- [ ] **Step 5: Commit** `Add in-memory filesystem and command-runner fakes`.

---

### Task 5: fsmerge — managed-region file helpers

**Files:** Create `internal/provider/fsmerge/fsmerge.go`; Test `internal/provider/fsmerge/fsmerge_test.go`

`fsmerge` operates on a `provider.Filesystem`. To avoid an import cycle, it takes the filesystem as an interface parameter declared in its own package.

- [ ] **Step 1: Write failing tests** covering: `MergeJSONKeys` adds owned keys, replaces an owned key, deletes an owned-but-no-longer-desired key, and leaves a foreign key untouched; `EnsureImportLine` adds the line once and is idempotent on a second call.

```go
package fsmerge

import "testing"

func TestMergeJSONKeysPreservesForeignKeys(t *testing.T) {
	fs := newMemFS()
	fs.files["/c.json"] = []byte(`{"mcpServers":{"foreign":{"x":1},"old":{"y":2}}}`)

	err := MergeJSONKeys(fs, "/c.json", "mcpServers",
		map[string]any{"new": map[string]any{"z": 3}},
		[]string{"old", "new"}) // owned keys: old (now gone), new (desired)
	if err != nil {
		t.Fatal(err)
	}
	out := string(fs.files["/c.json"])
	for _, want := range []string{`"foreign"`, `"new"`} {
		if !contains(out, want) {
			t.Errorf("result missing %s: %s", want, out)
		}
	}
	if contains(out, `"old"`) {
		t.Errorf("owned-but-undesired key not removed: %s", out)
	}
}

func TestEnsureImportLineIdempotent(t *testing.T) {
	fs := newMemFS()
	if err := EnsureImportLine(fs, "/CLAUDE.md", ".claude/ainfra/context.md"); err != nil {
		t.Fatal(err)
	}
	if err := EnsureImportLine(fs, "/CLAUDE.md", ".claude/ainfra/context.md"); err != nil {
		t.Fatal(err)
	}
	out := string(fs.files["/CLAUDE.md"])
	if n := countLines(out, "@.claude/ainfra/context.md"); n != 1 {
		t.Errorf("import line appears %d times, want 1: %q", n, out)
	}
}
```

Also write a tiny in-test `memFS` (or reuse a minimal struct) plus `contains`/`countLines` helpers in the test file — keep them local to the test.

- [ ] **Step 2: Run** — FAIL.
- [ ] **Step 3: Implement** `internal/provider/fsmerge/fsmerge.go`. Define a local `FS` interface (`ReadFile`, `WriteFile`, `MkdirAll`) so `provider.OSFilesystem` and the fakes satisfy it structurally. `MergeJSONKeys(fs, path, topKey, desired map[string]any, ownedKeys []string)`: read+`json.Unmarshal` the file (treat a missing file as `{}`), ensure the `topKey` object exists, delete every `ownedKey` from it, then set every entry of `desired`, then `json.MarshalIndent` and write. `WriteOwnedFile(fs, path, content)`: `MkdirAll` the parent then `WriteFile`. `EnsureImportLine(fs, claudeMdPath, importPath)`: read the file (missing → empty), if it does not already contain a line equal to `@<importPath>` append `\n@<importPath>\n`, write back. Use `path/filepath` for the parent dir, `encoding/json`, `strings`.

- [ ] **Step 4: Run** — PASS.
- [ ] **Step 5: Commit** `Add fsmerge managed-region file helpers`.

---

### Task 6: The applied-state ledger

**Files:** Create `internal/provider/applied.go`; Test `internal/provider/applied_test.go`

The ledger is the `lockfile.Lock` ainfra last applied on this machine, stored at `<root>/.ainfra/applied.lock`. It reuses the `lockfile` package for read/write.

- [ ] **Step 1: Write the failing test** — `ReadApplied` on a missing file returns an empty lock with non-nil entry maps and no error; `WriteApplied` then `ReadApplied` round-trips an entry.

```go
package provider

import (
	"path/filepath"
	"testing"

	"github.com/MHilhorst/ainfra/internal/lockfile"
)

func TestAppliedLedgerRoundTrip(t *testing.T) {
	root := t.TempDir()
	got, err := ReadApplied(root)
	if err != nil {
		t.Fatalf("ReadApplied on missing: %v", err)
	}
	if got.Entries.Skills == nil {
		t.Error("missing-file ledger must have non-nil entry maps")
	}
	l := &lockfile.Lock{Version: 1, Entries: lockfile.Entries{
		Skills: map[string]lockfile.Entry{"s": {Layer: "repo", ContentHash: "sha256:x"}},
	}}
	if err := WriteApplied(root, l); err != nil {
		t.Fatalf("WriteApplied: %v", err)
	}
	back, err := ReadApplied(root)
	if err != nil {
		t.Fatal(err)
	}
	if back.Entries.Skills["s"].ContentHash != "sha256:x" {
		t.Errorf("round-trip lost the entry: %+v", back.Entries.Skills)
	}
}
```

- [ ] **Step 2: Run** — FAIL.
- [ ] **Step 3: Implement** `internal/provider/applied.go`:

```go
package provider

import (
	"path/filepath"

	"github.com/MHilhorst/ainfra/internal/lockfile"
)

// appliedPath is the per-machine applied-state ledger location under a repo
// root. .ainfra/ is git-ignored, so the ledger is never committed.
func appliedPath(root string) string {
	return filepath.Join(root, ".ainfra", "applied.lock")
}

// ReadApplied loads the applied-state ledger — the lock ainfra last applied on
// this machine. A missing ledger is not an error: it returns an empty lock, so
// a first-ever apply treats every desired entry as a create.
func ReadApplied(root string) (*lockfile.Lock, error) {
	return lockfile.Read(appliedPath(root))
}

// WriteApplied snapshots l as the applied-state ledger after a successful apply.
func WriteApplied(root string, l *lockfile.Lock) error {
	dir := filepath.Join(root, ".ainfra")
	if err := ensureDir(dir); err != nil {
		return err
	}
	return lockfile.Write(appliedPath(root), l)
}
```

Add an `ensureDir(dir string) error` helper in the same file using `os.MkdirAll(dir, 0o755)` (import `os`). `lockfile.Read` already returns an empty lock with non-nil maps for a missing file.

- [ ] **Step 4: Run** — PASS.
- [ ] **Step 5: Commit** `Add the applied-state ledger`.

---

### Task 7: Lockfile-to-Resource conversion and the dependency-order walk

**Files:** Create `internal/provider/resources.go`; Test `internal/provider/resources_test.go`

The orchestrator needs to turn a `lockfile.Lock` into `[]Resource` per channel, and to compute the order channels' entries should be processed from per-entry `requires`.

- [ ] **Step 1: Write failing tests** — `ResourcesByChannel` extracts a lock's entries into per-channel `[]Resource` slices carrying `ID`/`Layer`/`ContentHash`/`Requires`; `ApplyOrder` topologically sorts entry node-refs so a `requires` target precedes the entry that needs it.

```go
package provider

import (
	"testing"

	"github.com/MHilhorst/ainfra/internal/lockfile"
)

func TestResourcesByChannel(t *testing.T) {
	l := &lockfile.Lock{Version: 1, Entries: lockfile.Entries{
		Skills: map[string]lockfile.Entry{"s": {Layer: "repo", ContentHash: "h", Requires: []string{"cli:node"}}},
		Hooks:  map[string]lockfile.Entry{"h": {Layer: "team", ContentHash: "hh"}},
	}}
	got := ResourcesByChannel(l)
	if len(got["skills"]) != 1 || got["skills"][0].ID != "s" || got["skills"][0].ContentHash != "h" {
		t.Errorf("skills = %+v", got["skills"])
	}
	if got["skills"][0].Requires[0] != "cli:node" {
		t.Errorf("requires not carried: %+v", got["skills"][0])
	}
	if len(got["hooks"]) != 1 {
		t.Errorf("hooks = %+v", got["hooks"])
	}
}

func TestApplyOrderRespectsRequires(t *testing.T) {
	l := &lockfile.Lock{Version: 1, Entries: lockfile.Entries{
		MCPServers: map[string]lockfile.Entry{
			"db": {Layer: "repo", ContentHash: "h", Requires: []string{"svc:tunnel"}},
		},
		BackgroundServices: map[string]lockfile.Entry{
			"tunnel": {Layer: "repo", ContentHash: "h2"},
		},
	}}
	order, err := ApplyOrder(l)
	if err != nil {
		t.Fatal(err)
	}
	pos := map[string]int{}
	for i, n := range order {
		pos[n] = i
	}
	if pos["svc:tunnel"] > pos["mcp:db"] {
		t.Errorf("svc:tunnel must come before mcp:db; order = %v", order)
	}
}
```

- [ ] **Step 2: Run** — FAIL.
- [ ] **Step 3: Implement** `internal/provider/resources.go`. `ResourcesByChannel(l *lockfile.Lock) map[string][]Resource` iterates every entry map on `l.Entries` and builds `Resource`s keyed by the channel name (`"mcpServers"`, `"backgroundServices"`, `"hooks"`, `"commands"`, `"cliTools"`, `"skills"`, `"plugins"`, `"rules"`, `"tools"`). `ApplyOrder(l *lockfile.Lock) ([]string, error)` builds a `graph.Graph` (`internal/graph`): for each entry add a node `"<prefix>:<id>"` (prefix per channel: `mcp`, `svc`, `hook`, `cmd`, `cli`, `skill`, `plugin`, `rule`, `tools`) and, for each ref in the entry's `Requires`, `AddNode(ref)` + `AddEdge(node, ref)`; return `g.TopoSort()`. Read `internal/graph/graph.go` to confirm `TopoSort` orders dependencies before dependents — if it returns dependents-first, reverse the result so a required node precedes its consumer (the test pins this).

- [ ] **Step 4: Run** — PASS.
- [ ] **Step 5: Commit** `Add lockfile-to-resource conversion and apply ordering`.

---

### Task 8: The orchestrator skeleton

**Files:** Create `internal/provider/orchestrator.go`; Test `internal/provider/orchestrator_test.go`

- [ ] **Step 1: Write failing tests** — register two fake providers; `Orchestrator.PlanAll` returns a per-channel plan computed from desired (lock) vs observed (fake) vs prior (ledger); `ApplyAll` invokes each provider's `Apply` and, on success, writes the applied ledger.

```go
package provider

import (
	"path/filepath"
	"testing"

	"github.com/MHilhorst/ainfra/internal/lockfile"
)

// stubProvider observes a fixed resource set and records applies.
type stubProvider struct {
	channel  string
	observed []Resource
	applied  []ChannelPlan
}

func (s *stubProvider) Channel() string                  { return s.channel }
func (s *stubProvider) Observe(Env) ([]Resource, error)  { return s.observed, nil }
func (s *stubProvider) Apply(_ Env, p ChannelPlan) (ApplyResult, error) {
	s.applied = append(s.applied, p)
	return ApplyResult{Channel: s.channel, Applied: p.Changes}, nil
}

func newTestLock() *lockfile.Lock {
	return &lockfile.Lock{Version: 1, Entries: lockfile.Entries{
		Skills: map[string]lockfile.Entry{"s": {Layer: "repo", ContentHash: "h1"}},
	}}
}

func TestOrchestratorPlanAndApply(t *testing.T) {
	root := t.TempDir()
	skills := &stubProvider{channel: "skills"} // observes nothing -> "s" is a create
	o := NewOrchestrator(root, Env{}, []Provider{skills})

	plan, err := o.PlanAll(newTestLock())
	if err != nil {
		t.Fatal(err)
	}
	if plan["skills"].Empty() {
		t.Fatalf("expected a create for skill s, got %+v", plan["skills"])
	}

	if err := o.ApplyAll(newTestLock()); err != nil {
		t.Fatalf("ApplyAll: %v", err)
	}
	if len(skills.applied) != 1 {
		t.Errorf("provider Apply not called: %+v", skills.applied)
	}
	if _, err := lockfile.Read(filepath.Join(root, ".ainfra", "applied.lock")); err != nil {
		t.Errorf("applied ledger not written: %v", err)
	}
}
```

- [ ] **Step 2: Run** — FAIL.
- [ ] **Step 3: Implement** `internal/provider/orchestrator.go`:

`Orchestrator` holds `root string`, `env Env`, and `providers map[string]Provider` (keyed by `Channel()`). `NewOrchestrator(root string, env Env, ps []Provider) *Orchestrator` builds the map.

`PlanAll(desired *lockfile.Lock) (map[string]ChannelPlan, error)`:
- `prior, err := ReadApplied(o.root)`
- `desiredByCh := ResourcesByChannel(desired)`, `priorByCh := ResourcesByChannel(prior)`
- for each registered provider: `observed, err := p.Observe(o.env)`; `plan := DiffResources(p.Channel(), desiredByCh[p.Channel()], observed, priorByCh[p.Channel()])`; store in the result map.
- return the map.

`ApplyAll(desired *lockfile.Lock) error`:
- `plans, err := PlanAll(desired)`
- for each provider with a non-`Empty()` plan: `_, err := p.Apply(o.env, plan)`; on error return it immediately (no ledger write — partial apply leaves the ledger at the last consistent state, per spec §7).
- on full success: `WriteApplied(o.root, desired)`.

Keep provider iteration deterministic (sort channel names).

- [ ] **Step 4: Run** — PASS.
- [ ] **Step 5: Commit** `Add the provider orchestrator skeleton`.

---

### Task 9: Full-suite verification

- [ ] **Step 1:** `go build ./... && go test ./...` — all green.
- [ ] **Step 2:** `go vet ./internal/provider/...` — no findings.
- [ ] **Step 3:** Confirm no import cycle: `internal/provider` may import `internal/lockfile` and `internal/graph`; `internal/provider/fsmerge` imports neither (standalone). If a cycle exists, report BLOCKED.
- [ ] **Step 4:** Commit only if Steps 1-3 produced source changes (e.g. a vet fix); otherwise nothing to commit.

---

## Self-Review

**Spec coverage (spec §3, §5, §6 foundation):** Provider interface — Task 1. Shared diff — Task 2 (replaces per-provider `Diff`, see header). `Env`/`Filesystem`/`CommandRunner` + reals — Task 3; fakes — Task 4. `fsmerge` — Task 5. Applied-state ledger (`.ainfra/applied.lock`) — Task 6. Resource conversion + dependency-order walk — Task 7. Orchestrator (`PlanAll`/`ApplyAll`) — Task 8.

**Type consistency:** `Resource`, `Change`, `ChangeKind`, `ChannelPlan`, `ApplyResult`, `Provider`, `Env`, `Filesystem`, `CommandRunner` defined in Tasks 1/3, used unchanged after. `DiffResources`, `ResourcesByChannel`, `ApplyOrder`, `ReadApplied`/`WriteApplied`, `NewOrchestrator` defined once each.

**Out of scope (later plans):** concrete providers (Plans 3-5), `plan`/`apply`/`check` command wiring + plan rendering (Plan 6).
