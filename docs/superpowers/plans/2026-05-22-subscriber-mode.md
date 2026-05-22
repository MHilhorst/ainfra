# Subscriber Mode Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let non-engineers run a team's MCP servers in the Claude Desktop app via a published, hash-pinned artifact their machine silently subscribes to.

**Architecture:** Add `claude-desktop` as a third render target (MCP-only). `ainfra publish` packages the resolved lockfile into an artifact with a subscription descriptor. `apply --from <url>` reconciles a machine against that artifact with no repo and no manifest. `ainfra installer` emits a one-time installer that drops a launchd job running `apply --from` on a schedule.

**Tech Stack:** Go, standard library (`net/http`, `crypto/sha256`, `encoding/json`), existing ainfra packages (`provider`, `agent`, `fsmerge`, `lockfile`, `manifest`, `schema`, `cli`).

Spec: `docs/superpowers/specs/2026-05-22-subscriber-mode-design.md`.

---

### Task 1: Register the `claude-desktop` agent

**Files:**
- Modify: `internal/agent/agent.go`
- Test: `internal/agent/agent_test.go`

- [ ] **Step 1: Write failing tests**

Add to `internal/agent/agent_test.go` (create the file if absent, `package agent`):

```go
func TestClaudeDesktopKnownAndMCPOnly(t *testing.T) {
	if !Known("claude-desktop") {
		t.Fatal("claude-desktop should be a known agent")
	}
	if !Supports(ClaudeDesktop, ChannelMCPServers) {
		t.Error("claude-desktop must support mcpServers")
	}
	for _, ch := range []string{ChannelHooks, ChannelCommands, ChannelRules, ChannelSkills, ChannelPlugins, ChannelTools, ChannelCLITools} {
		if Supports(ClaudeDesktop, ch) {
			t.Errorf("claude-desktop must not support %q", ch)
		}
	}
}
```

- [ ] **Step 2: Run test, verify it fails**

Run: `go test ./internal/agent/ -run TestClaudeDesktop -v`
Expected: FAIL — `undefined: ClaudeDesktop`.

- [ ] **Step 3: Implement**

In `internal/agent/agent.go`, add the constant alongside `ClaudeCode`/`Codex`:

```go
	ClaudeDesktop ID = "claude-desktop"
```

Add to the `capabilities` map:

```go
	ClaudeDesktop: {
		ChannelMCPServers: true,
	},
```

- [ ] **Step 4: Run test, verify it passes**

Run: `go test ./internal/agent/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/agent/
git commit -m "Register claude-desktop agent: MCP-only render target"
```

---

### Task 2: The `claudedesktop.MCP` provider

**Files:**
- Create: `internal/provider/claudedesktop/mcp.go`
- Test: `internal/provider/claudedesktop/mcp_test.go`

The provider mirrors `internal/provider/codex/mcp.go` but renders JSON into the
Claude Desktop config file. The server-object shape is identical to
`internal/provider/claudecode/mcp.go`'s `buildMCPServerObject`.

- [ ] **Step 1: Write failing tests**

Create `internal/provider/claudedesktop/mcp_test.go`. Tests must cover: a create
renders into `mcpServers`; a foreign server already in the file survives an
apply; `Observe` of a missing file returns no resources. Use the in-memory FS
fake the codex tests use (`internal/provider/codex/mcp_test.go` shows the
pattern — `provider.Env{FS: ...}`). Key assertions:

```go
func TestApplyCreatesServerAndPreservesForeign(t *testing.T) {
	// Seed config with a foreign server, apply one ainfra-owned server,
	// assert both keys exist in mcpServers afterwards.
}

func TestObserveMissingFileNoResources(t *testing.T) {
	// Empty FS -> Observe returns nil, nil.
}

func TestConfigPathPerOS(t *testing.T) {
	// configPath uses Application Support on darwin, APPDATA on windows.
}
```

- [ ] **Step 2: Run test, verify it fails**

Run: `go test ./internal/provider/claudedesktop/ -v`
Expected: FAIL — package/type undefined.

- [ ] **Step 3: Implement**

Create `internal/provider/claudedesktop/mcp.go`:

```go
// Package claudedesktop contains the Claude Desktop app channel providers.
// Claude Desktop reads a single JSON config file; ainfra reconciles only its
// mcpServers object. See docs/superpowers/specs/2026-05-22-subscriber-mode-design.md.
package claudedesktop

import (
	"encoding/json"
	"errors"
	iofs "io/fs"
	"path/filepath"
	"runtime"

	"github.com/MHilhorst/ainfra/internal/provider"
	"github.com/MHilhorst/ainfra/internal/provider/fsmerge"
)

// MCP reconciles entries in claude_desktop_config.json under "mcpServers".
type MCP struct{}

// Channel returns the channel name this provider manages.
func (MCP) Channel() string { return "mcpServers" }

// configPath returns the OS-specific Claude Desktop config file path.
func configPath(env provider.Env) string {
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(env.Home, "AppData", "Roaming", "Claude", "claude_desktop_config.json")
	default: // darwin and anything else
		return filepath.Join(env.Home, "Library", "Application Support", "Claude", "claude_desktop_config.json")
	}
}

// Observe reads the config file and returns a Resource per mcpServers key.
// A missing file is treated as no resources.
func (MCP) Observe(env provider.Env) ([]provider.Resource, error) {
	raw, err := env.FS.ReadFile(configPath(env))
	if errors.Is(err, iofs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	servers, ok := doc["mcpServers"].(map[string]any)
	if !ok {
		return nil, nil
	}
	resources := make([]provider.Resource, 0, len(servers))
	for key := range servers {
		resources = append(resources, provider.Resource{ID: key, Channel: "mcpServers"})
	}
	return resources, nil
}

// Apply executes the channel plan against the config file. When env.DryRun is
// true the result is computed but the file is not written.
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
			desired[c.ID] = buildServerObject(c.Resource.Payload)
		}
	}
	if len(ownedKeys) == 0 {
		return provider.ApplyResult{Channel: "mcpServers"}, nil
	}
	if !env.DryRun {
		if err := fsmerge.MergeJSONKeys(env.FS, configPath(env), "mcpServers", desired, ownedKeys); err != nil {
			return provider.ApplyResult{}, err
		}
	}
	return provider.ApplyResult{Channel: "mcpServers", Applied: applied}, nil
}

// buildServerObject constructs a Claude Desktop mcpServers entry from a
// resource payload. Optional fields absent from the payload are omitted.
func buildServerObject(payload map[string]any) map[string]any {
	obj := map[string]any{}
	if v, ok := payload["command"]; ok && v != nil && v != "" {
		obj["command"] = v
	}
	if v, ok := payload["args"]; ok && v != nil {
		obj["args"] = v
	}
	if v, ok := payload["env"]; ok && v != nil {
		obj["env"] = v
	}
	if v, ok := payload["transport"]; ok && v != nil && v != "" {
		obj["type"] = v
	}
	if v, ok := payload["url"]; ok && v != nil && v != "" {
		obj["url"] = v
	}
	if v, ok := payload["headers"]; ok && v != nil {
		obj["headers"] = v
	}
	return obj
}
```

- [ ] **Step 4: Run test, verify it passes**

Run: `go test ./internal/provider/claudedesktop/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/provider/claudedesktop/
git commit -m "Add claudedesktop.MCP provider rendering claude_desktop_config.json"
```

---

### Task 3: Wire `claude-desktop` into the agent set

**Files:**
- Modify: `internal/provider/agentset/agentset.go`
- Test: `internal/provider/agentset/agentset_test.go`

- [ ] **Step 1: Write failing test**

Add to `agentset_test.go` (create if absent, `package agentset`):

```go
func TestForAgentClaudeDesktop(t *testing.T) {
	ps, err := ForAgent(agent.ClaudeDesktop)
	if err != nil {
		t.Fatalf("ForAgent(claude-desktop): %v", err)
	}
	var hasMCP bool
	for _, p := range ps {
		if p.Channel() == "mcpServers" {
			hasMCP = true
		}
	}
	if !hasMCP {
		t.Error("claude-desktop provider set must include the mcpServers provider")
	}
}
```

- [ ] **Step 2: Run test, verify it fails**

Run: `go test ./internal/provider/agentset/ -run ClaudeDesktop -v`
Expected: FAIL — `ForAgent` returns the "no provider set" error.

- [ ] **Step 3: Implement**

In `internal/provider/agentset/agentset.go`, add the import
`"github.com/MHilhorst/ainfra/internal/provider/claudedesktop"` and a case to
the `switch` in `ForAgent`:

```go
	case agent.ClaudeDesktop:
		return append([]provider.Provider{
			claudedesktop.MCP{},
		}, sharedProviders()...), nil
```

- [ ] **Step 4: Run test, verify it passes**

Run: `go test ./internal/provider/agentset/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/provider/agentset/
git commit -m "Wire claude-desktop into the agent provider set"
```

---

### Task 4: The `publish:` manifest block

**Files:**
- Modify: `internal/manifest/types.go`
- Modify: `internal/manifest/validate.go`
- Modify: `internal/schema/schema.go`
- Test: `internal/manifest/validate_test.go`

- [ ] **Step 1: Write failing tests**

Add validation tests to `internal/manifest/validate_test.go`: a manifest with a
well-formed `publish:` block validates; a `publish:` block with an unknown
`agent` value is rejected; a negative `intervalMinutes` is rejected.

```go
func TestPublishBlockValid(t *testing.T) {
	m := &Manifest{Version: 1, Publish: &Publish{
		ArtifactURL: "https://example.com/a", Agent: "claude-desktop",
		Sync: PublishSync{IntervalMinutes: 360, RunAtLogin: true},
	}}
	if err := Validate(m); err != nil {
		t.Fatalf("expected valid, got %v", err)
	}
}

func TestPublishBlockRejectsUnknownAgentAndBadInterval(t *testing.T) {
	bad := &Manifest{Version: 1, Publish: &Publish{ArtifactURL: "https://x", Agent: "nope"}}
	if Validate(bad) == nil {
		t.Error("unknown publish.agent must be rejected")
	}
	neg := &Manifest{Version: 1, Publish: &Publish{ArtifactURL: "https://x", Agent: "claude-desktop", Sync: PublishSync{IntervalMinutes: -1}}}
	if Validate(neg) == nil {
		t.Error("negative intervalMinutes must be rejected")
	}
}
```

(Match `Validate`'s real signature — check `validate.go`; if validation is a
method or takes a layer, adapt the calls accordingly.)

- [ ] **Step 2: Run test, verify it fails**

Run: `go test ./internal/manifest/ -run Publish -v`
Expected: FAIL — `undefined: Publish`.

- [ ] **Step 3: Implement types**

In `internal/manifest/types.go`, add a field to the `Manifest` struct:

```go
	Publish *Publish `yaml:"publish,omitempty"`
```

And the new types:

```go
// Publish configures the artifact a team publishes for subscriber machines
// (spec: docs/superpowers/specs/2026-05-22-subscriber-mode-design.md §5).
type Publish struct {
	ArtifactURL string      `yaml:"artifactURL"`
	Agent       string      `yaml:"agent"`
	Sync        PublishSync `yaml:"sync"`
}

// PublishSync controls the subscriber's generated scheduled job.
type PublishSync struct {
	IntervalMinutes int  `yaml:"intervalMinutes"`
	RunAtLogin      bool `yaml:"runAtLogin"`
}
```

- [ ] **Step 4: Implement validation**

In `internal/manifest/validate.go`, inside the function that validates a
`Manifest`, add (using `agent.Known` — add the import if absent):

```go
	if m.Publish != nil {
		if m.Publish.ArtifactURL == "" {
			return fmt.Errorf("publish: artifactURL is required")
		}
		if !agent.Known(m.Publish.Agent) {
			return fmt.Errorf("publish: unknown agent %q", m.Publish.Agent)
		}
		if m.Publish.Sync.IntervalMinutes < 0 {
			return fmt.Errorf("publish: sync.intervalMinutes must not be negative")
		}
	}
```

- [ ] **Step 5: Implement schema**

In `internal/schema/schema.go`, add a `publish` property to the manifest JSON
Schema object, mirroring the structure of an existing object property: an
object with `artifactURL` (string, required), `agent` (string), and `sync`
(object with `intervalMinutes` integer and `runAtLogin` boolean).

- [ ] **Step 6: Run tests, verify they pass**

Run: `go test ./internal/manifest/ ./internal/schema/ -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/manifest/ internal/schema/
git commit -m "Add publish: manifest block — schema, types, validation"
```

---

### Task 5: The artifact package

**Files:**
- Create: `internal/artifact/artifact.go`
- Test: `internal/artifact/artifact_test.go`

This package owns the artifact directory layout, the `ainfra.sub.json`
descriptor, and `MANIFEST.sha256` generation/verification.

- [ ] **Step 1: Write failing tests**

Create `internal/artifact/artifact_test.go`:

```go
func TestWriteThenVerifyRoundTrips(t *testing.T) {
	dir := t.TempDir()
	d := Descriptor{SchemaVersion: 1, ArtifactURL: "https://x", Agent: "claude-desktop",
		Sync: Sync{IntervalMinutes: 360, RunAtLogin: true}}
	files := map[string][]byte{"ainfra.lock": []byte("version: 1\n")}
	if err := Write(dir, d, files); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := Verify(dir); err != nil {
		t.Fatalf("Verify: %v", err)
	}
}

func TestVerifyDetectsTampering(t *testing.T) {
	dir := t.TempDir()
	d := Descriptor{SchemaVersion: 1, ArtifactURL: "https://x", Agent: "claude-desktop"}
	must(t, Write(dir, d, map[string][]byte{"ainfra.lock": []byte("a")}))
	must(t, os.WriteFile(filepath.Join(dir, "ainfra.lock"), []byte("tampered"), 0o644))
	if Verify(dir) == nil {
		t.Error("Verify must reject a tampered artifact")
	}
}

func TestReadDescriptor(t *testing.T) {
	dir := t.TempDir()
	in := Descriptor{SchemaVersion: 1, ArtifactURL: "https://x", Agent: "claude-desktop"}
	must(t, Write(dir, in, map[string][]byte{"ainfra.lock": []byte("a")}))
	got, err := ReadDescriptor(dir)
	if err != nil || got.ArtifactURL != "https://x" {
		t.Fatalf("ReadDescriptor: %v / %+v", err, got)
	}
}
```

Add a `must` test helper if not present.

- [ ] **Step 2: Run test, verify it fails**

Run: `go test ./internal/artifact/ -v`
Expected: FAIL — package undefined.

- [ ] **Step 3: Implement**

Create `internal/artifact/artifact.go`:

```go
// Package artifact owns the published-artifact layout a subscriber machine
// fetches: a copy of ainfra.lock, the ainfra.sub.json descriptor, optional
// bundles, and a MANIFEST.sha256 integrity index.
// See docs/superpowers/specs/2026-05-22-subscriber-mode-design.md §3.
package artifact

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// DescriptorName is the descriptor filename inside an artifact.
const DescriptorName = "ainfra.sub.json"

// ManifestName is the integrity-index filename inside an artifact.
const ManifestName = "MANIFEST.sha256"

// Descriptor is the subscription descriptor a subscriber machine reads.
type Descriptor struct {
	SchemaVersion int    `json:"schemaVersion"`
	ArtifactURL   string `json:"artifactURL"`
	Agent         string `json:"agent"`
	Sync          Sync   `json:"sync"`
}

// Sync controls the subscriber's generated scheduled job.
type Sync struct {
	IntervalMinutes int  `json:"intervalMinutes"`
	RunAtLogin      bool `json:"runAtLogin"`
}

// Write creates an artifact directory: every entry of files, the descriptor,
// and a MANIFEST.sha256 hashing all of them (the descriptor included, the
// manifest itself excluded).
func Write(dir string, d Descriptor, files map[string][]byte) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	all := map[string][]byte{}
	for name, content := range files {
		all[name] = content
	}
	desc, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	all[DescriptorName] = desc

	for name, content := range all {
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(full, content, 0o644); err != nil {
			return err
		}
	}
	return os.WriteFile(filepath.Join(dir, ManifestName), []byte(hashIndex(all)), 0o644)
}

// hashIndex renders a deterministic "<sha256>  <name>" line per file, sorted
// by name.
func hashIndex(files map[string][]byte) string {
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, n := range names {
		sum := sha256.Sum256(files[n])
		fmt.Fprintf(&b, "%s  %s\n", hex.EncodeToString(sum[:]), n)
	}
	return b.String()
}

// Verify recomputes hashes for every file listed in MANIFEST.sha256 and fails
// if any file is missing or its content does not match.
func Verify(dir string) error {
	idx, err := os.ReadFile(filepath.Join(dir, ManifestName))
	if err != nil {
		return fmt.Errorf("artifact: reading %s: %w", ManifestName, err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(idx)), "\n") {
		if line == "" {
			continue
		}
		want, name, ok := strings.Cut(line, "  ")
		if !ok {
			return fmt.Errorf("artifact: malformed %s line %q", ManifestName, line)
		}
		content, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return fmt.Errorf("artifact: %s missing: %w", name, err)
		}
		sum := sha256.Sum256(content)
		if got := hex.EncodeToString(sum[:]); got != want {
			return fmt.Errorf("artifact: %s hash mismatch (want %s, got %s)", name, want, got)
		}
	}
	return nil
}

// ReadDescriptor loads and parses the descriptor from an artifact directory.
func ReadDescriptor(dir string) (Descriptor, error) {
	var d Descriptor
	raw, err := os.ReadFile(filepath.Join(dir, DescriptorName))
	if err != nil {
		return d, err
	}
	err = json.Unmarshal(raw, &d)
	return d, err
}
```

- [ ] **Step 4: Run tests, verify they pass**

Run: `go test ./internal/artifact/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/artifact/
git commit -m "Add artifact package: descriptor, layout, MANIFEST.sha256 integrity"
```

---

### Task 6: The `ainfra publish` command

**Files:**
- Create: `cmd/ainfra/cmd_publish.go`
- Modify: `cmd/ainfra/main.go` (register the command)
- Test: `cmd/ainfra/cmd_publish_test.go`

- [ ] **Step 1: Write failing test**

Create `cmd/ainfra/cmd_publish_test.go`. Test: in a temp dir with an
`ainfra.yaml` carrying a `publish:` block and an `ainfra.lock`, `runPublish`
exits 0 and produces `<out>/ainfra.lock`, `<out>/ainfra.sub.json`,
`<out>/MANIFEST.sha256`; `artifact.Verify(out)` passes. Second test: a manifest
with no `publish:` block exits non-zero with a clear error. Follow the harness
used by `cmd_plan_test.go` / `cmd_lock.go` tests for invoking a command in a
temp dir.

- [ ] **Step 2: Run test, verify it fails**

Run: `go test ./cmd/ainfra/ -run Publish -v`
Expected: FAIL — `runPublish` undefined.

- [ ] **Step 3: Implement**

Create `cmd/ainfra/cmd_publish.go`:

```go
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/MHilhorst/ainfra/internal/artifact"
	"github.com/MHilhorst/ainfra/internal/cli"
	"github.com/MHilhorst/ainfra/internal/manifest"
	"github.com/MHilhorst/ainfra/internal/ui"
)

// newPublishCommand packages the resolved lockfile into a subscriber artifact.
func newPublishCommand() *cli.Command {
	var out string
	return &cli.Command{
		Name:      "publish",
		Summary:   "Package the resolved lockfile into a subscriber artifact",
		UsageLine: "ainfra publish [--out <dir>]",
		Example:   "ainfra publish --out ./dist",
		SetFlags:  func(fs *flag.FlagSet) { fs.StringVar(&out, "out", "ainfra-artifact", "artifact output directory") },
		Run:       func(ctx cli.Context) int { return runPublish(ctx, out) },
	}
}

func runPublish(ctx cli.Context, out string) int {
	errColor := ui.NewColorizer(ctx.Stderr, ctx.NoColor)
	dir := ctx.Dir

	layers, err := manifest.LoadLayers(dir)
	if err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}
	repo := layers[manifest.LayerRepo]
	if repo == nil || repo.Publish == nil {
		ui.RenderError(ctx.Stderr, errColor, fmt.Errorf("no publish: block in ainfra.yaml — add one to publish an artifact"))
		return 1
	}
	pub := repo.Publish

	lockPath := filepath.Join(dir, "ainfra.lock")
	lockBytes, err := os.ReadFile(lockPath)
	if err != nil {
		ui.RenderError(ctx.Stderr, errColor, fmt.Errorf("ainfra.lock not found — run `ainfra lock` first"))
		return 1
	}

	desc := artifact.Descriptor{
		SchemaVersion: 1,
		ArtifactURL:   pub.ArtifactURL,
		Agent:         pub.Agent,
		Sync: artifact.Sync{
			IntervalMinutes: pub.Sync.IntervalMinutes,
			RunAtLogin:      pub.Sync.RunAtLogin,
		},
	}
	if err := artifact.Write(out, desc, map[string][]byte{"ainfra.lock": lockBytes}); err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}

	c := ui.NewColorizer(ctx.Stdout, ctx.NoColor)
	fmt.Fprintf(ctx.Stdout, "ainfra: wrote artifact to %s\n", out)
	ui.Next(ctx.Stdout, c, "upload the artifact directory to "+pub.ArtifactURL)
	return 0
}
```

Note: `LoadLayers` keys by `manifest.Layer`; the repo manifest is
`layers[manifest.LayerRepo]`. If `LoadLayers` returns the repo manifest under a
different key in this codebase, adjust — verify against `internal/manifest/load.go`.

- [ ] **Step 4: Register the command**

In `cmd/ainfra/main.go`, add `newPublishCommand()` to the command slice
alongside `newLockCommand()` etc.

- [ ] **Step 5: Run test, verify it passes**

Run: `go test ./cmd/ainfra/ -run Publish -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add cmd/ainfra/cmd_publish.go cmd/ainfra/cmd_publish_test.go cmd/ainfra/main.go
git commit -m "Add ainfra publish command: package lockfile into subscriber artifact"
```

---

### Task 7: The HTTP fetcher

**Files:**
- Create: `internal/provider/fetch/http.go`
- Test: `internal/provider/fetch/http_test.go`

The artifact for a subscriber is fetched over HTTP(S). v1 supports an artifact
served as a directory of files; the subscriber fetches the descriptor and the
files it names. For simplicity and testability, v1 fetches a single artifact
**file** by URL via `FetchURL`, used by the artifact-download step in Task 8.

- [ ] **Step 1: Write failing test**

Create `internal/provider/fetch/http_test.go`: spin up an `httptest.Server`
serving known bytes, assert `FetchURL` returns them; assert a 404 yields an
error.

```go
func TestFetchURLReturnsBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("hello"))
	}))
	defer srv.Close()
	got, err := FetchURL(srv.URL)
	if err != nil || string(got) != "hello" {
		t.Fatalf("FetchURL: %v / %q", err, got)
	}
}

func TestFetchURLErrorsOn404(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	if _, err := FetchURL(srv.URL); err == nil {
		t.Error("FetchURL must error on 404")
	}
}
```

- [ ] **Step 2: Run test, verify it fails**

Run: `go test ./internal/provider/fetch/ -run FetchURL -v`
Expected: FAIL — `FetchURL` undefined.

- [ ] **Step 3: Implement**

Create `internal/provider/fetch/http.go`:

```go
package fetch

import (
	"fmt"
	"io"
	"net/http"
	"time"
)

// FetchURL retrieves the bytes at an http(s) URL. A non-2xx response is an
// error. It is the subscriber's artifact-download primitive.
func FetchURL(url string) ([]byte, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("fetch: GET %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("fetch: GET %s: status %d", url, resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}
```

- [ ] **Step 4: Run test, verify it passes**

Run: `go test ./internal/provider/fetch/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/provider/fetch/http.go internal/provider/fetch/http_test.go
git commit -m "Add HTTP fetcher: subscriber artifact-download primitive"
```

---

### Task 8: Subscriber mode — `apply --from` and `check --from`

**Files:**
- Modify: `cmd/ainfra/commands.go`
- Modify: `cmd/ainfra/reconcile.go`
- Test: `cmd/ainfra/cmd_apply_test.go`

In subscriber mode the source is an artifact directory or URL, not a repo. The
artifact's `ainfra.lock` is already-resolved state, and its descriptor names
the agent. We reuse the orchestrator with the artifact's agent provider set.

- [ ] **Step 1: Write failing test**

Add to `cmd_apply_test.go`: build a local artifact directory with
`artifact.Write` (descriptor agent `claude-desktop`, an `ainfra.lock` with one
MCP server), run `apply --from <dir> --yes` with a temp `$HOME`, assert exit 0
and that `<home>/Library/Application Support/Claude/claude_desktop_config.json`
now contains the server. Second test: `apply --from <dir>` where the artifact
fails `Verify` (tamper a file) exits non-zero and writes no config file.

- [ ] **Step 2: Run test, verify it fails**

Run: `go test ./cmd/ainfra/ -run ApplyFrom -v`
Expected: FAIL — `--from` flag unknown.

- [ ] **Step 3: Implement the subscriber reconcile path**

In `cmd/ainfra/reconcile.go`, add:

```go
// artifactSource resolves a --from value to a local artifact directory. A
// local path is used in place; an http(s) URL has its descriptor and
// lockfile downloaded into a temp directory. The returned directory is a
// verified artifact: artifact.Verify has passed.
func artifactSource(from string) (dir string, cleanup func(), err error) {
	if !strings.HasPrefix(from, "http://") && !strings.HasPrefix(from, "https://") {
		if err := artifact.Verify(from); err != nil {
			return "", func() {}, err
		}
		return from, func() {}, nil
	}
	tmp, err := os.MkdirTemp("", "ainfra-artifact-")
	if err != nil {
		return "", func() {}, err
	}
	cleanup = func() { os.RemoveAll(tmp) }
	for _, name := range []string{artifact.ManifestName, artifact.DescriptorName, "ainfra.lock"} {
		body, err := fetch.FetchURL(strings.TrimRight(from, "/") + "/" + name)
		if err != nil {
			cleanup()
			return "", func() {}, err
		}
		if err := os.WriteFile(filepath.Join(tmp, name), body, 0o644); err != nil {
			cleanup()
			return "", func() {}, err
		}
	}
	if err := artifact.Verify(tmp); err != nil {
		cleanup()
		return "", func() {}, err
	}
	return tmp, cleanup, nil
}

// providersForArtifact returns the provider set for an artifact's declared agent.
func providersForArtifact(dir string) ([]provider.Provider, agent.ID, error) {
	desc, err := artifact.ReadDescriptor(dir)
	if err != nil {
		return nil, "", err
	}
	id := agent.ID(desc.Agent)
	ps, err := agentset.ForAgent(id)
	return ps, id, err
}
```

Add the needed imports (`os`, `strings`, `path/filepath`, the `artifact` and
`fetch` packages).

- [ ] **Step 4: Add `runApplyFrom`**

In `cmd/ainfra/commands.go`, add a subscriber apply path. The artifact's
lockfile already carries rendered MCP payloads (it is `ainfra.lock`), so use
the lockfile-driven plan path (`PlanAll` / equivalent apply). Implement:

```go
func runApplyFrom(ctx cli.Context, from string, yes bool) int {
	errColor := ui.NewColorizer(ctx.Stderr, ctx.NoColor)

	dir, cleanup, err := artifactSource(from)
	if err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}
	defer cleanup()

	lock, err := lockfile.Read(filepath.Join(dir, "ainfra.lock"))
	if err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}
	providers, _, err := providersForArtifact(dir)
	if err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}

	home, _ := os.UserHomeDir()
	env := provider.Env{FS: provider.OSFilesystem{}, Runner: provider.ExecRunner{}, Home: home, Root: home}
	orch := provider.NewOrchestrator(home, env, providers)
	plans, err := orch.PlanAll(lock)
	if err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}

	allEmpty := true
	for _, p := range plans {
		if !p.Empty() {
			allEmpty = false
		}
	}
	if allEmpty {
		fmt.Fprintln(ctx.Stdout, "Nothing to do.")
		return 0
	}
	c := ui.NewColorizer(ctx.Stdout, ctx.NoColor)
	ui.RenderPlan(ctx.Stdout, c, plans)
	if !yes {
		ok, err := ui.Confirm(ctx.Stdin, ctx.Stdout, "Apply these changes? (yes/no): ")
		if err != nil || !ok {
			fmt.Fprintln(ctx.Stdout, "Aborted.")
			return 0
		}
	}
	if err := orch.ApplyAll(lock); err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}
	fmt.Fprintln(ctx.Stdout, "Apply complete.")
	return 0
}
```

IMPORTANT — verify the orchestrator API before writing this: `commands.go`'s
`runApply` uses `PlanAllRendered` / `ApplyAllRendered` with rendered resources
from `resolve.RenderResources`, while `runPlan` uses `PlanAll(merged)` with a
lockfile. The artifact path has only a lockfile. Inspect
`internal/provider/orchestrator.go`: if a lockfile-only `ApplyAll` does not
exist, the MCP payloads needed for apply must come from the lockfile entries.
Confirm the lockfile `Entry` for an MCP server carries the command/args/env
payload; if it does, add an orchestrator method that plans+applies from a
lockfile (mirroring `PlanAll`). Implement that orchestrator method as part of
this step rather than assuming `ApplyAll` exists.

- [ ] **Step 5: Wire the `--from` flag**

In `newApplyCommand`, add the flag and branch:

```go
func newApplyCommand() *cli.Command {
	var yes bool
	var from string
	return &cli.Command{
		Name:      "apply",
		Summary:   "Reconcile the environment to match the manifest (or a published artifact)",
		UsageLine: "ainfra apply [--yes] [--from <url-or-dir>]",
		Example:   "ainfra apply --from https://downloads.example.com/ainfra/sales --yes",
		SetFlags: func(fs *flag.FlagSet) {
			fs.BoolVar(&yes, "yes", false, "skip confirmation prompt")
			fs.StringVar(&from, "from", "", "reconcile against a published artifact instead of a repo")
		},
		Run: func(ctx cli.Context) int {
			if from != "" {
				return runApplyFrom(ctx, from, yes)
			}
			return runApply(ctx, yes)
		},
	}
}
```

Apply the same `--from` flag and a `runCheckFrom` (plan-only, no apply, exit 1
on drift) to `newCheckCommand`.

- [ ] **Step 6: Run tests, verify they pass**

Run: `go test ./cmd/ainfra/ -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add cmd/ainfra/ internal/provider/
git commit -m "Add subscriber mode: apply/check --from a published artifact"
```

---

### Task 9: The `ainfra installer` command

**Files:**
- Create: `internal/installer/launchd.go`
- Create: `cmd/ainfra/cmd_installer.go`
- Modify: `cmd/ainfra/main.go`
- Test: `internal/installer/launchd_test.go`, `cmd/ainfra/cmd_installer_test.go`

v1 emits a macOS launchd plist and a `.command` install script. The script and
plist are generated from an `artifact.Descriptor`.

- [ ] **Step 1: Write failing test for the plist**

Create `internal/installer/launchd_test.go`:

```go
func TestLaunchdPlistContainsIntervalAndURL(t *testing.T) {
	out := LaunchdPlist(Params{
		Label: "com.ainfra.subscriber", BinPath: "/usr/local/bin/ainfra",
		ArtifactURL: "https://x/a", IntervalMinutes: 360, RunAtLogin: true,
	})
	for _, want := range []string{"com.ainfra.subscriber", "https://x/a", "<integer>21600</integer>", "RunAtLoad"} {
		if !strings.Contains(out, want) {
			t.Errorf("plist missing %q", want)
		}
	}
}
```

(21600 = 360 minutes in seconds — `StartInterval` is in seconds.)

- [ ] **Step 2: Run test, verify it fails**

Run: `go test ./internal/installer/ -v`
Expected: FAIL — package undefined.

- [ ] **Step 3: Implement the plist generator**

Create `internal/installer/launchd.go`:

```go
// Package installer generates the one-time installer and the scheduled job a
// subscriber machine uses to stay in sync with a published artifact.
// See docs/superpowers/specs/2026-05-22-subscriber-mode-design.md §6.
package installer

import (
	"fmt"
	"strings"
)

// Params drive launchd plist and install-script generation.
type Params struct {
	Label           string
	BinPath         string
	ArtifactURL     string
	IntervalMinutes int
	RunAtLogin      bool
}

// LaunchdPlist renders a launchd LaunchAgent plist that runs
// `ainfra apply --from <url> --yes` on an interval.
func LaunchdPlist(p Params) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	b.WriteString(`<plist version="1.0">` + "\n<dict>\n")
	fmt.Fprintf(&b, "  <key>Label</key><string>%s</string>\n", p.Label)
	b.WriteString("  <key>ProgramArguments</key>\n  <array>\n")
	for _, arg := range []string{p.BinPath, "apply", "--from", p.ArtifactURL, "--yes"} {
		fmt.Fprintf(&b, "    <string>%s</string>\n", arg)
	}
	b.WriteString("  </array>\n")
	if p.RunAtLogin {
		b.WriteString("  <key>RunAtLoad</key><true/>\n")
	}
	fmt.Fprintf(&b, "  <key>StartInterval</key><integer>%d</integer>\n", p.IntervalMinutes*60)
	b.WriteString("</dict>\n</plist>\n")
	return b.String()
}
```

- [ ] **Step 4: Write failing test for the install script**

Add to `cmd/ainfra/cmd_installer_test.go`: `runInstaller` in a temp dir with an
`ainfra.yaml` carrying a `publish:` block produces an output `.command` file
that is non-empty and references the artifact URL.

- [ ] **Step 5: Implement the installer command**

Create `cmd/ainfra/cmd_installer.go`. `newInstallerCommand` reads the repo
manifest's `publish:` block (same lookup as `runPublish`), builds
`installer.Params`, and writes a `.command` shell script to `--out` (default
`ainfra-install.command`). The script: downloads the `ainfra` release binary
into `~/.ainfra/bin`, writes the launchd plist (from `installer.LaunchdPlist`)
into `~/Library/LaunchAgents/com.ainfra.subscriber.plist`, runs
`launchctl load` on it, then runs the first `ainfra apply --from <url> --yes`.
Embed the plist text and artifact URL into the generated script as heredocs.
Register `newInstallerCommand()` in `main.go`.

- [ ] **Step 6: Run tests, verify they pass**

Run: `go test ./internal/installer/ ./cmd/ainfra/ -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/installer/ cmd/ainfra/cmd_installer.go cmd/ainfra/cmd_installer_test.go cmd/ainfra/main.go
git commit -m "Add ainfra installer: one-time installer + launchd scheduled job"
```

---

### Task 10: Full verification and docs

**Files:**
- Modify: `README.md` (commands table, status)

- [ ] **Step 1: Run the whole suite**

Run: `go build ./... && go test ./...`
Expected: build succeeds, all tests PASS.

- [ ] **Step 2: Smoke-test end to end**

```bash
go build -o /tmp/ainfra ./cmd/ainfra
# In a scratch dir with an ainfra.yaml (publish: block) + ainfra.lock:
/tmp/ainfra publish --out /tmp/art
/tmp/ainfra apply --from /tmp/art --yes
```
Expected: artifact written, then `claude_desktop_config.json` reconciled.

- [ ] **Step 3: Update README**

Add `publish`, `installer` rows to the commands table; add an `apply --from` /
`check --from` note. Add a "Subscriber mode" paragraph and a status-table row.

- [ ] **Step 4: Commit**

```bash
git add README.md
git commit -m "Document subscriber mode: publish, installer, apply --from"
```

---

## Self-Review Notes

- **Spec coverage:** §2 → Tasks 1-3; §3 → Tasks 5-6; §4 → Tasks 7-8; §5 → Task 4; §6 → Task 9; §8 testing folded into each task. All sections covered.
- **Deferred items** (secrets, Windows installer, signing, hosting) are intentionally absent — they are out of scope per spec §7.
- **Known verification points flagged inline:** `manifest.LoadLayers` key for the repo layer (Task 6), the `Validate` signature (Task 4), and the orchestrator's lockfile-only apply path (Task 8) must each be confirmed against the real code during execution — the plan says so at the point of use.
