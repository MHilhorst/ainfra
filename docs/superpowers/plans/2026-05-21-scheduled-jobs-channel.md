# Scheduled Jobs Channel Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the `scheduledJobs` channel — `ainfra`'s ninth, a targeted-infrastructure channel for cron-style jobs — through `ainfra lock`.

**Architecture:** Mirrors the Iteration-3 hooks/commands channels exactly. A new `manifest.ScheduledJob` type plus a top-level `targets` vocabulary and a `host` block; validation enforces non-empty fields and that every `runsOn` / `host.targets` label is in the merged vocabulary; the pipeline resolves jobs into lockfile entries (content hash + recorded `runsOn`) and wires their `requires` edges into the dependency graph. The lockfile stays machine-agnostic — `runsOn` filtering and crontab generation are `apply`-time and out of scope.

**Tech Stack:** Go 1.25, `gopkg.in/yaml.v3`, `go test`.

**Spec:** `docs/superpowers/specs/2026-05-21-scheduled-jobs-design.md`.

---

## File Structure

| File | Change |
|------|--------|
| `internal/manifest/types.go` | Add `ScheduledJob` + `Host` types; add `Targets`, `ScheduledJobs`, `Host` to `Manifest`. |
| `internal/manifest/validate.go` | Validate scheduled jobs and host targets against the vocabulary. |
| `internal/lockfile/types.go` | Add `Entries.ScheduledJobs`; add `Entry.RunsOn`. |
| `internal/lockfile/io.go` | Initialise the `ScheduledJobs` map in `ensureMaps`. |
| `internal/resolve/pipeline.go` | Merge the vocabulary; resolve scheduled jobs into the lock; route them in `splitByLayer`. |
| `examples/multi-database/ainfra.yaml` + `ainfra.lock` | A worked scheduled job. |
| `spec/manifest-schema.md`, `spec/lockfile-schema.md`, `docs/design.md`, `docs/assessment-vs-real-config.md` | Doc updates. |

---

## Task 1: Manifest types

**Files:**
- Modify: `internal/manifest/types.go`
- Test: `internal/manifest/types_test.go`

- [ ] **Step 1: Write the failing test** — append to `internal/manifest/types_test.go`:

```go
func TestManifestUnmarshalsScheduledJobsAndHost(t *testing.T) {
	src := []byte(`
version: 1
targets: [hub, laptop]
host:
  targets: [hub]
scheduledJobs:
  nightly-health:
    schedule: "0 6 * * *"
    command: claude -p "check replication lag"
    runsOn: [hub]
    requires: [ { cliTool: mysql-client } ]
`)
	var m Manifest
	if err := yaml.Unmarshal(src, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(m.Targets) != 2 || m.Targets[0] != "hub" {
		t.Errorf("targets vocabulary not parsed: %v", m.Targets)
	}
	if len(m.Host.Targets) != 1 || m.Host.Targets[0] != "hub" {
		t.Errorf("host.targets not parsed: %v", m.Host.Targets)
	}
	j := m.ScheduledJobs["nightly-health"]
	if j.Schedule != "0 6 * * *" || j.Command == "" {
		t.Errorf("job not parsed: %+v", j)
	}
	if len(j.RunsOn) != 1 || j.RunsOn[0] != "hub" {
		t.Errorf("runsOn not parsed: %v", j.RunsOn)
	}
	if len(j.Requires) != 1 || j.Requires[0].CLITool != "mysql-client" {
		t.Errorf("requires not parsed: %v", j.Requires)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/manifest/ -run TestManifestUnmarshalsScheduledJobs`
Expected: FAIL — `m.Targets undefined` / `unknown field`.

- [ ] **Step 3: Add the `Manifest` fields** — in `internal/manifest/types.go`, change the `Manifest` struct's tail from:

```go
	MCPServers         map[string]MCPServer         `yaml:"mcpServers"`
	Hooks              map[string]Hook              `yaml:"hooks"`
	Commands           map[string]Command           `yaml:"commands"`
}
```

to:

```go
	MCPServers         map[string]MCPServer         `yaml:"mcpServers"`
	Hooks              map[string]Hook              `yaml:"hooks"`
	Commands           map[string]Command           `yaml:"commands"`
	ScheduledJobs      map[string]ScheduledJob      `yaml:"scheduledJobs"`
	Targets            []string                     `yaml:"targets"`
	Host               Host                         `yaml:"host"`
}
```

- [ ] **Step 4: Add the new types** — in `internal/manifest/types.go`, immediately before the `// Require is one dependency-graph edge` comment, add:

```go
// ScheduledJob is a cron-style recurring command (spec §13). It is
// targeted-infrastructure: it runs only on machines whose targets intersect
// its RunsOn, not on every developer's machine.
type ScheduledJob struct {
	Schedule    string    `yaml:"schedule"`
	Command     string    `yaml:"command"`
	Source      string    `yaml:"source"`
	RunsOn      []string  `yaml:"runsOn"`
	Description string    `yaml:"description"`
	Requires    []Require `yaml:"requires"`
	Enabled     *bool     `yaml:"enabled"`
	Overridable bool      `yaml:"overridable"`
}

// Host declares which target labels the current machine carries (spec §13).
// It lives in the personal layer; the AINFRA_TARGETS env var can override it
// for ephemeral machines. Consumed at apply time, not at lock time.
type Host struct {
	Targets []string `yaml:"targets"`
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/manifest/ -run TestManifestUnmarshalsScheduledJobs`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/manifest/types.go internal/manifest/types_test.go
git commit -m "Add ScheduledJob, Host, and targets vocabulary to the manifest schema"
```

---

## Task 2: Validation

**Files:**
- Modify: `internal/manifest/validate.go`
- Test: `internal/manifest/validate_test.go`

- [ ] **Step 1: Write the failing tests** — append to `internal/manifest/validate_test.go`:

```go
func TestValidateRejectsScheduledJobWithoutSchedule(t *testing.T) {
	m := &Manifest{Version: 1, Targets: []string{"hub"},
		ScheduledJobs: map[string]ScheduledJob{
			"j": {Command: "echo x", RunsOn: []string{"hub"}},
		}}
	err := Validate(m)
	if err == nil || !strings.Contains(err.Error(), "schedule") {
		t.Fatalf("want schedule error, got %v", err)
	}
}

func TestValidateRejectsScheduledJobWithoutRunsOn(t *testing.T) {
	m := &Manifest{Version: 1, Targets: []string{"hub"},
		ScheduledJobs: map[string]ScheduledJob{
			"j": {Schedule: "0 6 * * *", Command: "echo x"},
		}}
	err := Validate(m)
	if err == nil || !strings.Contains(err.Error(), "runsOn") {
		t.Fatalf("want runsOn error, got %v", err)
	}
}

func TestValidateRejectsRunsOnOutsideVocabulary(t *testing.T) {
	m := &Manifest{Version: 1, Targets: []string{"hub"},
		ScheduledJobs: map[string]ScheduledJob{
			"j": {Schedule: "0 6 * * *", Command: "echo x", RunsOn: []string{"mars"}},
		}}
	err := Validate(m)
	if err == nil || !strings.Contains(err.Error(), "vocabulary") {
		t.Fatalf("want vocabulary error, got %v", err)
	}
}

func TestValidateRejectsHostTargetOutsideVocabulary(t *testing.T) {
	m := &Manifest{Version: 1, Targets: []string{"hub"},
		Host: Host{Targets: []string{"mars"}}}
	err := Validate(m)
	if err == nil || !strings.Contains(err.Error(), "vocabulary") {
		t.Fatalf("want vocabulary error, got %v", err)
	}
}

func TestValidateAcceptsValidScheduledJob(t *testing.T) {
	m := &Manifest{Version: 1, Targets: []string{"hub", "laptop"},
		Host: Host{Targets: []string{"hub"}},
		ScheduledJobs: map[string]ScheduledJob{
			"j": {Schedule: "0 6 * * *", Command: "echo x", RunsOn: []string{"hub"}},
		}}
	if err := Validate(m); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/manifest/ -run TestValidate`
Expected: the four new `TestValidate*ScheduledJob` / `*RunsOn` / `*HostTarget` tests FAIL (no validation yet — `Validate` returns nil).

- [ ] **Step 3: Add the validation** — in `internal/manifest/validate.go`, replace the final `commands` loop and `return nil`:

```go
	for _, id := range slices.Sorted(maps.Keys(m.Commands)) {
		if m.Commands[id].Source == "" {
			return fmt.Errorf("commands.%s: a command must declare a source", id)
		}
	}
	return nil
}
```

with:

```go
	for _, id := range slices.Sorted(maps.Keys(m.Commands)) {
		if m.Commands[id].Source == "" {
			return fmt.Errorf("commands.%s: a command must declare a source", id)
		}
	}
	vocabulary := map[string]bool{}
	for _, t := range m.Targets {
		vocabulary[t] = true
	}
	for _, id := range slices.Sorted(maps.Keys(m.ScheduledJobs)) {
		j := m.ScheduledJobs[id]
		if j.Schedule == "" {
			return fmt.Errorf("scheduledJobs.%s: a scheduled job must declare a schedule", id)
		}
		if j.Command == "" {
			return fmt.Errorf("scheduledJobs.%s: a scheduled job must declare a command", id)
		}
		if len(j.RunsOn) == 0 {
			return fmt.Errorf("scheduledJobs.%s: a scheduled job must declare runsOn", id)
		}
		for _, t := range j.RunsOn {
			if !vocabulary[t] {
				return fmt.Errorf("scheduledJobs.%s: runsOn target %q is not in the declared targets vocabulary", id, t)
			}
		}
	}
	for _, t := range m.Host.Targets {
		if !vocabulary[t] {
			return fmt.Errorf("host.targets: %q is not in the declared targets vocabulary", t)
		}
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/manifest/ -run TestValidate`
Expected: PASS (all `TestValidate*`).

- [ ] **Step 5: Commit**

```bash
git add internal/manifest/validate.go internal/manifest/validate_test.go
git commit -m "Validate scheduled jobs and host targets against the vocabulary"
```

---

## Task 3: Lockfile types and I/O

**Files:**
- Modify: `internal/lockfile/types.go`, `internal/lockfile/io.go`
- Test: `internal/lockfile/io_test.go`

- [ ] **Step 1: Write the failing test** — append to `internal/lockfile/io_test.go`:

```go
func TestWriteThenReadRoundTripsScheduledJobs(t *testing.T) {
	dir := t.TempDir()
	lock := &Lock{Version: 1, Entries: Entries{
		ScheduledJobs: map[string]Entry{
			"nightly": {Layer: "repo", RunsOn: []string{"hub"}, ContentHash: "sha256:xyz"},
		},
	}}
	path := filepath.Join(dir, "ainfra.lock")
	if err := Write(path, lock); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	e := got.Entries.ScheduledJobs["nightly"]
	if e.ContentHash != "sha256:xyz" || len(e.RunsOn) != 1 || e.RunsOn[0] != "hub" {
		t.Errorf("round-trip lost scheduled job data: %+v", e)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/lockfile/ -run TestWriteThenReadRoundTripsScheduledJobs`
Expected: FAIL — `Entries has no field ScheduledJobs` / `Entry has no field RunsOn`.

- [ ] **Step 3: Add the lockfile fields** — in `internal/lockfile/types.go`, change `Entries`:

```go
// Entries groups lock entries by channel.
type Entries struct {
	MCPServers         map[string]Entry `yaml:"mcpServers"`
	BackgroundServices map[string]Entry `yaml:"backgroundServices"`
	Hooks              map[string]Entry `yaml:"hooks"`
	Commands           map[string]Entry `yaml:"commands"`
	ScheduledJobs      map[string]Entry `yaml:"scheduledJobs"`
	CLITools           map[string]Entry `yaml:"cliTools"`
}
```

and add a `RunsOn` field to `Entry` (after `ResolvedVersion`, before `ContentHash`):

```go
	ResolvedVersion string         `yaml:"resolvedVersion,omitempty"`
	RunsOn          []string       `yaml:"runsOn,omitempty"`
	ContentHash     string         `yaml:"contentHash"`
```

- [ ] **Step 4: Initialise the new map** — in `internal/lockfile/io.go`, in `ensureMaps`, add after the `Commands` block:

```go
	if l.Entries.ScheduledJobs == nil {
		l.Entries.ScheduledJobs = map[string]Entry{}
	}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/lockfile/`
Expected: PASS (all lockfile tests).

- [ ] **Step 6: Commit**

```bash
git add internal/lockfile/types.go internal/lockfile/io.go internal/lockfile/io_test.go
git commit -m "Add scheduledJobs entries and runsOn to the lockfile"
```

---

## Task 4: Pipeline resolution

**Files:**
- Modify: `internal/resolve/pipeline.go`
- Test: `internal/resolve/pipeline_test.go`

- [ ] **Step 1: Write the failing test** — append to `internal/resolve/pipeline_test.go`:

```go
func TestLockPipelineResolvesScheduledJobs(t *testing.T) {
	dir := t.TempDir()
	manifestYAML := `version: 1
targets: [hub, laptop]
cliTools:
  claude: { versionConstraint: ">=1" }
scheduledJobs:
  nightly-health:
    schedule: "0 6 * * *"
    command: claude -p "check replication lag"
    runsOn: [hub]
    requires: [ { cliTool: claude } ]
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
	for _, want := range []string{"scheduledJobs:", "nightly-health", "runsOn:", "hub", "contentHash:"} {
		if !strings.Contains(out, want) {
			t.Errorf("lock missing %q\n---\n%s", want, out)
		}
	}
}

func TestLockPipelineRejectsRunsOnOutsideVocabulary(t *testing.T) {
	dir := t.TempDir()
	manifestYAML := `version: 1
targets: [hub]
scheduledJobs:
  bad:
    schedule: "0 6 * * *"
    command: echo x
    runsOn: [mars]
`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(manifestYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RunLock(dir); err == nil {
		t.Fatal("want validation error for runsOn outside the vocabulary")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/resolve/ -run TestLockPipelineResolvesScheduledJobs`
Expected: FAIL — the lock has no `scheduledJobs:` section.

- [ ] **Step 3: Merge the vocabulary for validation** — in `internal/resolve/pipeline.go`, replace this block:

```go
	// Build a merged template map so lower layers can reference templates from
	// higher layers (e.g. personal layer reusing a repo-layer template).
	allTemplates := map[string]manifest.Template{}
	for _, layerName := range []manifest.Layer{manifest.LayerTeam, manifest.LayerRepo, manifest.LayerPersonal} {
		if m, ok := layers[layerName]; ok {
			for name, tmpl := range m.Templates {
				if _, exists := allTemplates[name]; !exists {
					allTemplates[name] = tmpl
				}
			}
		}
	}

	// Validate each layer. For layers that reference cross-layer templates,
	// inject the merged template map so the existence check passes.
	for _, m := range layers {
		toValidate := m
		if len(m.Templates) < len(allTemplates) {
			// Shallow copy with merged templates so cross-layer refs validate.
			copied := *m
			copied.Templates = allTemplates
			toValidate = &copied
		}
		if err := manifest.Validate(toValidate); err != nil {
			return err
		}
	}
```

with:

```go
	// Merge templates and the targets vocabulary across layers, so a lower
	// layer can reference a template or a target label declared higher up.
	allTemplates := map[string]manifest.Template{}
	targetSet := map[string]bool{}
	for _, layerName := range []manifest.Layer{manifest.LayerTeam, manifest.LayerRepo, manifest.LayerPersonal} {
		if m, ok := layers[layerName]; ok {
			for name, tmpl := range m.Templates {
				if _, exists := allTemplates[name]; !exists {
					allTemplates[name] = tmpl
				}
			}
			for _, t := range m.Targets {
				targetSet[t] = true
			}
		}
	}
	allTargets := slices.Sorted(maps.Keys(targetSet))

	// Validate each layer against the merged templates and vocabulary.
	for _, m := range layers {
		copied := *m
		copied.Templates = allTemplates
		copied.Targets = allTargets
		if err := manifest.Validate(&copied); err != nil {
			return err
		}
	}
```

- [ ] **Step 4: Initialise the `ScheduledJobs` lock map** — in `internal/resolve/pipeline.go`, in the `lock := &lockfile.Lock{...}` literal, change the `Entries` block from:

```go
		Entries: lockfile.Entries{
			MCPServers:         map[string]lockfile.Entry{},
			BackgroundServices: map[string]lockfile.Entry{},
			Hooks:              map[string]lockfile.Entry{},
			Commands:           map[string]lockfile.Entry{},
			CLITools:           map[string]lockfile.Entry{},
		}}
```

to:

```go
		Entries: lockfile.Entries{
			MCPServers:         map[string]lockfile.Entry{},
			BackgroundServices: map[string]lockfile.Entry{},
			Hooks:              map[string]lockfile.Entry{},
			Commands:           map[string]lockfile.Entry{},
			ScheduledJobs:      map[string]lockfile.Entry{},
			CLITools:           map[string]lockfile.Entry{},
		}}
```

- [ ] **Step 5: Resolve scheduled jobs** — in `internal/resolve/pipeline.go`, in the layer loop that already handles `m.Hooks` and `m.Commands`, add a third inner loop after the `m.Commands` loop, before that outer `for` loop's closing brace:

```go
		for _, id := range slices.Sorted(maps.Keys(m.ScheduledJobs)) {
			j := m.ScheduledJobs[id]
			node := "job:" + id
			g.AddNode(node)
			addRequireEdges(g, node, j.Requires)
			lock.Entries.ScheduledJobs[id] = lockfile.Entry{
				Layer:  string(layerName),
				RunsOn: j.RunsOn,
				ContentHash: lockfile.ContentHash(map[string]any{
					"schedule": j.Schedule, "command": j.Command, "source": j.Source,
					"runsOn": j.RunsOn, "description": j.Description,
				}),
			}
		}
```

- [ ] **Step 6: Route scheduled jobs in `splitByLayer`** — in `internal/resolve/pipeline.go`, in `splitByLayer`, change the `mk` closure's `Entries` literal to include `ScheduledJobs`:

```go
	mk := func() *lockfile.Lock {
		return &lockfile.Lock{Version: 1, GeneratedAt: l.GeneratedAt, Entries: lockfile.Entries{
			MCPServers: map[string]lockfile.Entry{}, BackgroundServices: map[string]lockfile.Entry{},
			Hooks: map[string]lockfile.Entry{}, Commands: map[string]lockfile.Entry{},
			ScheduledJobs: map[string]lockfile.Entry{},
			CLITools:      map[string]lockfile.Entry{}}}
	}
```

and add a `route` call after the `Commands` one:

```go
	route(func(x *lockfile.Lock) map[string]lockfile.Entry { return x.Entries.Commands }, l.Entries.Commands)
	route(func(x *lockfile.Lock) map[string]lockfile.Entry { return x.Entries.ScheduledJobs }, l.Entries.ScheduledJobs)
	return committed, personal
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `go test ./internal/resolve/ -run TestLockPipeline`
Expected: PASS (existing pipeline tests plus the two new ones).

- [ ] **Step 8: Run the full suite + build**

Run: `go build ./... && go vet ./... && gofmt -l internal/ cmd/ && go test ./... -count=1`
Expected: build/vet succeed; `gofmt` prints nothing; all packages PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/resolve/pipeline.go internal/resolve/pipeline_test.go
git commit -m "Resolve scheduled jobs through the lock pipeline"
```

---

## Task 5: Example and documentation

**Files:**
- Modify: `examples/multi-database/ainfra.yaml`, `examples/multi-database/ainfra.lock`
- Modify: `spec/manifest-schema.md`, `spec/lockfile-schema.md`, `docs/design.md`, `docs/assessment-vs-real-config.md`

- [ ] **Step 1: Add a scheduled job to the example** — in `examples/multi-database/ainfra.yaml`, append at end of file:

```yaml

# --- Targets: the governed vocabulary of machine roles -----------------------
targets: [hub, laptop]

# --- Scheduled jobs: targeted infrastructure, run on designated machines -----
scheduledJobs:
  nightly-replication-check:
    schedule: "0 6 * * *"
    command: claude -p "Report overnight replication lag across the four databases."
    runsOn: [hub]
    description: Nightly read-only replication-lag check; runs on the hub only.
    requires:
      - cliTool: mysql-client
```

- [ ] **Step 2: Regenerate the example lock**

Run: `cd examples/multi-database && go run ../../cmd/ainfra lock && rm -f ainfra.personal.lock && cd ../..`
Expected: prints `ainfra: wrote ainfra.lock and ainfra.personal.lock`. Confirm `examples/multi-database/ainfra.lock` now contains a `scheduledJobs:` section with `nightly-replication-check`, `runsOn:` / `hub`, and a `contentHash`.

- [ ] **Step 3: Update the manifest spec** — in `spec/manifest-schema.md`, in the §2 top-level structure block add after the `commands:` line:

```yaml
scheduledJobs:        {}      # channel 9 — cron-style targeted jobs (§13)
targets:              []      # the governed target vocabulary (§13)
host:                 {}      # this machine's target labels (§13)
```

Then append a new section at the end of the file:

```markdown
---

## 13. Channel 9 — Scheduled jobs

> Added by Iteration 4. A *targeted-infrastructure* channel — deliberately
> distinct from channels 1–8, which are per-developer environment. A scheduled
> job runs on designated machines only, never reproduced everywhere. See
> `docs/superpowers/specs/2026-05-21-scheduled-jobs-design.md`.

The team layer declares a governed vocabulary of target labels:

```yaml
targets: [hub, laptop, ci]    # open to extend, but team-agreed
```

A scheduled job declares a cron `schedule`, a `command`, and the targets it
`runsOn` (required — there is no "runs everywhere" default for infrastructure):

```yaml
scheduledJobs:
  flare-triage:
    schedule: "0 */4 * * *"
    command: claude -p "$(cat prompts/flare-triage.md)"
    source: ./scripts/flare-triage.sh   # optional bundled script
    runsOn: [hub]                       # every label must be in `targets`
    description: Triage new Flare errors.
    requires:
      - cliTool: claude
```

A machine declares which targets it carries, in its personal layer:

```yaml
host:
  targets: [hub]
```

`ainfra` is local and registry-less: it labels a machine and *trusts* the team
to label exactly one `hub`. It cannot detect a second accidental `hub`. A
targeted job therefore trades uniform reproduction for **label-and-trust**;
`check` gives the local guarantee only. The crontab generation and the
`runsOn`-vs-machine filtering happen at `apply` time.
```

- [ ] **Step 4: Update the lockfile spec** — in `spec/lockfile-schema.md`, in the `entries:` structure block add `scheduledJobs:` after `commands:`:

```yaml
  commands:          { <id>: <entry> }
  scheduledJobs:     { <id>: <entry> }
```

and append this sentence to the paragraph that describes the `hooks`/`commands`
entries:

```markdown

`scheduledJobs` entries additionally record `runsOn` (the job's target labels)
so the deferred `apply` step can filter per machine. The lockfile itself stays
machine-agnostic — it records every job regardless of the machine.
```

- [ ] **Step 5: Update the design doc** — in `docs/design.md` §1, change "eight channels" to "nine channels", the "Eight configurable channels:" line to "Nine configurable channels:", and add a list item:

```markdown
9. **Scheduled jobs** — cron-style targeted infrastructure (a *distinct* kind of
   channel: run on designated machines, not reproduced everywhere)
```

Then change the Iteration-3 note to also mention Iteration 4:

```markdown
> Channels 7–8 were added in Iteration 3 and channel 9 in Iteration 4, after
> assessing the schema against a real team config repo. Channel 9 is
> deliberately framed as targeted infrastructure, not a per-developer channel.
> See [assessment-vs-real-config.md](assessment-vs-real-config.md).
```

- [ ] **Step 6: Close the gap in the assessment doc** — in `docs/assessment-vs-real-config.md`, in the §2 coverage-map table, change the cron row from:

```markdown
| **5 cron jobs** | — | **Gap — open** (see §5) |
```

to:

```markdown
| **5 cron jobs** | **`scheduledJobs` (NEW — Iteration 4)** | **Clean — channel added** (targeted-infrastructure) |
```

and in §5, remove item 1 (scheduled jobs) and renumber the remaining items; add one sentence at the top of §5: "Iteration 4 closed the scheduled-jobs gap with a `scheduledJobs` channel; the items below remain."

- [ ] **Step 7: Verify build and tests still pass**

Run: `go build ./... && go test ./... -count=1`
Expected: build succeeds; all packages PASS.

- [ ] **Step 8: Commit**

```bash
git add examples/ spec/ docs/design.md docs/assessment-vs-real-config.md
git commit -m "Add scheduled jobs to the example and update specs and docs"
```

---

## Self-Review

**Spec coverage:** §1 framing → design.md update (Task 5). §2 label-and-trust → spec §13 text (Task 5). §3 schema (`targets`, `scheduledJobs`, `host`) → Tasks 1, 5. §4 mechanism → spec §13 (Task 5; crontab generation itself is `apply`-time, out of scope per §8). §5 lockfile representation → Task 3 (`RunsOn` field), Task 4 (pipeline records it). §6 validation → Task 2. §7 dependency graph → Task 4 (`addRequireEdges`, `job:` prefix). §8 scope → the whole plan stops at `ainfra lock`. §9 non-goals → nothing in the plan implements remote execution, uniqueness enforcement, cron-syntax validation, or supervision.

**Placeholder scan:** every code step contains complete Go or exact YAML/markdown. No `TODO`, no "similar to Task N".

**Type consistency:** `manifest.ScheduledJob` / `manifest.Host` / `Manifest.Targets` (Task 1) are used verbatim in Tasks 2 and 4. `lockfile.Entries.ScheduledJobs` and `lockfile.Entry.RunsOn` (Task 3) are written by Task 4's pipeline code. The graph node prefix `job:` and the `addRequireEdges` helper match the existing pipeline. `slices`/`maps` are already imported in both `validate.go` and `pipeline.go` from Iteration 3.
