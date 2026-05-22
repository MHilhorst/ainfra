# Apply Dry-Run and Sandbox Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `ainfra apply --dry-run` (preview an apply with zero writes) and `ainfra apply --no-install` (reconcile config files but skip CLI-tool installs).

**Architecture:** `provider.Env` already carries a `DryRun` bool that every channel provider honours. This plan wires a `--dry-run` CLI flag to it and makes the orchestrator skip the applied-ledger write under dry-run. It then adds a parallel `NoInstall` bool — honoured only by the `cliTools` provider — and a `--no-install` flag. No new packages, no signature changes to `buildEnv`; the apply command sets the two `Env` fields directly on the value `buildEnv` returns.

**Tech Stack:** Go 1.25, standard library only. Tests use the `run()` CLI harness in `cmd/ainfra` and the `FakeRunner`/`MemFilesystem` fakes in `internal/provider`.

**Context:** This is item 5 of `docs/superpowers/specs/2026-05-22-apply-hardening-design.md`. It is sequenced first because it makes the other five items safe to test.

---

### Task 1: Orchestrator skips the applied-ledger write under dry-run

The orchestrator stores `Env` as `o.env`. Under dry-run, every provider's `Apply` is still called (each no-ops its own writes), but `ApplyAllRendered` must not write `.ainfra/applied.lock` — a dry run that recorded a ledger would falsely claim the machine was reconciled.

**Files:**
- Modify: `internal/provider/orchestrator.go:137-155` (`ApplyAllRendered`)
- Test: `internal/provider/orchestrator_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/provider/orchestrator_test.go`. Add `"os"` to the import block (it currently imports `"path/filepath"`, `"testing"`, and the `lockfile` package).

```go
func TestApplyAllRenderedDryRunSkipsLedger(t *testing.T) {
	root := t.TempDir()
	skills := &stubProvider{channel: "skills"} // observes nothing -> "s" is a create
	o := NewOrchestrator(root, Env{DryRun: true}, []Provider{skills})

	rendered := map[string][]Resource{
		"skills": {{ID: "s", Channel: "skills", ContentHash: "h1"}},
	}
	if err := o.ApplyAllRendered(rendered, newTestLock()); err != nil {
		t.Fatalf("ApplyAllRendered (dry run): %v", err)
	}
	if len(skills.applied) != 1 {
		t.Errorf("dry run should still call provider Apply, got %+v", skills.applied)
	}
	if _, err := os.Stat(filepath.Join(root, ".ainfra", "applied.lock")); !os.IsNotExist(err) {
		t.Errorf("dry run wrote the applied ledger; want it skipped (stat err = %v)", err)
	}
}

func TestApplyAllRenderedWritesLedgerWhenNotDryRun(t *testing.T) {
	root := t.TempDir()
	skills := &stubProvider{channel: "skills"}
	o := NewOrchestrator(root, Env{}, []Provider{skills})

	rendered := map[string][]Resource{
		"skills": {{ID: "s", Channel: "skills", ContentHash: "h1"}},
	}
	if err := o.ApplyAllRendered(rendered, newTestLock()); err != nil {
		t.Fatalf("ApplyAllRendered: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".ainfra", "applied.lock")); err != nil {
		t.Errorf("non-dry-run apply did not write the applied ledger: %v", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify the first fails**

Run: `go test ./internal/provider/ -run TestApplyAllRendered -v`
Expected: `TestApplyAllRenderedDryRunSkipsLedger` FAILS with "dry run wrote the applied ledger" (the ledger is written unconditionally today). `TestApplyAllRenderedWritesLedgerWhenNotDryRun` PASSES.

- [ ] **Step 3: Make ApplyAllRendered honour DryRun**

In `internal/provider/orchestrator.go`, change the end of `ApplyAllRendered` from:

```go
	for _, ch := range o.sortedChannels() {
		plan := plans[ch]
		if plan.Empty() {
			continue
		}
		p := o.providers[ch]
		if _, err := p.Apply(o.env, plan); err != nil {
			return err
		}
	}

	return WriteApplied(o.root, desired)
}
```

to:

```go
	for _, ch := range o.sortedChannels() {
		plan := plans[ch]
		if plan.Empty() {
			continue
		}
		p := o.providers[ch]
		if _, err := p.Apply(o.env, plan); err != nil {
			return err
		}
	}

	// A dry run exercises every provider's Apply (each no-ops its own writes)
	// but must not record a ledger — the machine was not reconciled.
	if o.env.DryRun {
		return nil
	}
	return WriteApplied(o.root, desired)
}
```

Also update the doc comment on `ApplyAllRendered` (lines 132-136): append a sentence — `When env.DryRun is set, providers still run but the applied ledger is not written.`

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/provider/ -run TestApplyAllRendered -v`
Expected: both PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/provider/orchestrator.go internal/provider/orchestrator_test.go
git commit -m "Skip applied-ledger write on dry-run apply"
```

---

### Task 2: Add the `apply --dry-run` flag

Wire a `--dry-run` flag through `newApplyCommand` and `runApply`. Under dry-run the confirmation prompt is skipped (nothing to confirm) and the final message names it a dry run. The plan is still rendered and preconditions are still checked, so the output is an honest preview of what apply would do.

**Files:**
- Modify: `cmd/ainfra/commands.go:141-241` (`newApplyCommand`, `runApply`)
- Test: `cmd/ainfra/cmd_apply_test.go`

- [ ] **Step 1: Write the failing test**

Add to `cmd/ainfra/cmd_apply_test.go` (its imports — `bytes`, `os`, `path/filepath`, `strings`, `testing` — already cover this):

```go
func TestApplyDryRun(t *testing.T) {
	dir := t.TempDir()

	srcContent := "# Hello command\n"
	if err := os.WriteFile(filepath.Join(dir, "hello.md"), []byte(srcContent), 0o644); err != nil {
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
	code := run([]string{"--chdir", dir, "apply", "--dry-run"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("apply --dry-run: code=%d out=%q err=%q", code, out.String(), errOut.String())
	}

	// The command file must NOT be written.
	dest := filepath.Join(dir, ".claude", "commands", "hello.md")
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Errorf("apply --dry-run wrote %s; want no write (stat err = %v)", dest, err)
	}
	// The applied ledger must NOT be written.
	ledger := filepath.Join(dir, ".ainfra", "applied.lock")
	if _, err := os.Stat(ledger); !os.IsNotExist(err) {
		t.Errorf("apply --dry-run wrote the applied ledger; want no write (stat err = %v)", err)
	}
	// Output names it a dry run.
	if !strings.Contains(out.String(), "Dry run") {
		t.Errorf("apply --dry-run: expected 'Dry run' in output, got: %q", out.String())
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./cmd/ainfra/ -run TestApplyDryRun -v`
Expected: FAIL — the `--dry-run` flag is not defined, so `flag.Parse` errors and `run` exits non-zero. (`apply: flag provided but not defined: -dry-run`.)

- [ ] **Step 3: Implement the flag**

In `cmd/ainfra/commands.go`, replace `newApplyCommand` (lines 141-153):

```go
func newApplyCommand() *cli.Command {
	var yes, dryRun bool
	return &cli.Command{
		Name:      "apply",
		Summary:   "Reconcile the environment to match the manifest",
		UsageLine: "ainfra apply [--yes] [--dry-run]",
		Example:   "ainfra apply --dry-run",
		SetFlags: func(fs *flag.FlagSet) {
			fs.BoolVar(&yes, "yes", false, "skip confirmation prompt")
			fs.BoolVar(&dryRun, "dry-run", false, "preview the apply without writing anything")
		},
		Run: func(ctx cli.Context) int {
			return runApply(ctx, yes, dryRun)
		},
	}
}
```

Change the `runApply` signature (line 155) from `func runApply(ctx cli.Context, yes bool) int {` to:

```go
func runApply(ctx cli.Context, yes, dryRun bool) int {
```

In `runApply`, replace the orchestrator-construction line (line 189) — currently `orch := provider.NewOrchestrator(dir, buildEnv(dir), providers)` — with:

```go
	env := buildEnv(dir)
	env.DryRun = dryRun
	orch := provider.NewOrchestrator(dir, env, providers)
```

Replace the precondition-check line (line 213) — currently `if failures := checkPreconditions(dir, buildEnv(dir)); len(failures) > 0 {` — with:

```go
	if failures := checkPreconditions(dir, env); len(failures) > 0 {
```

Replace the confirmation block (lines 222-232) so dry-run skips the prompt:

```go
	// Confirm unless --yes or --dry-run (a dry run changes nothing).
	if !yes && !dryRun {
		ok, err := ui.Confirm(ctx.Stdin, ctx.Stdout, "Do you want to apply these changes? (yes/no): ")
		if err != nil {
			ui.RenderError(ctx.Stderr, errColor, err)
			return 1
		}
		if !ok {
			fmt.Fprintln(ctx.Stdout, "Aborted.")
			return 0
		}
	}
```

Replace the final apply + message block (lines 234-240):

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

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./cmd/ainfra/ -run TestApplyDryRun -v`
Expected: PASS.

- [ ] **Step 5: Run the full apply test file to confirm no regression**

Run: `go test ./cmd/ainfra/ -run TestApply -v`
Expected: all PASS (`TestApplyYesWritesFile`, `TestApplySecondRunNothingToDo`, `TestApplyNoLockFile`, `TestApplyDryRun`).

- [ ] **Step 6: Commit**

```bash
git add cmd/ainfra/commands.go cmd/ainfra/cmd_apply_test.go
git commit -m "Add apply --dry-run flag"
```

---

### Task 3: Add a `NoInstall` field to `provider.Env` and honour it in `CLITools.Apply`

`NoInstall` is like `DryRun` but scoped to the `cliTools` channel only: the file-writing channels still reconcile, but `CLITools.Apply` skips both the adapter install path and the declare-and-check probe. This lets the file channels be tested on a real machine without `brew install` / `npm install -g` side effects.

**Files:**
- Modify: `internal/provider/env.go:33-40` (`Env` struct)
- Modify: `internal/provider/shared/clitools.go:32-95` (`Apply`)
- Test: `internal/provider/shared/clitools_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/provider/shared/clitools_test.go` (package `shared_test`; imports `provider` and `shared` are already present):

```go
func TestCLIToolsApply_NoInstallSkipsAdapter(t *testing.T) {
	runner := provider.NewFakeRunner() // every call is unscripted -> errors if reached
	env := provider.Env{
		FS:        provider.NewMemFilesystem(),
		Runner:    runner,
		Root:      "/repo",
		NoInstall: true,
	}

	plan := provider.ChannelPlan{
		Channel: "cliTools",
		Changes: []provider.Change{{
			Kind: provider.ChangeCreate,
			ID:   "jq",
			Resource: provider.Resource{
				ID:      "jq",
				Channel: "cliTools",
				Payload: map[string]any{
					"install": map[string]any{"brew": map[string]any{}},
				},
			},
		}},
	}

	res, err := shared.CLITools{}.Apply(env, plan)
	if err != nil {
		t.Fatalf("Apply with NoInstall: unexpected error: %v", err)
	}
	if len(runner.Calls) != 0 {
		t.Errorf("NoInstall: expected no commands run, got %v", runner.Calls)
	}
	if len(res.Applied) != 1 {
		t.Errorf("NoInstall: expected the change still recorded as applied, got %+v", res.Applied)
	}
}

func TestCLIToolsApply_NoInstallSkipsDeclareCheckProbe(t *testing.T) {
	runner := provider.NewFakeRunner()
	env := provider.Env{
		FS:        provider.NewMemFilesystem(),
		Runner:    runner,
		Root:      "/repo",
		NoInstall: true,
	}

	// No recognised adapter — normally falls through to a `tool --version` probe.
	plan := provider.ChannelPlan{
		Channel: "cliTools",
		Changes: []provider.Change{{
			Kind: provider.ChangeCreate,
			ID:   "some-tool",
			Resource: provider.Resource{
				ID:      "some-tool",
				Channel: "cliTools",
				Payload: map[string]any{
					"install": map[string]any{"manual": map[string]any{}},
				},
			},
		}},
	}

	res, err := shared.CLITools{}.Apply(env, plan)
	if err != nil {
		t.Fatalf("Apply with NoInstall: unexpected error: %v", err)
	}
	if len(runner.Calls) != 0 {
		t.Errorf("NoInstall: expected no probe, got %v", runner.Calls)
	}
	if len(res.Applied) != 1 {
		t.Errorf("NoInstall: expected the change still recorded as applied, got %+v", res.Applied)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/provider/shared/ -run TestCLIToolsApply_NoInstall -v`
Expected: FAIL — `Env` has no `NoInstall` field, so the test file does not compile (`unknown field NoInstall in struct literal`).

- [ ] **Step 3: Add the `NoInstall` field**

In `internal/provider/env.go`, change the `Env` struct (lines 33-40) to add the field below `DryRun`:

```go
// Env is the injected environment a provider observes and applies against.
type Env struct {
	FS     Filesystem
	Runner CommandRunner
	Fetch  fetch.Fetcher
	Home   string // Claude Code config root (e.g. the user's home directory)
	Root   string // the repo root the manifest was resolved from
	DryRun bool
	// NoInstall, when set, makes the cliTools provider skip package installs
	// and the declare-and-check probe while the file-writing channels still
	// reconcile. Unlike DryRun it does not suppress file writes.
	NoInstall bool
}
```

- [ ] **Step 4: Honour `NoInstall` in `CLITools.Apply`**

In `internal/provider/shared/clitools.go`, change the adapter-install guard (line 60) from `if !env.DryRun {` to:

```go
			if !env.DryRun && !env.NoInstall {
```

and the declare-and-check guard (line 83) from `if !env.DryRun {` to:

```go
		if !env.DryRun && !env.NoInstall {
```

Update the `Apply` doc comment (lines 30-32) — replace `Honors env.DryRun.` with `Honors env.DryRun and env.NoInstall (both skip the install and probe).`

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/provider/shared/ -run TestCLIToolsApply -v`
Expected: all PASS — the two new tests plus the existing `TestCLIToolsApply_CreateWithBrew`.

- [ ] **Step 6: Commit**

```bash
git add internal/provider/env.go internal/provider/shared/clitools.go internal/provider/shared/clitools_test.go
git commit -m "Add Env.NoInstall, honoured by the cliTools provider"
```

---

### Task 4: Add the `apply --no-install` flag

Wire a `--no-install` flag through `newApplyCommand` and `runApply`, setting `env.NoInstall`. The proof is an end-to-end test: a manifest with a CLI tool whose binary is absent aborts a plain `apply` (the `cliTools` channel runs first and fails the declare-and-check probe), but `apply --no-install` skips that and the later `commands` channel still writes its file.

**Files:**
- Modify: `cmd/ainfra/commands.go` (`newApplyCommand`, `runApply` — both edited in Task 2)
- Test: `cmd/ainfra/cmd_apply_test.go`

- [ ] **Step 1: Write the failing tests**

Add to `cmd/ainfra/cmd_apply_test.go`:

```go
func TestApplyNoInstall(t *testing.T) {
	dir := t.TempDir()

	srcContent := "# Hello command\n"
	if err := os.WriteFile(filepath.Join(dir, "hello.md"), []byte(srcContent), 0o644); err != nil {
		t.Fatal(err)
	}
	// A CLI tool whose binary is absent and whose only install method is
	// unrecognised. Without --no-install the cliTools channel (applied before
	// commands) fails the declare-and-check probe and aborts the apply.
	yaml := "version: 1\n" +
		"cliTools:\n" +
		"  ainfra-absent-tool-xyz:\n" +
		"    install:\n" +
		"      manual: {}\n" +
		"commands:\n" +
		"  hello:\n" +
		"    source: hello.md\n"
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	if code := run([]string{"--chdir", dir, "lock"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("lock failed")
	}

	var out, errOut bytes.Buffer
	code := run([]string{"--chdir", dir, "apply", "--yes", "--no-install"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("apply --yes --no-install: code=%d out=%q err=%q", code, out.String(), errOut.String())
	}

	// The file-writing channels still reconcile.
	dest := filepath.Join(dir, ".claude", "commands", "hello.md")
	if _, err := os.Stat(dest); err != nil {
		t.Errorf("apply --no-install: expected %s to be written, got: %v", dest, err)
	}
}

func TestApplyWithoutNoInstallFailsOnAbsentTool(t *testing.T) {
	dir := t.TempDir()

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
		t.Fatalf("apply --yes (no --no-install): expected non-zero exit for an absent tool, got 0; out=%q err=%q",
			out.String(), errOut.String())
	}
}
```

- [ ] **Step 2: Run the tests to verify the first fails**

Run: `go test ./cmd/ainfra/ -run 'TestApplyNoInstall|TestApplyWithoutNoInstall' -v`
Expected: `TestApplyNoInstall` FAILS — `--no-install` is not defined, so `flag.Parse` errors and `run` exits non-zero. `TestApplyWithoutNoInstallFailsOnAbsentTool` PASSES (it already documents current behaviour).

- [ ] **Step 3: Implement the flag**

In `cmd/ainfra/commands.go`, update `newApplyCommand` to add the third flag:

```go
func newApplyCommand() *cli.Command {
	var yes, dryRun, noInstall bool
	return &cli.Command{
		Name:      "apply",
		Summary:   "Reconcile the environment to match the manifest",
		UsageLine: "ainfra apply [--yes] [--dry-run] [--no-install]",
		Example:   "ainfra apply --dry-run",
		SetFlags: func(fs *flag.FlagSet) {
			fs.BoolVar(&yes, "yes", false, "skip confirmation prompt")
			fs.BoolVar(&dryRun, "dry-run", false, "preview the apply without writing anything")
			fs.BoolVar(&noInstall, "no-install", false, "reconcile config files but skip CLI-tool installs")
		},
		Run: func(ctx cli.Context) int {
			return runApply(ctx, yes, dryRun, noInstall)
		},
	}
}
```

Change the `runApply` signature from `func runApply(ctx cli.Context, yes, dryRun bool) int {` to:

```go
func runApply(ctx cli.Context, yes, dryRun, noInstall bool) int {
```

In `runApply`, extend the env-construction block added in Task 2 (currently `env := buildEnv(dir)` / `env.DryRun = dryRun`) to also set `NoInstall`:

```go
	env := buildEnv(dir)
	env.DryRun = dryRun
	env.NoInstall = noInstall
	orch := provider.NewOrchestrator(dir, env, providers)
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./cmd/ainfra/ -run 'TestApplyNoInstall|TestApplyWithoutNoInstall' -v`
Expected: both PASS.

- [ ] **Step 5: Run the whole suite**

Run: `go test ./...`
Expected: all packages PASS.

- [ ] **Step 6: Commit**

```bash
git add cmd/ainfra/commands.go cmd/ainfra/cmd_apply_test.go
git commit -m "Add apply --no-install flag"
```

---

## Self-review notes

- **Spec coverage.** Item 5 of the hardening spec asks for `--dry-run` and
  `--no-install`; both are delivered (Tasks 2 and 4) on the supporting
  primitives in Tasks 1 and 3. `--home` / `AINFRA_HOME` is deliberately not
  built — see the spec's item 5 for the verified rationale (`env.Home` is read
  by no provider; `--chdir` is the existing sandbox lever).
- **Type consistency.** `runApply` is defined once with the final signature
  `runApply(ctx cli.Context, yes, dryRun, noInstall bool) int`; Task 2
  introduces it with two trailing bools and Task 4 adds the third — execute the
  tasks in order. `Env.NoInstall` is the single field name used in `env.go`,
  `clitools.go`, and `commands.go`.
- **No dependency on the cliTools payload bug.** `internal/provider/shared/clitools.go:49`
  asserts `Payload["install"].(map[string]any)` while `resolve.RenderResources`
  supplies `map[string]map[string]any` — a real latent bug (the install map is
  always nil through the rendered apply path, so every cliTool falls to
  declare-and-check). The Task 4 fixture uses `manual: {}` (an unrecognised
  method) so its declare-and-check behaviour holds whether or not that bug is
  later fixed. The bug itself is out of scope here; it belongs with hardening
  items 1-2.
