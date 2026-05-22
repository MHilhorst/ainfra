# Apply Failure Isolation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `ainfra apply` isolate failures — one failing channel or CLI tool no longer aborts the whole apply — and write the applied ledger for everything that succeeded.

**Architecture:** `Orchestrator.ApplyAllRendered` stops returning on the first error. It applies every channel, accumulates per-channel `ApplyResult`s, skips resources whose `requires:` dependency failed earlier in the same run, and writes a *partial* applied ledger (succeeded resources get their desired entry; failed/skipped resources fall back to their prior entry). The `cliTools` provider gains per-entry isolation so one bad `brew install` does not abort its sibling installs. The apply command prints an `applied N, skipped M, failed K` summary.

**Tech Stack:** Go 1.25, standard library only. Tests use the `run()` CLI harness in `cmd/ainfra` and the orchestrator/`FakeRunner` fakes in `internal/provider`.

**Context:** This is item 1 of `docs/superpowers/specs/2026-05-22-apply-hardening-design.md`, scoped (per the spec's sequencing) to **orchestrator-level isolation + the `cliTools` provider**. The other eight channel providers keep returning on their first error; the orchestrator treats that as a whole-channel failure and continues to the next channel. Per-entry isolation for those eight providers is a noted follow-up.

**Prerequisite:** The item-5 plan (`2026-05-22-apply-dry-run-and-sandbox.md`) is already merged — `Env.DryRun`/`Env.NoInstall` and the dry-run ledger skip exist. Task 3 below modifies the `ApplyAllRendered` those commits produced.

---

### Task 1: Add failure/skip types and the `splitBlocked` helper

`ApplyResult` today carries only `Applied []Change`. Add `Failed` and `Skipped` so a provider (or the orchestrator) can report per-resource outcomes. Add `splitBlocked`, the pure function that partitions a channel plan into changes that may run and changes blocked by an already-failed dependency.

**Files:**
- Modify: `internal/provider/provider.go:68-72` (`ApplyResult`)
- Modify: `internal/provider/orchestrator.go` (add `splitBlocked`, `nodeRef`)
- Test: `internal/provider/orchestrator_test.go`

- [ ] **Step 1: Add the types**

In `internal/provider/provider.go`, replace the `ApplyResult` block (lines 68-72):

```go
// ChangeFailure is one Change whose apply was attempted and did not succeed.
type ChangeFailure struct {
	Change Change
	Err    error
}

// ChangeSkip is one Change deliberately not attempted because a resource it
// requires failed earlier in the same apply run.
type ChangeSkip struct {
	Change Change
	Reason string
}

// ApplyResult records what a provider's Apply actually did. Applied holds the
// changes that succeeded; Failed holds changes attempted that errored; Skipped
// holds changes the orchestrator blocked before the provider saw them.
type ApplyResult struct {
	Channel string
	Applied []Change
	Failed  []ChangeFailure
	Skipped []ChangeSkip
}
```

This is additive — every existing provider sets only `Channel` and `Applied`, so they keep compiling.

- [ ] **Step 2: Write the failing test for `splitBlocked`**

Append to `internal/provider/orchestrator_test.go`:

```go
func TestSplitBlocked(t *testing.T) {
	plan := ChannelPlan{
		Channel: "mcpServers",
		Changes: []Change{
			{Kind: ChangeCreate, ID: "free", Resource: Resource{ID: "free"}},
			{Kind: ChangeCreate, ID: "blocked", Resource: Resource{ID: "blocked", Requires: []string{"cli:ssh"}}},
			{Kind: ChangeNoop, ID: "noop", Resource: Resource{ID: "noop", Requires: []string{"cli:ssh"}}},
		},
	}
	failedRefs := map[string]bool{"cli:ssh": true}

	runnable, skipped := splitBlocked(plan, failedRefs)

	gotRunnable := []string{}
	for _, c := range runnable.Changes {
		gotRunnable = append(gotRunnable, c.ID)
	}
	// "free" runs; "blocked" is skipped; "noop" stays runnable (a noop is free).
	if len(gotRunnable) != 2 || gotRunnable[0] != "free" || gotRunnable[1] != "noop" {
		t.Errorf("runnable = %v, want [free noop]", gotRunnable)
	}
	if len(skipped) != 1 || skipped[0].Change.ID != "blocked" {
		t.Fatalf("skipped = %+v, want one skip for 'blocked'", skipped)
	}
	if !strings.Contains(skipped[0].Reason, "cli:ssh") {
		t.Errorf("skip reason should name the failed dependency, got %q", skipped[0].Reason)
	}
}
```

Add `"strings"` to the import block of `orchestrator_test.go` (it currently imports `"os"`, `"path/filepath"`, `"testing"`, and the `lockfile` package).

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./internal/provider/ -run TestSplitBlocked -v`
Expected: FAIL — `splitBlocked` is undefined, the package does not compile.

- [ ] **Step 4: Implement `splitBlocked` and `nodeRef`**

Append to `internal/provider/orchestrator.go` (the file already imports `"sort"`; add `"fmt"` to its import block):

```go
// nodeRef returns the dependency-graph node ref for a resource — the same
// "<prefix>:<id>" scheme the resolve pipeline uses (e.g. "cli:ssh", "svc:db").
func nodeRef(channel, id string) string {
	if p, ok := channelPrefix[channel]; ok {
		return p + ":" + id
	}
	return channel + ":" + id
}

// splitBlocked partitions plan into the changes that may run and the changes
// blocked because a resource they require is in failedRefs. A blocked non-noop
// change becomes a ChangeSkip; noop changes always stay runnable.
func splitBlocked(plan ChannelPlan, failedRefs map[string]bool) (runnable ChannelPlan, skipped []ChangeSkip) {
	runnable = ChannelPlan{Channel: plan.Channel}
	for _, c := range plan.Changes {
		blockedBy := ""
		if c.Kind != ChangeNoop {
			for _, ref := range c.Resource.Requires {
				if failedRefs[ref] {
					blockedBy = ref
					break
				}
			}
		}
		if blockedBy != "" {
			skipped = append(skipped, ChangeSkip{
				Change: c,
				Reason: fmt.Sprintf("requires %q, which failed earlier in this apply", blockedBy),
			})
			continue
		}
		runnable.Changes = append(runnable.Changes, c)
	}
	return runnable, skipped
}
```

`channelPrefix` is the existing map in `internal/provider/resources.go:55`.

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./internal/provider/ -run TestSplitBlocked -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/provider/provider.go internal/provider/orchestrator.go internal/provider/orchestrator_test.go
git commit -m "Add ChangeFailure/ChangeSkip types and splitBlocked helper"
```

---

### Task 2: Add the partial-ledger builder

After a partial apply the applied ledger must reflect machine reality: a resource that succeeded gets its *desired* entry; a resource that failed or was skipped falls back to its *prior* entry (or is absent if it had none). When nothing failed, the result equals the desired lock — identical to today's behaviour.

**Files:**
- Modify: `internal/provider/orchestrator.go` (add `buildLedger`, `mergeLedgerChannel`)
- Test: `internal/provider/orchestrator_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/provider/orchestrator_test.go`:

```go
func TestBuildLedgerFailedFallsBackToPrior(t *testing.T) {
	prior := &lockfile.Lock{Version: 1, Entries: lockfile.Entries{
		Skills:   map[string]lockfile.Entry{"s": {Layer: "repo", ContentHash: "old"}},
		CLITools: map[string]lockfile.Entry{},
	}}
	desired := &lockfile.Lock{Version: 1, Entries: lockfile.Entries{
		Skills:   map[string]lockfile.Entry{"s": {Layer: "repo", ContentHash: "new"}},
		CLITools: map[string]lockfile.Entry{"x": {Layer: "repo", ContentHash: "h"}},
	}}
	results := []ApplyResult{
		{Channel: "skills", Applied: []Change{{Kind: ChangeUpdate, ID: "s"}}},
		{Channel: "cliTools", Failed: []ChangeFailure{{Change: Change{Kind: ChangeCreate, ID: "x"}}}},
	}

	ledger := buildLedger(prior, desired, results)

	// "s" succeeded -> desired entry ("new").
	if got := ledger.Entries.Skills["s"].ContentHash; got != "new" {
		t.Errorf("skills[s] hash = %q, want %q (succeeded -> desired)", got, "new")
	}
	// "x" failed to create and had no prior entry -> absent.
	if _, ok := ledger.Entries.CLITools["x"]; ok {
		t.Errorf("cliTools[x] present; want absent (failed create with no prior)")
	}
}

func TestBuildLedgerNoFailuresEqualsDesired(t *testing.T) {
	prior := &lockfile.Lock{Version: 1, Entries: lockfile.Entries{
		Skills: map[string]lockfile.Entry{"s": {Layer: "repo", ContentHash: "old"}},
	}}
	desired := &lockfile.Lock{Version: 1, Entries: lockfile.Entries{
		Skills: map[string]lockfile.Entry{"s": {Layer: "repo", ContentHash: "new"}},
	}}
	results := []ApplyResult{
		{Channel: "skills", Applied: []Change{{Kind: ChangeUpdate, ID: "s"}}},
	}

	ledger := buildLedger(prior, desired, results)
	if got := ledger.Entries.Skills["s"].ContentHash; got != "new" {
		t.Errorf("skills[s] hash = %q, want %q (no failures -> desired)", got, "new")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/provider/ -run TestBuildLedger -v`
Expected: FAIL — `buildLedger` is undefined.

- [ ] **Step 3: Implement `buildLedger` and `mergeLedgerChannel`**

Append to `internal/provider/orchestrator.go`:

```go
// buildLedger constructs the applied-state ledger after a (possibly partial)
// apply. A resource that failed or was skipped falls back to its prior entry
// (or is dropped if it had none); every other resource takes its desired entry.
// With no failures the result equals desired — today's behaviour.
func buildLedger(prior, desired *lockfile.Lock, results []ApplyResult) *lockfile.Lock {
	bad := map[string]bool{} // key: "<channel>/<id>"
	for _, r := range results {
		for _, f := range r.Failed {
			bad[r.Channel+"/"+f.Change.ID] = true
		}
		for _, s := range r.Skipped {
			bad[r.Channel+"/"+s.Change.ID] = true
		}
	}
	d, p := desired.Entries, prior.Entries
	return &lockfile.Lock{
		Version:      desired.Version,
		GeneratedAt:  desired.GeneratedAt,
		ManifestHash: desired.ManifestHash,
		Entries: lockfile.Entries{
			MCPServers:         mergeLedgerChannel("mcpServers", d.MCPServers, p.MCPServers, bad),
			BackgroundServices: mergeLedgerChannel("backgroundServices", d.BackgroundServices, p.BackgroundServices, bad),
			Hooks:              mergeLedgerChannel("hooks", d.Hooks, p.Hooks, bad),
			Commands:           mergeLedgerChannel("commands", d.Commands, p.Commands, bad),
			CLITools:           mergeLedgerChannel("cliTools", d.CLITools, p.CLITools, bad),
			Skills:             mergeLedgerChannel("skills", d.Skills, p.Skills, bad),
			Plugins:            mergeLedgerChannel("plugins", d.Plugins, p.Plugins, bad),
			Rules:              mergeLedgerChannel("rules", d.Rules, p.Rules, bad),
			Tools:              mergeLedgerChannel("tools", d.Tools, p.Tools, bad),
		},
	}
}

// mergeLedgerChannel merges one channel's desired and prior entry maps: a
// "<channel>/<id>" present in bad takes the prior entry (or is dropped if prior
// has none); otherwise the desired entry. Prior-only ids (e.g. a failed delete)
// are re-added when bad.
func mergeLedgerChannel(channel string, desiredCh, priorCh map[string]lockfile.Entry, bad map[string]bool) map[string]lockfile.Entry {
	out := make(map[string]lockfile.Entry, len(desiredCh))
	for id, e := range desiredCh {
		if bad[channel+"/"+id] {
			if pe, ok := priorCh[id]; ok {
				out[id] = pe
			}
			continue
		}
		out[id] = e
	}
	for id, pe := range priorCh {
		if _, inDesired := desiredCh[id]; inDesired {
			continue
		}
		if bad[channel+"/"+id] {
			out[id] = pe
		}
	}
	return out
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/provider/ -run TestBuildLedger -v`
Expected: both PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/provider/orchestrator.go internal/provider/orchestrator_test.go
git commit -m "Add partial applied-ledger builder"
```

---

### Task 3: Rewrite `ApplyAllRendered` to isolate failures

`ApplyAllRendered` stops returning on the first error. It applies every channel, skips dependents of failed resources, writes the partial ledger, and returns the per-channel results plus an aggregated `*ApplyError`. The signature changes from `error` to `([]ApplyResult, error)`.

**Files:**
- Modify: `internal/provider/provider.go` (add `ApplyError`)
- Modify: `internal/provider/orchestrator.go:132-155` (`ApplyAllRendered`)
- Modify: `internal/provider/orchestrator_test.go` (fix the two existing `ApplyAllRendered` callers; add new tests)
- Modify: `cmd/ainfra/commands.go` (fix the `ApplyAllRendered` call site)

- [ ] **Step 1: Write the failing tests**

First add a scriptable fake provider. Append to `internal/provider/orchestrator_test.go`:

```go
// scriptedProvider returns a fixed ApplyResult/error and records the plan it
// received, so tests can drive orchestrator failure paths.
type scriptedProvider struct {
	channel  string
	observed []Resource
	result   ApplyResult
	err      error
	gotPlan  ChannelPlan
}

func (s *scriptedProvider) Channel() string                 { return s.channel }
func (s *scriptedProvider) Observe(Env) ([]Resource, error) { return s.observed, nil }
func (s *scriptedProvider) Apply(_ Env, p ChannelPlan) (ApplyResult, error) {
	s.gotPlan = p
	r := s.result
	r.Channel = s.channel
	return r, s.err
}
```

Then the behaviour tests, also appended to `orchestrator_test.go`:

```go
func TestApplyAllRenderedContinuesPastChannelFailure(t *testing.T) {
	root := t.TempDir()
	// cliTools is processed before skills; cliTools errors catastrophically.
	cliTools := &scriptedProvider{channel: "cliTools", err: errors.New("brew is broken")}
	skills := &scriptedProvider{channel: "skills"}
	o := NewOrchestrator(root, Env{}, []Provider{cliTools, skills})

	rendered := map[string][]Resource{
		"cliTools": {{ID: "x", Channel: "cliTools", ContentHash: "h"}},
		"skills":   {{ID: "s", Channel: "skills", ContentHash: "h"}},
	}
	_, err := o.ApplyAllRendered(rendered, newTestLock())
	if err == nil {
		t.Fatal("expected an aggregated error, got nil")
	}
	if _, ok := err.(*ApplyError); !ok {
		t.Errorf("error type = %T, want *ApplyError", err)
	}
	// skills must still have been applied despite cliTools failing.
	if skills.gotPlan.Empty() {
		t.Error("skills channel was not applied after cliTools failed")
	}
}

func TestApplyAllRenderedSkipsBlockedDependents(t *testing.T) {
	root := t.TempDir()
	// cliTools fails resource "x"; mcpServers "m" requires "cli:x".
	cliTools := &scriptedProvider{
		channel: "cliTools",
		result: ApplyResult{
			Failed: []ChangeFailure{{Change: Change{Kind: ChangeCreate, ID: "x"}, Err: errors.New("install failed")}},
		},
	}
	mcp := &scriptedProvider{channel: "mcpServers"}
	o := NewOrchestrator(root, Env{}, []Provider{cliTools, mcp})

	rendered := map[string][]Resource{
		"cliTools":   {{ID: "x", Channel: "cliTools", ContentHash: "h"}},
		"mcpServers": {{ID: "m", Channel: "mcpServers", ContentHash: "h", Requires: []string{"cli:x"}}},
	}
	results, err := o.ApplyAllRendered(rendered, newTestLock())
	if err == nil {
		t.Fatal("expected an aggregated error, got nil")
	}
	// "m" must not have been handed to the mcpServers provider.
	for _, c := range mcp.gotPlan.Changes {
		if c.ID == "m" && c.Kind != ChangeNoop {
			t.Errorf("blocked resource 'm' was passed to the provider: %+v", c)
		}
	}
	// "m" must be reported as skipped.
	skippedM := false
	for _, r := range results {
		if r.Channel != "mcpServers" {
			continue
		}
		for _, s := range r.Skipped {
			if s.Change.ID == "m" {
				skippedM = true
			}
		}
	}
	if !skippedM {
		t.Errorf("resource 'm' not reported as skipped; results = %+v", results)
	}
}

func TestApplyAllRenderedWritesPartialLedger(t *testing.T) {
	root := t.TempDir()
	cliTools := &scriptedProvider{
		channel: "cliTools",
		result: ApplyResult{
			Failed: []ChangeFailure{{Change: Change{Kind: ChangeCreate, ID: "x"}, Err: errors.New("nope")}},
		},
	}
	skills := &scriptedProvider{
		channel:  "skills",
		observed: []Resource{{ID: "s", Channel: "skills"}}, // present -> "s" is an update
		result:   ApplyResult{Applied: []Change{{Kind: ChangeUpdate, ID: "s"}}},
	}
	o := NewOrchestrator(root, Env{}, []Provider{cliTools, skills})

	desired := &lockfile.Lock{Version: 1, Entries: lockfile.Entries{
		Skills:   map[string]lockfile.Entry{"s": {Layer: "repo", ContentHash: "new"}},
		CLITools: map[string]lockfile.Entry{"x": {Layer: "repo", ContentHash: "h"}},
	}}
	rendered := map[string][]Resource{
		"cliTools": {{ID: "x", Channel: "cliTools", ContentHash: "h"}},
		"skills":   {{ID: "s", Channel: "skills", ContentHash: "new"}},
	}
	if _, err := o.ApplyAllRendered(rendered, desired); err == nil {
		t.Fatal("expected an aggregated error, got nil")
	}

	ledger, err := lockfile.Read(filepath.Join(root, ".ainfra", "applied.lock"))
	if err != nil {
		t.Fatalf("ledger not written: %v", err)
	}
	if got := ledger.Entries.Skills["s"].ContentHash; got != "new" {
		t.Errorf("ledger skills[s] = %q, want %q (succeeded)", got, "new")
	}
	if _, ok := ledger.Entries.CLITools["x"]; ok {
		t.Errorf("ledger cliTools[x] present; want absent (failed create)")
	}
}
```

Add `"errors"` to the `orchestrator_test.go` import block.

Finally, fix the two existing callers from the item-5 plan so the package compiles under the new signature. In `orchestrator_test.go`, in `TestApplyAllRenderedDryRunSkipsLedger` change:

```go
	if err := o.ApplyAllRendered(rendered, newTestLock()); err != nil {
```

to:

```go
	if _, err := o.ApplyAllRendered(rendered, newTestLock()); err != nil {
```

and in `TestApplyAllRenderedWritesLedgerWhenNotDryRun` make the identical `if err :=` → `if _, err :=` change.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/provider/ -run TestApplyAllRendered -v`
Expected: FAIL — `ApplyError` is undefined and `ApplyAllRendered` still returns a single value, so the package does not compile.

- [ ] **Step 3: Add the `ApplyError` type**

In `internal/provider/provider.go`, append:

```go
// ApplyError aggregates the per-resource failures of a partial apply. When it
// is returned the applied ledger has already been written for everything that
// succeeded.
type ApplyError struct {
	Errs []error
}

// Error summarizes the failures. The full per-resource list is on Errs.
func (e *ApplyError) Error() string {
	if len(e.Errs) == 1 {
		return e.Errs[0].Error()
	}
	return fmt.Sprintf("%d resources failed to apply", len(e.Errs))
}
```

Add `"fmt"` to the `provider.go` import block (the file currently has no imports — add `import "fmt"` below the package clause).

- [ ] **Step 4: Rewrite `ApplyAllRendered`**

In `internal/provider/orchestrator.go`, replace the whole `ApplyAllRendered` function (the doc comment plus the body, lines 132-155) with:

```go
// ApplyAllRendered applies rendered resources (which carry Payload) and writes
// the applied ledger. Unlike a fail-fast apply, it does not stop on the first
// error: it applies every channel, skips resources whose requires: dependency
// failed earlier in the run, and writes a partial ledger (succeeded resources
// take their desired entry; failed or skipped ones fall back to prior). It
// returns the per-channel results and, if anything failed, an *ApplyError.
// When env.DryRun is set, providers still run but the ledger is not written.
func (o *Orchestrator) ApplyAllRendered(rendered map[string][]Resource, desired *lockfile.Lock) ([]ApplyResult, error) {
	plans, err := o.PlanAllRendered(rendered)
	if err != nil {
		return nil, err
	}
	prior, err := ReadApplied(o.root)
	if err != nil {
		return nil, err
	}

	failedRefs := map[string]bool{} // node refs that failed or were skipped
	var results []ApplyResult
	var errs []error

	for _, ch := range o.sortedChannels() {
		plan := plans[ch]
		if plan.Empty() {
			continue
		}
		p := o.providers[ch]

		runnable, skipped := splitBlocked(plan, failedRefs)

		res := ApplyResult{Channel: ch}
		r, applyErr := p.Apply(o.env, runnable)
		if applyErr != nil {
			// A catastrophic channel error fails every runnable change in it.
			for _, c := range runnable.Changes {
				if c.Kind != ChangeNoop {
					res.Failed = append(res.Failed, ChangeFailure{Change: c, Err: applyErr})
				}
			}
		} else {
			res = r
			res.Channel = ch
		}
		res.Skipped = append(res.Skipped, skipped...)

		for _, f := range res.Failed {
			failedRefs[nodeRef(ch, f.Change.ID)] = true
			errs = append(errs, fmt.Errorf("%s %s: %w", ch, f.Change.ID, f.Err))
		}
		for _, s := range res.Skipped {
			failedRefs[nodeRef(ch, s.Change.ID)] = true
		}
		results = append(results, res)
	}

	ledger := buildLedger(prior, desired, results)
	if !o.env.DryRun {
		if err := WriteApplied(o.root, ledger); err != nil {
			return results, err
		}
	}

	if len(errs) > 0 {
		return results, &ApplyError{Errs: errs}
	}
	return results, nil
}
```

- [ ] **Step 5: Fix the `cmd/ainfra` call site**

In `cmd/ainfra/commands.go`, `runApply` currently has:

```go
	if err := orch.ApplyAllRendered(rendered, merged); err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}

	if dryRun {
		fmt.Fprintln(ctx.Stdout, "Dry run complete — no changes were applied.")
	} else {
		fmt.Fprintln(ctx.Stdout, "Apply complete.")
	}
	return 0
}
```

Replace it with (the summary output is wired in Task 5 — for now just consume both return values):

```go
	if _, err := orch.ApplyAllRendered(rendered, merged); err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}

	if dryRun {
		fmt.Fprintln(ctx.Stdout, "Dry run complete — no changes were applied.")
	} else {
		fmt.Fprintln(ctx.Stdout, "Apply complete.")
	}
	return 0
}
```

- [ ] **Step 6: Run the new tests, then the whole suite**

Run: `go test ./internal/provider/ -run TestApplyAllRendered -v`
Expected: all PASS (the three new tests plus the two dry-run tests).

Run: `go test ./...`
Expected: all packages PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/provider/provider.go internal/provider/orchestrator.go internal/provider/orchestrator_test.go cmd/ainfra/commands.go
git commit -m "Isolate apply failures: continue past errors, skip blocked dependents, write partial ledger"
```

---

### Task 4: Per-entry isolation in the `cliTools` provider

`CLITools.Apply` returns on the first failed change today. Refactor it to apply each entry independently and collect failures into `ApplyResult.Failed`, so one bad `brew install` does not abort its siblings. The per-change logic moves into a helper, `applyOne`.

**Files:**
- Modify: `internal/provider/shared/clitools.go:30-95` (`Apply`)
- Test: `internal/provider/shared/clitools_test.go`

- [ ] **Step 1: Write the failing test and update the one test that expects an error return**

Append a new test to `internal/provider/shared/clitools_test.go`:

```go
func TestCLIToolsApply_OneFailureDoesNotAbortSiblings(t *testing.T) {
	runner := provider.NewFakeRunner()
	// "good" installs cleanly.
	runner.Script["brew list --versions good"] = provider.FakeResult{Err: errors.New("absent")}
	runner.Script["brew install good"] = provider.FakeResult{Output: []byte("ok")}
	// "bad" is absent and its install fails.
	runner.Script["brew list --versions bad"] = provider.FakeResult{Err: errors.New("absent")}
	runner.Script["brew install bad"] = provider.FakeResult{Err: errors.New("brew install bad: network error")}

	env := provider.Env{FS: provider.NewMemFilesystem(), Runner: runner, Root: "/repo"}

	mkChange := func(id string) provider.Change {
		return provider.Change{
			Kind: provider.ChangeCreate,
			ID:   id,
			Resource: provider.Resource{
				ID:      id,
				Channel: "cliTools",
				Payload: map[string]any{"install": map[string]any{"brew": map[string]any{}}},
			},
		}
	}
	plan := provider.ChannelPlan{
		Channel: "cliTools",
		Changes: []provider.Change{mkChange("good"), mkChange("bad")},
	}

	res, err := shared.CLITools{}.Apply(env, plan)
	if err != nil {
		t.Fatalf("Apply should not return a top-level error for a per-entry failure: %v", err)
	}
	if len(res.Applied) != 1 || res.Applied[0].ID != "good" {
		t.Errorf("Applied = %+v, want one entry 'good'", res.Applied)
	}
	if len(res.Failed) != 1 || res.Failed[0].Change.ID != "bad" {
		t.Fatalf("Failed = %+v, want one failure 'bad'", res.Failed)
	}
	if res.Failed[0].Err == nil {
		t.Error("Failed[0].Err is nil; want the underlying install error")
	}
}
```

Then update `TestCLIToolsApply_UnknownMethodToolAbsent_ReturnsError` (lines 136-174) — a per-entry failure is now reported in `ApplyResult.Failed`, not as the `error` return. Rename it and replace its assertion block. Change the function name on line 136 to `TestCLIToolsApply_UnknownMethodToolAbsent_Fails`, and replace lines 166-173:

```go
	p := shared.CLITools{}
	_, err := p.Apply(env, plan)
	if err == nil {
		t.Fatal("Apply: expected error for absent tool with no supported install method, got nil")
	}
	if !strings.Contains(err.Error(), "install it manually") {
		t.Errorf("error message should mention manual install, got: %v", err)
	}
}
```

with:

```go
	p := shared.CLITools{}
	res, err := p.Apply(env, plan)
	if err != nil {
		t.Fatalf("Apply: per-entry failures go in ApplyResult.Failed, not the error return: %v", err)
	}
	if len(res.Failed) != 1 || res.Failed[0].Change.ID != "mytool" {
		t.Fatalf("Failed = %+v, want one failure for 'mytool'", res.Failed)
	}
	if !strings.Contains(res.Failed[0].Err.Error(), "install it manually") {
		t.Errorf("failure message should mention manual install, got: %v", res.Failed[0].Err)
	}
}
```

- [ ] **Step 2: Run the tests to verify the state**

Run: `go test ./internal/provider/shared/ -run TestCLIToolsApply -v`
Expected: `TestCLIToolsApply_OneFailureDoesNotAbortSiblings` FAILS (the second install error aborts `Apply` and is returned as `err`), and `TestCLIToolsApply_UnknownMethodToolAbsent_Fails` FAILS (it still gets a non-nil `err`).

- [ ] **Step 3: Refactor `CLITools.Apply`**

In `internal/provider/shared/clitools.go`, replace the entire `Apply` method (its doc comment and body, lines 28-95) with:

```go
// Apply executes the channel plan. Each Create/Update entry is applied
// independently: it selects a package adapter from the install payload and
// installs if not already present, or runs a declare-and-check probe when no
// supported adapter is declared. A failed entry is recorded in
// ApplyResult.Failed and does not abort its siblings. Delete changes are a
// no-op — ainfra does not uninstall CLI tools (see design §6). Honors
// env.DryRun and env.NoInstall (both skip the install and probe).
func (CLITools) Apply(env provider.Env, plan provider.ChannelPlan) (provider.ApplyResult, error) {
	var applied []provider.Change
	var failed []provider.ChangeFailure

	for _, c := range plan.Changes {
		if c.Kind == provider.ChangeNoop {
			continue
		}
		if c.Kind == provider.ChangeDelete {
			// ainfra does not uninstall CLI tools; treat as a no-op.
			applied = append(applied, c)
			continue
		}
		if err := applyOne(env, c); err != nil {
			failed = append(failed, provider.ChangeFailure{Change: c, Err: err})
			continue
		}
		applied = append(applied, c)
	}

	return provider.ApplyResult{
		Channel: "cliTools",
		Applied: applied,
		Failed:  failed,
	}, nil
}

// applyOne installs a single CLI tool. It selects the first install method
// pkg.Select recognises and installs the tool if absent; if no supported
// adapter is declared it runs a `<tool> --version` probe. It returns nil when
// the tool is present (or env.DryRun/env.NoInstall suppressed the work).
func applyOne(env provider.Env, c provider.Change) error {
	id := c.ID
	installMap, _ := c.Resource.Payload["install"].(map[string]any)

	for _, method := range slices.Sorted(maps.Keys(installMap)) {
		adapter, ok := pkg.Select(method)
		if !ok {
			continue
		}
		if !env.DryRun && !env.NoInstall {
			installed, err := adapter.IsInstalled(env, id)
			if err != nil {
				return fmt.Errorf("checking install state via %s failed: %w", adapter.Name(), err)
			}
			if !installed {
				if err := adapter.Install(env, id); err != nil {
					return fmt.Errorf("install via %s failed: %w", adapter.Name(), err)
				}
			}
		}
		return nil
	}

	// No recognised adapter — declare-and-check fallback.
	if !env.DryRun && !env.NoInstall {
		if _, err := env.Runner.Run(id, "--version"); err != nil {
			return fmt.Errorf("not installed and no supported install method is declared; install it manually")
		}
	}
	return nil
}
```

The imports (`fmt`, `maps`, `slices`, `provider`, `pkg`) are unchanged — all are still used.

- [ ] **Step 4: Run the cliTools tests to verify they pass**

Run: `go test ./internal/provider/shared/ -run TestCLIToolsApply -v`
Expected: all PASS — including the new isolation test and the renamed `_Fails` test, and the existing `CreateWithBrew`, `DryRun_NoInstall`, `NoInstall*`, `MultiMethod`, and `Delete_Noop` tests.

- [ ] **Step 5: Commit**

```bash
git add internal/provider/shared/clitools.go internal/provider/shared/clitools_test.go
git commit -m "Isolate cliTools apply failures per entry"
```

---

### Task 5: Print the apply summary

`runApply` prints an `applied N, skipped M, failed K` line after every apply, and on failure lists each failed and skipped resource with its reason.

**Files:**
- Modify: `cmd/ainfra/commands.go` (`runApply`, plus a `renderApplySummary` helper)
- Test: `cmd/ainfra/cmd_apply_test.go`

- [ ] **Step 1: Write the failing test**

Append to `cmd/ainfra/cmd_apply_test.go`:

```go
func TestApplyPrintsSummary(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "hello.md"), []byte("# hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	yaml := "version: 1\ncommands:\n  hello:\n    source: hello.md\n"
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"--chdir", dir, "lock"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("lock failed")
	}

	var out, errOut bytes.Buffer
	code := run([]string{"--chdir", dir, "apply", "--yes"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("apply --yes: code=%d out=%q err=%q", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "applied 1, skipped 0, failed 0") {
		t.Errorf("expected an apply summary line, got: %q", out.String())
	}
}

func TestApplyFailureListsFailedResource(t *testing.T) {
	dir := t.TempDir()

	// A CLI tool whose binary is absent and whose only install method is
	// unrecognised — its cliTools entry fails the declare-and-check probe.
	yaml := "version: 1\n" +
		"cliTools:\n" +
		"  ainfra-absent-tool-xyz:\n" +
		"    install:\n" +
		"      manual: {}\n"
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"--chdir", dir, "lock"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("lock failed")
	}

	var out, errOut bytes.Buffer
	code := run([]string{"--chdir", dir, "apply", "--yes"}, &out, &errOut)
	if code == 0 {
		t.Fatalf("apply of an absent tool: expected non-zero exit, got 0; out=%q", out.String())
	}
	combined := out.String() + errOut.String()
	if !strings.Contains(combined, "failed 1") {
		t.Errorf("expected 'failed 1' in the summary, got: %q", combined)
	}
	if !strings.Contains(combined, "ainfra-absent-tool-xyz") {
		t.Errorf("expected the failed resource id in the output, got: %q", combined)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./cmd/ainfra/ -run 'TestApplyPrintsSummary|TestApplyFailureLists' -v`
Expected: both FAIL — no summary line is printed yet (`TestApplyFailureListsFailedResource` already exits non-zero but lacks the `failed 1` text).

- [ ] **Step 3: Add the summary rendering**

In `cmd/ainfra/commands.go`, add `"io"` to the import block. Add this helper (place it just above `runApply`):

```go
// renderApplySummary prints the one-line apply tally and, for any failed or
// skipped resource, a reason line.
func renderApplySummary(stdout, stderr io.Writer, results []provider.ApplyResult) {
	var applied, skipped, failed int
	for _, r := range results {
		applied += len(r.Applied)
		skipped += len(r.Skipped)
		failed += len(r.Failed)
	}
	fmt.Fprintf(stdout, "applied %d, skipped %d, failed %d\n", applied, skipped, failed)
	for _, r := range results {
		for _, f := range r.Failed {
			fmt.Fprintf(stderr, "  failed:  %s %s — %v\n", r.Channel, f.Change.ID, f.Err)
		}
		for _, s := range r.Skipped {
			fmt.Fprintf(stderr, "  skipped: %s %s — %s\n", r.Channel, s.Change.ID, s.Reason)
		}
	}
}
```

Then replace the `ApplyAllRendered` call block in `runApply` (the version Task 3 step 5 produced) with:

```go
	results, err := orch.ApplyAllRendered(rendered, merged)
	renderApplySummary(ctx.Stdout, ctx.Stderr, results)
	if err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}

	if dryRun {
		fmt.Fprintln(ctx.Stdout, "Dry run complete — no changes were applied.")
	} else {
		fmt.Fprintln(ctx.Stdout, "Apply complete.")
	}
	return 0
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./cmd/ainfra/ -run 'TestApplyPrintsSummary|TestApplyFailureLists' -v`
Expected: both PASS.

- [ ] **Step 5: Run the whole suite**

Run: `go test ./... && go vet ./...`
Expected: all packages PASS, vet clean.

- [ ] **Step 6: Commit**

```bash
git add cmd/ainfra/commands.go cmd/ainfra/cmd_apply_test.go
git commit -m "Print apply summary with failed and skipped resources"
```

---

## Self-review notes

- **Spec coverage.** Item 1 of the hardening spec asks for failure accumulation
  (Task 3), requires-aware skip (Tasks 1 + 3), per-entry isolation in `cliTools`
  (Task 4), a partial ledger (Task 2 + 3), and the `applied N, skipped M,
  failed K` summary (Task 5). The eight non-`cliTools` providers keep their
  fail-fast `Apply`; the orchestrator degrades that to a whole-channel failure
  and continues — the scoped behaviour chosen for this plan. Per-entry
  isolation for those providers is the noted follow-up.
- **Type consistency.** `ApplyResult` gains `Failed []ChangeFailure` and
  `Skipped []ChangeSkip`; those names are used identically in `provider.go`,
  `orchestrator.go`, `clitools.go`, and the tests. `ApplyAllRendered`'s final
  signature is `([]ApplyResult, error)`, applied in Task 3 and consumed by the
  Task 3 call-site fix and the Task 5 rewrite.
- **`ApplyAll` is intentionally untouched.** The non-rendered `ApplyAll` still
  fails fast; it is exercised only by `TestOrchestratorPlanAndApply` and
  `TestOrchestratorChannelOrder` and is not on the `apply` command path
  (`runApply` uses `ApplyAllRendered`). Unifying the two is out of scope.
- **Ordering caveat.** `requires:` edges resolve only to `cli:` and `svc:`
  refs, and `channelOrder` applies `cliTools` then `backgroundServices` before
  the dependent channels — so a service that requires a failed CLI tool is
  correctly skipped. A CLI tool that requires a *service* would not be (the
  service is applied later). This matches the spec's motivating example and is
  acceptable for this increment.
- **No regression in the happy path.** With zero failures, `buildLedger`
  returns a lock equal to `desired` — exactly what `WriteApplied(o.root,
  desired)` wrote before — so `TestE2EReconciliation`, `TestApplyYesWritesFile`,
  and the dry-run tests keep passing; the only new output is the additive
  summary line.
