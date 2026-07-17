# `ainfra install --prune` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `ainfra install --prune`, which removes config not declared in ainfra — but never on first sight: an offered ledger guarantees the user is shown an entry on one run before it can be deleted on a later one.

**Architecture:** `--prune` reuses the existing tombstone delete path. `DiffResources` gains `DiffOpts{Prune}` and synthesizes a `ChangeDelete` for any observed resource in neither `desired` nor `prior`. The orchestrator then partitions those deletes against a per-scope offered ledger (`.ainfra/prune-offered.json`): unseen ones are reported and dropped from the plan; previously-seen ones are armed, backed up via a `Pruner` interface, and applied. Everything downstream of `Change` (plan rendering, `--dry-run`, `--strict`, confirm prompt, `Apply`) is untouched.

**Tech Stack:** Go 1.x, stdlib only. Tests use the existing fake `provider.Filesystem` and `CommandRunner`.

## Global Constraints

- **Spec:** `docs/superpowers/specs/2026-07-17-install-prune-untracked-design.md`. Read it before Task 1; it carries the evidence the design rests on.
- **Pruneable channels are exactly `mcpServers` (repo-scope only), `skills`, `commands`.** Nothing else. `hooks` cannot work (`Hooks.Observe` is ledger-sourced, so `observed` ≡ `prior` and the untracked set is always empty). `rules` is deferred (its `Observe` spans repo and `$HOME`). `tools`, `backgroundServices`, `plugins`, `marketplaces` are excluded by choice.
- **`DiffOpts{}` zero value must preserve today's behavior exactly.** Untracked resources are left alone without `Prune`.
- **`--dry-run` must never write the offered ledger.** A preview that armed deletions would make the next real run delete on what the user experienced as the first run.
- **No `--force-prune` / `--prune-now` escape hatch.** The enforced gap between "shown" and "deleted" is the feature.
- **`--yes` skips the confirm prompt, never the guard.**
- **Never delete what could not be backed up.**
- **No emoji anywhere.** No "Generated with Claude Code" or `Co-Authored-By` in commits. Short, professional commit messages focused on why.
- **Run `go test ./...` before every commit.** It must pass.

---

### Task 1: Prune in the diff

**Files:**
- Modify: `internal/provider/provider.go` (add `Change.Prune`)
- Modify: `internal/provider/diff.go` (add `DiffOpts`, prune branch)
- Modify: `internal/provider/orchestrator.go:107`, `:169` (pass `DiffOpts{}`)
- Test: `internal/provider/diff_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `type DiffOpts struct{ Prune bool }`; `func DiffResources(channel string, desired, observed, prior []Resource, opts DiffOpts) ChannelPlan`; field `Change.Prune bool`.

- [ ] **Step 1: Write the failing tests**

Add to `internal/provider/diff_test.go`:

```go
func TestDiffPruneRemovesUntracked(t *testing.T) {
	desired := []Resource{{ID: "kept", Channel: "skills"}}
	observed := []Resource{{ID: "kept", Channel: "skills"}, {ID: "stray", Channel: "skills"}}
	p := DiffResources("skills", desired, observed, nil, DiffOpts{Prune: true})

	c, ok := find(p, "stray")
	if !ok {
		t.Fatal("stray not in plan")
	}
	if c.Kind != ChangeDelete {
		t.Errorf("Kind = %v, want ChangeDelete", c.Kind)
	}
	if !c.Prune {
		t.Error("Prune = false, want true")
	}
}

func TestDiffWithoutPruneLeavesUntracked(t *testing.T) {
	desired := []Resource{{ID: "kept", Channel: "skills"}}
	observed := []Resource{{ID: "kept", Channel: "skills"}, {ID: "stray", Channel: "skills"}}
	p := DiffResources("skills", desired, observed, nil, DiffOpts{})

	if _, ok := find(p, "stray"); ok {
		t.Error("stray in plan without Prune; untracked resources must be left alone")
	}
}

func TestDiffPruneSkipsTombstonedID(t *testing.T) {
	desired := []Resource{{ID: "gone", Channel: "skills", Tombstone: true}}
	observed := []Resource{{ID: "gone", Channel: "skills"}}
	p := DiffResources("skills", desired, observed, nil, DiffOpts{Prune: true})

	n := 0
	for _, c := range p.Changes {
		if c.ID == "gone" && c.Kind == ChangeDelete {
			n++
		}
	}
	if n != 1 {
		t.Errorf("delete count = %d, want 1 (tombstone must not double up with prune)", n)
	}
}

func TestDiffPruneSkipsPriorID(t *testing.T) {
	observed := []Resource{{ID: "retired", Channel: "skills"}}
	prior := []Resource{{ID: "retired", Channel: "skills"}}
	p := DiffResources("skills", nil, observed, prior, DiffOpts{Prune: true})

	n := 0
	for _, c := range p.Changes {
		if c.ID == "retired" && c.Kind == ChangeDelete {
			n++
		}
	}
	if n != 1 {
		t.Errorf("delete count = %d, want 1 (prior delete must not double up with prune)", n)
	}
	c, _ := find(p, "retired")
	if c.Prune {
		t.Error("Prune = true for a prior-tracked delete; only untracked deletes are prune deletes")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd ~/projects/ainfra/.claude/worktrees/install-prune && go test ./internal/provider/ -run TestDiffPrune -v`
Expected: build failure — `too many arguments in call to DiffResources`, `unknown field Prune`.

- [ ] **Step 3: Add the `Prune` field**

In `internal/provider/provider.go`, in the `Change` struct, after `Detail`:

```go
	// Prune marks a delete synthesized because the resource is untracked —
	// present on the machine but in neither the manifest nor the applied
	// ledger — rather than one the manifest asked for. The orchestrator uses
	// it to find the deletes subject to the offered-ledger guard and to
	// backup. It is a structural flag rather than a Detail string match,
	// which would be fragile.
	Prune bool
```

- [ ] **Step 4: Add `DiffOpts` and the prune branch**

In `internal/provider/diff.go`, above `DiffResources`:

```go
// DiffOpts tunes the diff. The zero value is the historical contract: a
// resource ainfra never recorded as its own is left untouched.
type DiffOpts struct {
	// Prune treats an observed resource that is in neither desired nor prior
	// as an implicit tombstone: an untracked resource becomes a delete. Off
	// by default — untracked does not mean unwanted, it usually means the
	// user never got around to declaring it, so the caller is responsible for
	// guarding these deletes (see the offered ledger) before applying them.
	Prune bool
}
```

Change the signature to `func DiffResources(channel string, desired, observed, prior []Resource, opts DiffOpts) ChannelPlan` and add, immediately before the final `sort.Slice`:

```go
	if opts.Prune {
		for id, got := range o {
			if _, wanted := d[id]; wanted {
				continue
			}
			if _, known := pr[id]; known {
				continue // already a Delete from the prior loop above
			}
			if _, tomb := tombstones[id]; tomb {
				continue // already handled above
			}
			plan.Changes = append(plan.Changes, Change{
				Kind:     ChangeDelete,
				ID:       id,
				Detail:   "not declared in ainfra — will be removed",
				Resource: got,
				Prune:    true,
			})
		}
	}
```

Update the doc comment above `DiffResources` to describe the third policy on the tombstone axis.

- [ ] **Step 5: Update the two production callers**

In `internal/provider/orchestrator.go`, both call sites become:

```go
		plan := DiffResources(p.Channel(), desiredByCh[p.Channel()], observed, priorForCh, DiffOpts{})
```

and

```go
		plan := DiffResources(p.Channel(), desiredForCh, observed, priorForCh, DiffOpts{})
```

- [ ] **Step 6: Update the 9 existing test call sites**

In `internal/provider/diff_test.go`, append `, DiffOpts{}` to every existing `DiffResources(...)` call (lines ~24, 52, 70, 80, 96, 124, 143, 159). Do not change their assertions — they encode the historical contract.

- [ ] **Step 7: Run the tests**

Run: `go test ./internal/provider/ -v`
Expected: PASS, including all pre-existing diff tests.

- [ ] **Step 8: Commit**

```bash
git add internal/provider/diff.go internal/provider/provider.go internal/provider/orchestrator.go internal/provider/diff_test.go
git commit -m "Add DiffOpts.Prune to synthesize deletes for untracked resources

Untracked resources stay untouched by default; the zero value preserves the
historical contract. Prune deletes are flagged so the caller can guard them
before they are applied."
```

---

### Task 2: The offered ledger

**Files:**
- Create: `internal/provider/offered.go`
- Test: `internal/provider/offered_test.go`

**Interfaces:**
- Consumes: `Filesystem` from `internal/provider/env.go`.
- Produces:
  - `type OfferedLedger struct { Version int; Offered map[string]OfferedEntry }`
  - `type OfferedEntry struct { FirstOfferedAt string }`
  - `func OfferedKey(channel, id string) string`
  - `func OfferedPath(root string) string`
  - `func OfferedPathUser() (string, error)`
  - `func ReadOffered(fs Filesystem, path string) (*OfferedLedger, bool, error)` — returns `(ledger, corrupt, err)`
  - `func WriteOffered(fs Filesystem, path string, l *OfferedLedger) error`

- [ ] **Step 1: Write the failing tests**

Create `internal/provider/offered_test.go`:

```go
package provider

import (
	"path/filepath"
	"testing"
)

func TestReadOfferedMissingIsEmpty(t *testing.T) {
	fs := newFakeFS()
	l, corrupt, err := ReadOffered(fs, "/nope/prune-offered.json")
	if err != nil {
		t.Fatalf("err = %v, want nil (a missing ledger is a first run)", err)
	}
	if corrupt {
		t.Error("corrupt = true, want false")
	}
	if len(l.Offered) != 0 {
		t.Errorf("Offered len = %d, want 0", len(l.Offered))
	}
}

func TestReadOfferedCorruptFailsOpen(t *testing.T) {
	fs := newFakeFS()
	path := "/repo/.ainfra/prune-offered.json"
	if err := fs.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	l, corrupt, err := ReadOffered(fs, path)
	if err != nil {
		t.Fatalf("err = %v, want nil; a corrupt ledger must fail open", err)
	}
	if !corrupt {
		t.Error("corrupt = false, want true so the caller can warn")
	}
	if len(l.Offered) != 0 {
		t.Error("a corrupt ledger must read as empty, so everything is re-offered and nothing is deleted")
	}
}

func TestWriteReadOfferedRoundTrip(t *testing.T) {
	fs := newFakeFS()
	path := "/repo/.ainfra/prune-offered.json"
	in := &OfferedLedger{
		Version: 1,
		Offered: map[string]OfferedEntry{
			"commands:ship": {FirstOfferedAt: "2026-07-17T11:04:22Z"},
		},
	}
	if err := WriteOffered(fs, path, in); err != nil {
		t.Fatal(err)
	}
	out, corrupt, err := ReadOffered(fs, path)
	if err != nil || corrupt {
		t.Fatalf("err = %v, corrupt = %v", err, corrupt)
	}
	got, ok := out.Offered["commands:ship"]
	if !ok {
		t.Fatal("commands:ship missing after round trip")
	}
	if got.FirstOfferedAt != "2026-07-17T11:04:22Z" {
		t.Errorf("FirstOfferedAt = %q, want 2026-07-17T11:04:22Z", got.FirstOfferedAt)
	}
}

func TestOfferedKey(t *testing.T) {
	if got := OfferedKey("commands", "ship"); got != "commands:ship" {
		t.Errorf("OfferedKey = %q, want commands:ship", got)
	}
}

func TestOfferedPath(t *testing.T) {
	want := filepath.Join("/repo", ".ainfra", "prune-offered.json")
	if got := OfferedPath("/repo"); got != want {
		t.Errorf("OfferedPath = %q, want %q", got, want)
	}
}
```

If `newFakeFS` does not exist in the package's test helpers, find the existing fake filesystem used by `internal/provider` tests (check `internal/provider/*_test.go` and `internal/mcpclient/fakes.go`) and use that constructor instead; do not write a second fake.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/provider/ -run TestOffered -v` and `go test ./internal/provider/ -run TestReadOffered -v`
Expected: build failure — `undefined: ReadOffered`.

- [ ] **Step 3: Implement the ledger**

Create `internal/provider/offered.go`:

```go
package provider

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/MHilhorst/ainfra/internal/xdg"
)

// offeredLedgerVersion is the on-disk schema version of the offered ledger.
const offeredLedgerVersion = 1

// OfferedEntry records when an untracked resource was first reported to the
// user.
type OfferedEntry struct {
	FirstOfferedAt string `json:"firstOfferedAt"`
}

// OfferedLedger is the per-scope record of untracked resources the user has
// been shown by a previous --prune run.
//
// It is what makes --prune safe. Untracked does not mean unwanted: it usually
// means the user never got around to declaring the thing. So prune reports an
// untracked resource on one run and only removes it on a later one, once the
// user has had the chance to declare it. This ledger is the memory of "you
// were told", and it is deliberately not bypassable by a flag.
type OfferedLedger struct {
	Version int                     `json:"version"`
	Offered map[string]OfferedEntry `json:"offered"`
}

// OfferedKey is the ledger key for one resource: "<channel>:<id>".
func OfferedKey(channel, id string) string { return channel + ":" + id }

// OfferedPath is the repo-scope offered ledger location. It sits in .ainfra/
// beside the applied ledger, which is git-ignored, so it never lands in git.
func OfferedPath(root string) string {
	return filepath.Join(root, ".ainfra", "prune-offered.json")
}

// OfferedPathUser is the user-scope offered ledger location, alongside the
// user-scope applied ledger under $XDG_CONFIG_HOME/ainfra.
func OfferedPathUser() (string, error) {
	dir, err := xdg.ConfigHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "prune-offered.json"), nil
}

// ReadOffered loads the offered ledger. A missing file is a first run, not an
// error: it returns an empty ledger.
//
// An unparseable file also returns an empty ledger, with corrupt=true so the
// caller can warn. This fails open on purpose: an empty ledger means every
// untracked resource is offered afresh and nothing is deleted this run. The
// cost is one extra offer round. Failing closed — treating an unreadable
// ledger as "everything was already offered" — would delete without the user
// ever having been shown the list, which is the one outcome this design
// exists to prevent.
func ReadOffered(fs Filesystem, path string) (*OfferedLedger, bool, error) {
	empty := &OfferedLedger{Version: offeredLedgerVersion, Offered: map[string]OfferedEntry{}}

	raw, err := fs.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return empty, false, nil
		}
		return nil, false, err
	}

	var l OfferedLedger
	if err := json.Unmarshal(raw, &l); err != nil {
		return empty, true, nil
	}
	if l.Offered == nil {
		l.Offered = map[string]OfferedEntry{}
	}
	l.Version = offeredLedgerVersion
	return &l, false, nil
}

// WriteOffered persists the offered ledger, creating its directory if needed.
func WriteOffered(fs Filesystem, path string, l *OfferedLedger) error {
	if err := fs.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	l.Version = offeredLedgerVersion
	if l.Offered == nil {
		l.Offered = map[string]OfferedEntry{}
	}
	raw, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	return fs.WriteFile(path, append(raw, '\n'), 0o644)
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/provider/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/provider/offered.go internal/provider/offered_test.go
git commit -m "Add the offered ledger backing --prune's declare-or-clear guard

Records untracked resources already reported to the user so prune can never
delete something on first sight. A corrupt ledger fails open: everything is
re-offered and nothing is deleted, because the alternative is deleting
without ever having shown the user the list."
```

---

### Task 3: The `Pruner` interface and provider backups

**Files:**
- Modify: `internal/provider/provider.go` (add `Pruner`)
- Create: `internal/provider/claudecode/backup.go` (shared copy helper)
- Modify: `internal/provider/claudecode/skills.go`, `commands.go`, `mcp.go` (add `Backup`)
- Test: `internal/provider/claudecode/backup_test.go`

**Interfaces:**
- Consumes: `Env`, `Resource` from Task 1's package.
- Produces: `type Pruner interface { Backup(env Env, r Resource, dir string) error }`, implemented by `Skills`, `Commands`, `MCP`.

- [ ] **Step 1: Write the failing tests**

Create `internal/provider/claudecode/backup_test.go`:

```go
package claudecode

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/MHilhorst/ainfra/internal/provider"
)

func TestPrunerImplementations(t *testing.T) {
	// The pruneable set is a property of the type system, not a list that can
	// drift. Quietly implementing Backup on an excluded provider must fail
	// the build here rather than silently widen --prune's blast radius.
	cases := []struct {
		p    provider.Provider
		want bool
	}{
		{MCP{}, true},
		{Skills{}, true},
		{Commands{}, true},
		{Hooks{}, false},
		{Rules{}, false},
		{Tools{}, false},
		{Services{}, false},
		{Plugins{}, false},
		{Marketplaces{}, false},
	}
	for _, c := range cases {
		_, ok := c.p.(provider.Pruner)
		if ok != c.want {
			t.Errorf("%s implements Pruner = %v, want %v", c.p.Channel(), ok, c.want)
		}
	}
}

func TestSkillsBackupRoundTrip(t *testing.T) {
	env := newTestEnv(t) // repo-scope env with a fake FS rooted at /repo
	skill := filepath.Join(env.Root, ".claude", "skills", "stray")
	if err := env.FS.MkdirAll(skill, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := env.FS.WriteFile(filepath.Join(skill, "SKILL.md"), []byte("hand written"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := env.FS.WriteFile(filepath.Join(skill, "refs", "x.md"), []byte("nested"), 0o644); err != nil {
		t.Fatal(err)
	}

	dir := "/repo/.ainfra/pruned-x"
	if err := (Skills{}).Backup(env, provider.Resource{ID: "stray", Channel: "skills"}, dir); err != nil {
		t.Fatal(err)
	}

	got, err := env.FS.ReadFile(filepath.Join(dir, "skills", "stray", "SKILL.md"))
	if err != nil || string(got) != "hand written" {
		t.Errorf("SKILL.md = %q, %v; want %q", got, err, "hand written")
	}
	nested, err := env.FS.ReadFile(filepath.Join(dir, "skills", "stray", "refs", "x.md"))
	if err != nil || string(nested) != "nested" {
		t.Errorf("refs/x.md = %q, %v; want %q", nested, err, "nested")
	}
}

func TestCommandsBackupRoundTrip(t *testing.T) {
	env := newTestEnv(t)
	path := filepath.Join(env.Root, ".claude", "commands", "ship.md")
	if err := env.FS.WriteFile(path, []byte("# ship"), 0o644); err != nil {
		t.Fatal(err)
	}

	dir := "/repo/.ainfra/pruned-x"
	if err := (Commands{}).Backup(env, provider.Resource{ID: "ship", Channel: "commands"}, dir); err != nil {
		t.Fatal(err)
	}

	got, err := env.FS.ReadFile(filepath.Join(dir, "commands", "ship.md"))
	if err != nil || string(got) != "# ship" {
		t.Errorf("ship.md = %q, %v; want %q", got, err, "# ship")
	}
}

func TestMCPBackupWritesFragmentNotEmptyFile(t *testing.T) {
	// Observe does not populate Payload, so a backup that serialized
	// Resource.Payload would write "{}" while reporting success. This is the
	// regression test for that trap: Backup must re-read .mcp.json.
	env := newTestEnv(t)
	doc := `{"mcpServers":{"stray":{"command":"npx","args":["stray-mcp"]}}}`
	if err := env.FS.WriteFile(filepath.Join(env.Root, ".mcp.json"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}

	dir := "/repo/.ainfra/pruned-x"
	if err := (MCP{}).Backup(env, provider.Resource{ID: "stray", Channel: "mcpServers"}, dir); err != nil {
		t.Fatal(err)
	}

	raw, err := env.FS.ReadFile(filepath.Join(dir, "mcpServers", "stray.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got["command"] != "npx" {
		t.Errorf("backup = %s; want the server's real entry, not an empty object", raw)
	}
}
```

`newTestEnv(t)` must return a `provider.Env` with `Root: "/repo"`, `Home: "/home"`, and the package's existing fake filesystem. If a helper of another name already exists in `internal/provider/claudecode/*_test.go`, use it and drop this one — do not add a second fake.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/provider/claudecode/ -run 'TestPruner|TestSkillsBackup|TestCommandsBackup|TestMCPBackup' -v`
Expected: build failure — `undefined: provider.Pruner`, `Skills has no field or method Backup`.

- [ ] **Step 3: Add the `Pruner` interface**

In `internal/provider/provider.go`, after the `Provider` interface:

```go
// Pruner is implemented by providers whose channel supports --prune.
//
// A provider that does not implement it can never have untracked resources
// removed: the orchestrator does not pass DiffOpts{Prune} to its diff. This
// makes the pruneable set a property of the type system rather than a list
// that can drift out of sync with intent.
//
// Deliberately not implemented by: hooks (Observe is ledger-sourced, so its
// untracked set is always empty and a prune would be a silent no-op), rules
// (Observe spans the repo and $HOME, so scoping it needs work this design
// defers), tools (would uninstall CLI binaries), backgroundServices (would
// tear down tunnels), plugins and marketplaces (installed artifacts).
type Pruner interface {
	Provider
	// Backup copies the on-disk state of r into dir before r is deleted.
	// Implementations own their storage layout: the orchestrator does not
	// know where a channel keeps its data, and Observe does not populate
	// Payload, so a generic backup would write empty files.
	Backup(env Env, r Resource, dir string) error
}
```

- [ ] **Step 4: Add the shared copy helper**

Create `internal/provider/claudecode/backup.go`:

```go
package claudecode

import (
	"path/filepath"

	"github.com/MHilhorst/ainfra/internal/provider"
)

// copyTree recursively copies src to dst using env.FS. A skill bundle may
// contain nested paths, so a flat copy would silently drop files from the
// backup of a directory that is about to be removed.
func copyTree(env provider.Env, src, dst string) error {
	names, err := env.FS.ReadDir(src)
	if err != nil {
		return err
	}
	if err := env.FS.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	for _, name := range names {
		s := filepath.Join(src, name)
		d := filepath.Join(dst, name)
		info, err := env.FS.Stat(s)
		if err != nil {
			return err
		}
		if info.IsDir() {
			if err := copyTree(env, s, d); err != nil {
				return err
			}
			continue
		}
		if err := copyFile(env, s, d); err != nil {
			return err
		}
	}
	return nil
}

// copyFile copies a single file, creating the destination directory.
func copyFile(env provider.Env, src, dst string) error {
	data, err := env.FS.ReadFile(src)
	if err != nil {
		return err
	}
	if err := env.FS.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return env.FS.WriteFile(dst, data, 0o644)
}
```

- [ ] **Step 5: Implement `Skills.Backup`**

Append to `internal/provider/claudecode/skills.go`:

```go
// Backup copies the skill's directory into dir before Apply removes it. The
// skill may be hours of hand-written work that exists nowhere else, so the
// copy is recursive and a failure must cancel the delete.
func (Skills) Backup(env provider.Env, r provider.Resource, dir string) error {
	return copyTree(env, skillDir(env, r.ID), filepath.Join(dir, "skills", r.ID))
}
```

- [ ] **Step 6: Implement `Commands.Backup`**

Append to `internal/provider/claudecode/commands.go`:

```go
// Backup copies the command's markdown file into dir before Apply removes it.
func (Commands) Backup(env provider.Env, r provider.Resource, dir string) error {
	return copyFile(env, commandPath(env, r.ID), filepath.Join(dir, "commands", r.ID+".md"))
}
```

- [ ] **Step 7: Implement `MCP.Backup`**

Append to `internal/provider/claudecode/mcp.go`:

```go
// Backup writes the server's entry from .mcp.json into dir before Apply
// removes it.
//
// It re-reads the file rather than serializing r.Payload: Observe does not
// populate Payload, so a payload-based backup would write an empty object
// while reporting success — losing exactly the data it claims to protect.
func (MCP) Backup(env provider.Env, r provider.Resource, dir string) error {
	raw, err := env.FS.ReadFile(mcpPath(env))
	if err != nil {
		return err
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return err
	}
	servers, ok := doc["mcpServers"].(map[string]any)
	if !ok {
		return fmt.Errorf("mcpServers: %s has no mcpServers object; refusing to back up %q as empty", mcpPath(env), r.ID)
	}
	entry, ok := servers[r.ID]
	if !ok {
		return fmt.Errorf("mcpServers: %q not found in %s; refusing to back up an empty entry", r.ID, mcpPath(env))
	}
	out, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return err
	}
	dst := filepath.Join(dir, "mcpServers", r.ID+".json")
	if err := env.FS.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return env.FS.WriteFile(dst, append(out, '\n'), 0o644)
}
```

Ensure `mcp.go` imports `fmt` and `path/filepath`; ensure `skills.go` imports `path/filepath`.

- [ ] **Step 8: Run the tests**

Run: `go test ./internal/provider/... -v`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/provider/provider.go internal/provider/claudecode/
git commit -m "Add the Pruner interface and provider-side backups

A channel is pruneable only if its provider implements Pruner, so the
pruneable set cannot drift from intent. Backup is provider-side because
Observe does not populate Payload: an orchestrator-level backup would write
empty files while reporting success."
```

---

### Task 4: Orchestrator guard and backup wiring

**Files:**
- Modify: `internal/provider/orchestrator.go`
- Test: `internal/provider/orchestrator_prune_test.go` (create)

**Interfaces:**
- Consumes: `DiffOpts`, `Change.Prune` (Task 1); `OfferedLedger`, `ReadOffered`, `WriteOffered`, `OfferedKey`, `OfferedPath`, `OfferedPathUser` (Task 2); `Pruner` (Task 3).
- Produces:
  - `func (o *Orchestrator) EnablePrune(now func() time.Time)`
  - `func (o *Orchestrator) NewlyOffered() []Change`
  - `func (o *Orchestrator) OfferedLedgerCorrupt() bool`

- [ ] **Step 1: Write the failing tests**

Create `internal/provider/orchestrator_prune_test.go` with these cases. Use the package's existing fake provider/filesystem helpers.

```go
package provider

import (
	"testing"
	"time"
)

func fixedNow() time.Time { return time.Date(2026, 7, 17, 11, 0, 0, 0, time.UTC) }

// A first --prune must report an untracked resource and delete nothing.
func TestPruneFirstRunOffersAndDoesNotDelete(t *testing.T) { /* see Step 3 for the shape */ }

// A second --prune deletes what the first offered and the user left undeclared.
func TestPruneSecondRunDeletes(t *testing.T) {}

// Declaring an entry between runs must spare it and drop its ledger row.
func TestPruneDeclaredBetweenRunsIsSpared(t *testing.T) {}

// A preview must not arm a deletion.
func TestPruneDryRunWritesNoLedger(t *testing.T) {}

// A corrupt ledger re-offers everything rather than deleting unannounced.
func TestPruneCorruptLedgerReOffers(t *testing.T) {}

// A provider that does not implement Pruner never receives Prune.
func TestPruneSkipsNonPruner(t *testing.T) {}

// MCP is repo-scope only: user-scope MCP.Observe would read $HOME/.mcp.json.
func TestPruneSkipsMCPInUserScope(t *testing.T) {}

// Never delete what could not be backed up.
func TestPruneBackupFailureCancelsDelete(t *testing.T) {}
```

Write each body against the real orchestrator API. Every test must assert on observable outcomes (files present/absent, ledger contents), not on internal call counts.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/provider/ -run TestPrune -v`
Expected: build failure — `o.EnablePrune undefined`.

- [ ] **Step 3: Add prune state to the Orchestrator**

In `internal/provider/orchestrator.go`, extend the struct and constructor:

```go
type Orchestrator struct {
	root      string
	scope     Scope
	env       Env
	providers map[string]Provider

	prune        bool
	now          func() time.Time
	offered      *OfferedLedger
	offeredBad   bool
	newlyOffered []Change
	backupDir    string
}

// EnablePrune turns on --prune for this orchestrator: untracked resources
// become candidate deletes, guarded by the offered ledger. now is injected so
// tests can pin the backup directory name and offer timestamps.
func (o *Orchestrator) EnablePrune(now func() time.Time) {
	o.prune = true
	o.now = now
}

// NewlyOffered returns the untracked resources reported to the user for the
// first time by the last plan. They are deliberately absent from the plan:
// prune never deletes on first sight.
func (o *Orchestrator) NewlyOffered() []Change { return o.newlyOffered }

// OfferedLedgerCorrupt reports whether the offered ledger could not be parsed
// and was treated as empty, so the caller can warn.
func (o *Orchestrator) OfferedLedgerCorrupt() bool { return o.offeredBad }

func (o *Orchestrator) clock() time.Time {
	if o.now != nil {
		return o.now()
	}
	return time.Now()
}

// offeredPath resolves the offered ledger for this orchestrator's scope.
func (o *Orchestrator) offeredPath() (string, error) {
	if o.scope == ScopeUser {
		return OfferedPathUser()
	}
	return OfferedPath(o.root), nil
}

// pruneableInScope reports whether p's untracked resources may be removed.
func (o *Orchestrator) pruneableInScope(p Provider) bool {
	if _, ok := p.(Pruner); !ok {
		return false
	}
	// MCP in user scope would read $HOME/.mcp.json, which is not where Claude
	// Code keeps user MCP servers (~/.claude.json). Pruning there would act on
	// the wrong file, so the channel is repo-scope-only for prune.
	if o.scope == ScopeUser && p.Channel() == "mcpServers" {
		return false
	}
	return true
}
```

Add `"time"` to the imports.

- [ ] **Step 4: Apply the guard inside `PlanAllRendered`**

The guard must live inside `PlanAllRendered`, not in the caller: `ApplyAllRendered` calls `PlanAllRendered` internally, so a caller-side guard would be bypassed on apply and the unoffered deletes would execute.

At the top of `PlanAllRendered`, after reading `prior`:

```go
	o.newlyOffered = nil
	if o.prune && o.offered == nil {
		path, err := o.offeredPath()
		if err != nil {
			return nil, err
		}
		l, corrupt, err := ReadOffered(o.env.FS, path)
		if err != nil {
			return nil, err
		}
		o.offered, o.offeredBad = l, corrupt
	}
```

Replace the `DiffResources` call with:

```go
		opts := DiffOpts{}
		if o.prune && o.pruneableInScope(p) {
			opts.Prune = true
		}
		plan := DiffResources(p.Channel(), desiredForCh, observed, priorForCh, opts)
		if opts.Prune {
			plan = o.guardPrune(plan)
		}
		result[ch] = plan
```

Add:

```go
// guardPrune removes prune deletes the user has not been shown before,
// recording them in newlyOffered instead. This is what makes --prune safe:
// untracked usually means "never got around to declaring it", not "unwanted",
// so an entry must appear in one run's report before a later run can delete
// it.
func (o *Orchestrator) guardPrune(plan ChannelPlan) ChannelPlan {
	out := ChannelPlan{Channel: plan.Channel}
	for _, c := range plan.Changes {
		if !c.Prune {
			out.Changes = append(out.Changes, c)
			continue
		}
		if _, seen := o.offered.Offered[OfferedKey(plan.Channel, c.ID)]; seen {
			out.Changes = append(out.Changes, c) // armed: shown on a previous run
			continue
		}
		o.newlyOffered = append(o.newlyOffered, c)
	}
	return out
}
```

- [ ] **Step 5: Back up before apply, and write the ledger after**

In `ApplyAllRendered`, immediately before `r, applyErr := p.Apply(o.env, runnable)`:

```go
		var backupFailed []ChangeFailure
		if o.prune && !o.env.DryRun {
			runnable, backupFailed = o.backupPrunes(p, runnable)
		}
```

and after `res.Skipped = append(res.Skipped, skipped...)`:

```go
		res.Failed = append(res.Failed, backupFailed...)
```

Add:

```go
// backupPrunes copies each prune delete's on-disk state into the run's backup
// directory, dropping any change whose backup failed. Never delete what could
// not be backed up: the resource may be the user's only copy.
func (o *Orchestrator) backupPrunes(p Provider, plan ChannelPlan) (ChannelPlan, []ChangeFailure) {
	pr, ok := p.(Pruner)
	if !ok {
		return plan, nil
	}
	out := ChannelPlan{Channel: plan.Channel}
	var failed []ChangeFailure
	for _, c := range plan.Changes {
		if !c.Prune {
			out.Changes = append(out.Changes, c)
			continue
		}
		if err := pr.Backup(o.env, c.Resource, o.runBackupDir()); err != nil {
			failed = append(failed, ChangeFailure{Change: c, Err: fmt.Errorf("backup failed, not deleting: %w", err)})
			continue
		}
		out.Changes = append(out.Changes, c)
	}
	return out, failed
}

// runBackupDir is this run's backup directory, computed once so every channel
// shares one timestamped tree.
func (o *Orchestrator) runBackupDir() string {
	if o.backupDir == "" {
		o.backupDir = filepath.Join(o.root, ".ainfra", "pruned-"+o.clock().UTC().Format("20060102T150405Z"))
	}
	return o.backupDir
}
```

Add `"path/filepath"` to the imports.

Then, after the applied-ledger write block, add the offered-ledger write:

```go
	if o.prune && !o.env.DryRun {
		if werr := o.writeOffered(results); werr != nil {
			errs = append(errs, fmt.Errorf("writing offered ledger: %w", werr))
		}
	}
```

Add:

```go
// writeOffered persists the offered ledger after an apply. A row exists only
// for a resource that is still untracked and still on disk: entries the user
// declared drop out because they are no longer untracked, and entries this run
// deleted drop out because they are gone.
func (o *Orchestrator) writeOffered(results []ApplyResult) error {
	deleted := map[string]bool{}
	for _, res := range results {
		for _, c := range res.Applied {
			if c.Prune && c.Kind == ChangeDelete {
				deleted[OfferedKey(res.Channel, c.ID)] = true
			}
		}
	}

	next := &OfferedLedger{Version: offeredLedgerVersion, Offered: map[string]OfferedEntry{}}
	stamp := o.clock().UTC().Format(time.RFC3339)

	keep := func(key string) {
		if deleted[key] {
			return
		}
		if prev, ok := o.offered.Offered[key]; ok {
			next.Offered[key] = prev
			return
		}
		next.Offered[key] = OfferedEntry{FirstOfferedAt: stamp}
	}
	for _, c := range o.newlyOffered {
		keep(OfferedKey(c.Resource.Channel, c.ID))
	}
	for _, res := range results {
		for _, c := range res.Applied {
			if c.Prune && c.Kind == ChangeDelete {
				continue
			}
		}
		for _, f := range res.Failed {
			if f.Change.Prune {
				keep(OfferedKey(res.Channel, f.Change.ID))
			}
		}
	}

	path, err := o.offeredPath()
	if err != nil {
		return err
	}
	return WriteOffered(o.env.FS, path, next)
}
```

Note: `Change.Resource.Channel` is set by `DiffResources` from the observed resource; if it is empty for a channel, key off the plan's channel instead. Verify against Task 1's tests before relying on it.

- [ ] **Step 6: Run the tests**

Run: `go test ./internal/provider/ -v`
Expected: PASS, all prune tests green.

- [ ] **Step 7: Commit**

```bash
git add internal/provider/orchestrator.go internal/provider/orchestrator_prune_test.go
git commit -m "Guard prune deletes behind the offered ledger and back them up

The guard lives inside PlanAllRendered because ApplyAllRendered re-plans
internally: a caller-side guard would be bypassed on apply. A prune delete is
applied only if a previous run reported it and its backup succeeded."
```

---

### Task 5: Wire `--prune` into `install`

**Files:**
- Modify: `cmd/ainfra/commands.go` (flag, user-scope forcing, offer report)
- Test: `cmd/ainfra/cmd_install_prune_test.go` (create)

**Interfaces:**
- Consumes: `EnablePrune`, `NewlyOffered`, `OfferedLedgerCorrupt` (Task 4).
- Produces: the `--prune` flag on `ainfra install`.

- [ ] **Step 1: Write the failing test**

Create `cmd/ainfra/cmd_install_prune_test.go`:

```go
package main

import (
	"bytes"
	"strings"
	"testing"
)

// The acceptance test for the whole feature, using the real numbers from the
// machine that motivated it: 8 undeclared commands. The first run must delete
// nothing.
func TestInstallPruneFirstRunDeletesNothing(t *testing.T) {
	dir := setupRepoWithUndeclaredCommands(t, "dbaccess", "document", "monitor", "review-wip", "ship", "spin", "start", "stop")

	var out bytes.Buffer
	code := run([]string{"--chdir", dir, "install", "--prune", "--yes"}, &out, &bytes.Buffer{})
	if code != 0 {
		t.Fatalf("code = %d, want 0\n%s", code, out.String())
	}
	for _, id := range []string{"ship", "start", "spin", "dbaccess"} {
		assertCommandExists(t, dir, id)
	}
	if !strings.Contains(out.String(), "Not declared in ainfra") {
		t.Errorf("first run must report undeclared entries; got:\n%s", out.String())
	}
}

// After the user declares what they want, a second run removes only the rest.
func TestInstallPruneSecondRunRemovesOnlyUndeclared(t *testing.T) {
	dir := setupRepoWithUndeclaredCommands(t, "ship", "cruft")

	if code := run([]string{"--chdir", dir, "install", "--prune", "--yes"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("first run failed")
	}
	declareCommand(t, dir, "ship")

	if code := run([]string{"--chdir", dir, "install", "--prune", "--yes"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("second run failed")
	}
	assertCommandExists(t, dir, "ship")
	assertCommandAbsent(t, dir, "cruft")
	assertBackupExists(t, dir, "commands", "cruft.md")
}

// A preview must not arm a deletion.
func TestInstallPruneDryRunDoesNotArm(t *testing.T) {
	dir := setupRepoWithUndeclaredCommands(t, "cruft")

	run([]string{"--chdir", dir, "install", "--prune", "--dry-run"}, &bytes.Buffer{}, &bytes.Buffer{})
	run([]string{"--chdir", dir, "install", "--prune", "--yes"}, &bytes.Buffer{}, &bytes.Buffer{})

	assertCommandExists(t, dir, "cruft") // still only offered, never armed by the preview
}

// The headline safety property.
func TestInstallWithoutPruneLeavesUndeclaredAlone(t *testing.T) {
	dir := setupRepoWithUndeclaredCommands(t, "ship")

	if code := run([]string{"--chdir", dir, "install", "--yes"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("install failed")
	}
	assertCommandExists(t, dir, "ship")
}
```

Write the helpers (`setupRepoWithUndeclaredCommands`, `declareCommand`, `assertCommandExists`, `assertCommandAbsent`, `assertBackupExists`) following the patterns in the existing `cmd/ainfra/cmd_install_*_test.go` files. Reuse their scaffolding rather than inventing a new harness.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/ainfra/ -run TestInstallPrune -v`
Expected: FAIL — `flag provided but not defined: -prune`.

- [ ] **Step 3: Add the flag**

In `cmd/ainfra/commands.go`, in `newInstallCommand`, add `prune` to the var block, then:

```go
			fs.BoolVar(&prune, "prune", false, "remove config not declared in ainfra; the first run only reports what it would remove")
```

Update `UsageLine` to include `[--prune]`.

- [ ] **Step 4: Force the user-scope orchestrator under `--prune`**

Change the user-scope condition so prune always constructs it. Without this the feature no-ops for exactly the messiest machines: a user with no personal-layer entries and an empty user ledger gets no user-scope orchestrator at all.

```go
	if herr != nil && prune {
		ui.RenderError(ctx.Stderr, errColor, fmt.Errorf("--prune needs your home directory to reconcile user-scope config: %w", herr))
		return 1
	}
	if herr == nil && (anyResources(userRendered) || userLedgerNonEmpty || prune) {
```

- [ ] **Step 5: Enable prune on both orchestrators**

After each `NewOrchestratorScoped` call, when `prune` is set:

```go
	orch := provider.NewOrchestratorScoped(dir, provider.ScopeRepo, env, providers)
	if prune {
		orch.EnablePrune(time.Now)
	}
```

and the same for `userOrch`. Add `"time"` to the imports.

- [ ] **Step 6: Report what was offered**

After the plans are computed and before the confirm prompt, print the offer report. Collect `orch.NewlyOffered()` and `userOrch.NewlyOffered()`:

```go
// renderOffered prints the undeclared entries prune reported but did not
// remove, and how to keep them. Prune never deletes on first sight, so this
// report is the user's one chance to declare something before a later run
// removes it.
func renderOffered(w io.Writer, c *ui.Colorizer, offered []provider.Change) {
	if len(offered) == 0 {
		return
	}
	fmt.Fprintln(w, c.Yellow("Not declared in ainfra (nothing removed yet):"))
	fmt.Fprintln(w)
	byCh := map[string][]string{}
	for _, ch := range offered {
		byCh[ch.Resource.Channel] = append(byCh[ch.Resource.Channel], ch.ID)
	}
	for _, ch := range sortedKeys(byCh) {
		ids := byCh[ch]
		sort.Strings(ids)
		fmt.Fprintf(w, "  %-10s %s\n", ch, strings.Join(ids, ", "))
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "To keep any of these, declare them:")
	fmt.Fprintln(w, "  ainfra add <channel> <id> --personal")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Anything still undeclared will be removed by the next 'ainfra install --prune'.")
	fmt.Fprintln(w, "Backups are written to .ainfra/pruned-<timestamp>/ when it does.")
}
```

Also warn when `orch.OfferedLedgerCorrupt()` is true: "the offered ledger was unreadable; every undeclared entry has been reported again and nothing was removed."

After a prune run, print what prune cannot clear, so users are not left believing the machine now matches the manifest exactly:

```go
	if prune {
		fmt.Fprintln(ctx.Stdout, c.Dim("--prune does not touch hooks, personal MCP servers in ~/.claude.json, CLI tools, background services, plugins, or marketplaces."))
	}
```

- [ ] **Step 7: Run the tests**

Run: `go test ./... `
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add cmd/ainfra/
git commit -m "Add ainfra install --prune

The first run reports undeclared config and removes nothing; a later run
removes what the user left undeclared, with a backup. --prune forces the
user-scope orchestrator into existence: it is otherwise built only when
personal entries exist, so prune would no-op on exactly the machines with the
most undeclared config."
```

---

### Task 6: Documentation

**Files:**
- Modify: `README.md` (command table)
- Modify: `docs/quickstart.md` (declare-or-clear workflow)
- Modify: `docs/reference/` (the install command's reference page — locate it with `grep -rl "no-install" docs/reference/`)

**Interfaces:**
- Consumes: the `--prune` flag from Task 5.
- Produces: nothing code depends on.

- [ ] **Step 1: Update the README command table**

Find the `install` row in `README.md` and add `--prune` to its flag list, matching the table's existing style.

- [ ] **Step 2: Document the workflow in `docs/quickstart.md`**

Add a section. It must state the honest scope — the feature's main way of misleading is implying it makes a machine match its manifest:

````markdown
## Clearing config you never declared

`ainfra install` only ever adds and updates, so a machine accumulates config no
manifest describes. `--prune` removes it — but never on first sight:

```bash
ainfra install --prune     # reports what is undeclared, removes nothing
ainfra add commands ship --personal   # keep the ones you want
ainfra install --prune     # removes what is still undeclared
```

Undeclared does not mean unwanted: it usually means you never got around to
declaring it. So the first run is always a report, and a second run removes only
what you saw and chose not to keep. Removed files are copied to
`.ainfra/pruned-<timestamp>/` first.

`--prune` covers `mcpServers` (in the repo), `skills`, and `commands`. It does
**not** touch hooks, personal MCP servers in `~/.claude.json`, CLI tools,
background services, plugins, or marketplaces — so it does not make your machine
match your manifest exactly.
````

- [ ] **Step 3: Update the install reference page**

Add `--prune` with the same honest-scope wording.

- [ ] **Step 4: Verify docs match the code**

Run: `scripts/dev/verify-agent-context.sh` if present; otherwise `grep -rn "prune" README.md docs/ | head -20` and read each hit against the implementation.
Expected: every claim true.

- [ ] **Step 5: Commit**

```bash
git add README.md docs/
git commit -m "Document install --prune and its honest scope

Spells out that prune does not touch hooks, personal MCP servers, CLI tools,
or services, so nobody reads it as making a machine match its manifest."
```

---

## Self-Review

**Spec coverage:** `DiffOpts.Prune` (Task 1), offered ledger incl. corrupt-fails-open and dry-run-writes-nothing (Tasks 2, 4, 5), `Pruner` interface and the excluded-channel table (Task 3), provider-side backup and the `Payload`-is-empty trap (Task 3), guard inside `PlanAllRendered` (Task 4), backup-failure cancels delete (Task 4), user-scope orchestrator forcing (Task 5), MCP skipped in user scope (Task 4), UX report and honest-scope notice (Task 5), docs (Task 6).

**Deviations from the spec, deliberate:**
- **`rules` dropped from the pruneable set.** The spec included it and described the repo/user double-observation as "the one place prune needs genuinely new scope logic". Cutting it removes that logic entirely, and the evidence says the untracked rules on the motivating machine are `claude-md` and `agents-md` — CLAUDE.md and AGENTS.md, which nobody wants pruned. High complexity, negative value. The spec's `Rules.Backup` and its scope-filter test are dropped with it.
- **No offered-ledger expiry.** Spec open question 2; answered "no expiry" as the spec's own default.

**Type consistency:** `DiffOpts`/`Change.Prune` (Task 1) are consumed with the same names in Tasks 3–5. `Pruner.Backup(env, r, dir)` matches its three call sites. `OfferedKey`/`OfferedPath`/`OfferedPathUser`/`ReadOffered`/`WriteOffered` (Task 2) match Task 4's uses. `EnablePrune(now func() time.Time)`, `NewlyOffered() []Change`, `OfferedLedgerCorrupt() bool` (Task 4) match Task 5's uses.

**Known loose ends for the implementer to resolve, not skip:**
- Task 4 Step 5's `writeOffered` has a vestigial empty loop over `res.Applied`; delete it and keep only the `res.Failed` pass.
- `Change.Resource.Channel` may be empty for some observed resources; if so, key the ledger off the plan's channel. Verify, do not assume.
- Test helper names (`newFakeFS`, `newTestEnv`) are guesses at the existing harness. Find the real ones first and reuse them.
