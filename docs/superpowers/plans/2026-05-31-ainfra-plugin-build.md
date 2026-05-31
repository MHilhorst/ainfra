# ainfra plugin build/release Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an `ainfra plugin build|release` subcommand that generates the team plugin's `.claude-plugin/plugin.json` and syncs its `marketplace.json` entry from a new `plugin:` manifest block, with an explicit-bump release gated by a content drift guard.

**Architecture:** A new maintainer-only CLI command (not part of `apply`). A pure `internal/plugin` package does content hashing, manifest rendering, marketplace-entry merging, semver bumping, and the release decision. The drift baseline `{version, contentHash}` is stored in a new `plugin:` section of `ainfra.lock`. The command orchestrates: load manifest → run `claude plugin validate` → hash → decide → write files + lock.

**Tech Stack:** Go 1.25, `gopkg.in/yaml.v3` (manifest + lock), `encoding/json` (plugin/marketplace files), `crypto/sha256`. No new dependencies.

Spec: `docs/superpowers/specs/2026-05-31-ainfra-plugin-build-design.md`

---

## File Structure

- `internal/manifest/types.go` — add `PluginBuild` + `PluginAuthor` structs and a `Plugin *PluginBuild` field on `Manifest` (singular; distinct from the existing consumer-side `Plugins map[string]Plugin`).
- `internal/manifest/validate.go` — validate the `plugin:` block.
- `internal/lockfile/types.go` — add `Plugin *PluginBaseline` to `Lock`.
- `internal/plugin/` (new package, pure):
  - `hash.go` — `ContentHash(root, paths)`.
  - `semver.go` — `Bump(version, level)`.
  - `render.go` — `RenderPluginJSON(p, version)`.
  - `marketplace.go` — `MergeMarketplaceEntry(existing, p)`.
  - `release.go` — `Decide(currentHash, baselineHash, baselineVersion, bumpLevel)`.
- `cmd/ainfra/cmd_plugin.go` — `newPluginCommand()` wiring `build`/`release`.
- `cmd/ainfra/main.go` — register the command.

---

## Task 1: Manifest `plugin:` block — types

**Files:**
- Modify: `internal/manifest/types.go`
- Test: `internal/manifest/plugin_build_test.go`

- [ ] **Step 1: Write the failing test**

```go
package manifest

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_PluginBlock(t *testing.T) {
	dir := t.TempDir()
	yaml := `version: 1
agent: claude-code
plugin:
  name: tvt-config
  description: "Team config"
  marketplace: trein-vertraging
  author: { name: Trein-Vertraging, url: https://github.com/trein-vertraging }
  repository: https://github.com/trein-vertraging/claude-config
  license: UNLICENSED
  content: [ skills/, .mcp.json ]
`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	layers, err := LoadLayers(dir)
	if err != nil {
		t.Fatalf("LoadLayers: %v", err)
	}
	repo := layers[LayerRepo]
	if repo == nil || repo.Plugin == nil {
		t.Fatal("expected plugin block to be parsed")
	}
	if repo.Plugin.Name != "tvt-config" || repo.Plugin.Marketplace != "trein-vertraging" {
		t.Errorf("got name=%q marketplace=%q", repo.Plugin.Name, repo.Plugin.Marketplace)
	}
	if repo.Plugin.Author.Name != "Trein-Vertraging" {
		t.Errorf("got author name %q", repo.Plugin.Author.Name)
	}
	if len(repo.Plugin.Content) != 2 || repo.Plugin.Content[0] != "skills/" {
		t.Errorf("got content %v", repo.Plugin.Content)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/manifest/ -run TestLoad_PluginBlock -v`
Expected: FAIL — `repo.Plugin undefined (type *Manifest has no field or method Plugin)`.

- [ ] **Step 3: Add the structs and field**

In `internal/manifest/types.go`, add the field to `Manifest` (right after the existing `Plugins map[string]Plugin` line):

```go
	Plugin *PluginBuild `yaml:"plugin,omitempty"`
```

And add these struct definitions near the other channel types:

```go
// PluginBuild declares how to generate this repo's own Claude Code plugin
// (the `plugin:` block). It drives `ainfra plugin build|release` only; it is
// not part of apply. Distinct from the consumer-side Plugin install type.
type PluginBuild struct {
	Name        string       `yaml:"name"`
	Description string       `yaml:"description"`
	Marketplace string       `yaml:"marketplace"`
	Author      PluginAuthor `yaml:"author"`
	Repository  string       `yaml:"repository,omitempty"`
	License     string       `yaml:"license,omitempty"`
	Content     []string     `yaml:"content,omitempty"`
}

// PluginAuthor is the author metadata written into plugin.json.
type PluginAuthor struct {
	Name string `yaml:"name" json:"name"`
	URL  string `yaml:"url" json:"url,omitempty"`
}

// ContentPaths returns the drift-hash inputs, defaulting to the standard
// plugin payload directories when none are declared.
func (p PluginBuild) ContentPaths() []string {
	if len(p.Content) > 0 {
		return p.Content
	}
	return []string{"skills/", "commands/", "hooks/", ".mcp.json"}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/manifest/ -run TestLoad_PluginBlock -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/manifest/types.go internal/manifest/plugin_build_test.go
git commit -m "feat(manifest): parse plugin build block"
```

---

## Task 2: Validate the `plugin:` block

**Files:**
- Modify: `internal/manifest/validate.go`
- Test: `internal/manifest/plugin_build_test.go`

- [ ] **Step 1: Write the failing test** (append to `plugin_build_test.go`)

```go
func TestValidatePlugin(t *testing.T) {
	ok := &Manifest{Plugin: &PluginBuild{Name: "tvt-config", Marketplace: "trein-vertraging"}}
	if err := validatePlugin(ok); err != nil {
		t.Errorf("valid plugin rejected: %v", err)
	}

	noName := &Manifest{Plugin: &PluginBuild{Marketplace: "m"}}
	if err := validatePlugin(noName); err == nil {
		t.Error("expected error when plugin.name missing")
	}

	noMarket := &Manifest{Plugin: &PluginBuild{Name: "n"}}
	if err := validatePlugin(noMarket); err == nil {
		t.Error("expected error when plugin.marketplace missing")
	}

	none := &Manifest{}
	if err := validatePlugin(none); err != nil {
		t.Errorf("absent plugin block must be valid: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/manifest/ -run TestValidatePlugin -v`
Expected: FAIL — `undefined: validatePlugin`.

- [ ] **Step 3: Implement `validatePlugin` and wire it into `ValidateAll`**

Add to `internal/manifest/validate.go`:

```go
// validatePlugin checks the optional `plugin:` build block. An absent block is
// valid; a present block must name the plugin and its target marketplace.
func validatePlugin(m *Manifest) error {
	if m.Plugin == nil {
		return nil
	}
	if strings.TrimSpace(m.Plugin.Name) == "" {
		return fmt.Errorf("plugin.name is required")
	}
	if strings.TrimSpace(m.Plugin.Marketplace) == "" {
		return fmt.Errorf("plugin.marketplace is required")
	}
	return nil
}
```

`ValidateAll` delegates to a per-manifest `func Validate(m *Manifest) error` (called at `validate.go:556` as `Validate(toValidate)`). Add the call near the top of `Validate`, alongside the other per-manifest checks:

Run: `grep -n "^func Validate(" internal/manifest/validate.go`

Inside `Validate`, add:

```go
	if err := validatePlugin(m); err != nil {
		return err
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/manifest/ -run TestValidatePlugin -v`
Expected: PASS.

- [ ] **Step 5: Run the full manifest package to check nothing regressed**

Run: `go test ./internal/manifest/`
Expected: PASS (ok).

- [ ] **Step 6: Commit**

```bash
git add internal/manifest/validate.go internal/manifest/plugin_build_test.go
git commit -m "feat(manifest): validate plugin build block"
```

---

## Task 3: Lockfile plugin baseline round-trips

**Files:**
- Modify: `internal/lockfile/types.go`
- Test: `internal/lockfile/plugin_baseline_test.go`

- [ ] **Step 1: Write the failing test**

```go
package lockfile

import (
	"path/filepath"
	"testing"
)

func TestPluginBaseline_RoundTrips(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ainfra.lock")

	l := &Lock{
		Version: 1,
		Plugin:  &PluginBaseline{Name: "tvt-config", Version: "2.11.0", ContentHash: "abc123"},
	}
	if err := Write(path, l); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.Plugin == nil {
		t.Fatal("plugin baseline lost on round-trip")
	}
	if got.Plugin.Version != "2.11.0" || got.Plugin.ContentHash != "abc123" {
		t.Errorf("got %+v", got.Plugin)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/lockfile/ -run TestPluginBaseline_RoundTrips -v`
Expected: FAIL — `unknown field or undefined: PluginBaseline`.

- [ ] **Step 3: Add the type and field**

In `internal/lockfile/types.go`, add to the `Lock` struct (after the `Secrets` field):

```go
	Plugin *PluginBaseline `yaml:"plugin,omitempty"`
```

And add the type:

```go
// PluginBaseline records the last released state of this repo's own plugin so
// `ainfra plugin release` can detect content drift. Written only by
// `ainfra plugin`; preserved untouched by lock/apply.
type PluginBaseline struct {
	Name        string `yaml:"name"`
	Version     string `yaml:"version"`
	ContentHash string `yaml:"contentHash"`
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/lockfile/ -run TestPluginBaseline_RoundTrips -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/lockfile/types.go internal/lockfile/plugin_baseline_test.go
git commit -m "feat(lockfile): add plugin release baseline"
```

---

## Task 4: Content hashing

**Files:**
- Create: `internal/plugin/hash.go`
- Test: `internal/plugin/hash_test.go`

- [ ] **Step 1: Write the failing test**

```go
package plugin

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestContentHash_StableAndSensitive(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "skills/a/SKILL.md", "alpha")
	writeFile(t, root, ".mcp.json", "{}")
	writeFile(t, root, "ignored.txt", "noise")

	h1, err := ContentHash(root, []string{"skills/", ".mcp.json"})
	if err != nil {
		t.Fatal(err)
	}
	// Same inputs -> same hash; unrelated file ignored.
	h2, _ := ContentHash(root, []string{"skills/", ".mcp.json"})
	if h1 != h2 {
		t.Error("hash not stable across calls")
	}
	writeFile(t, root, "ignored.txt", "different noise")
	h3, _ := ContentHash(root, []string{"skills/", ".mcp.json"})
	if h1 != h3 {
		t.Error("hash changed due to unrelated file")
	}
	// Changing a tracked file changes the hash.
	writeFile(t, root, "skills/a/SKILL.md", "beta")
	h4, _ := ContentHash(root, []string{"skills/", ".mcp.json"})
	if h1 == h4 {
		t.Error("hash did not change when tracked content changed")
	}
}

func TestContentHash_MissingPathIgnored(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, ".mcp.json", "{}")
	if _, err := ContentHash(root, []string{"skills/", ".mcp.json"}); err != nil {
		t.Errorf("missing dir should be ignored, got %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/plugin/ -run TestContentHash -v`
Expected: FAIL — package/`ContentHash` does not exist.

- [ ] **Step 3: Implement**

```go
package plugin

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	iofs "io/fs"
	"os"
	"path/filepath"
	"sort"
)

// ContentHash returns a deterministic hash over the files under the given
// paths (relative to root). Order-independent; a missing path is skipped. A
// single-file path (e.g. ".mcp.json") hashes that file.
func ContentHash(root string, paths []string) (string, error) {
	type entry struct {
		rel string
		sum [32]byte
	}
	var entries []entry

	for _, p := range paths {
		abs := filepath.Join(root, p)
		err := filepath.WalkDir(abs, func(path string, d iofs.DirEntry, err error) error {
			if err != nil {
				if errors.Is(err, iofs.ErrNotExist) {
					return nil
				}
				return err
			}
			if d.IsDir() {
				return nil
			}
			data, rerr := os.ReadFile(path)
			if rerr != nil {
				return rerr
			}
			rel, rerr := filepath.Rel(root, path)
			if rerr != nil {
				return rerr
			}
			entries = append(entries, entry{rel: filepath.ToSlash(rel), sum: sha256.Sum256(data)})
			return nil
		})
		if err != nil {
			return "", err
		}
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].rel < entries[j].rel })

	h := sha256.New()
	for _, e := range entries {
		io.WriteString(h, e.rel)
		h.Write([]byte{0})
		h.Write(e.sum[:])
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/plugin/ -run TestContentHash -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/plugin/hash.go internal/plugin/hash_test.go
git commit -m "feat(plugin): deterministic content hashing"
```

---

## Task 5: Semver bump

**Files:**
- Create: `internal/plugin/semver.go`
- Test: `internal/plugin/semver_test.go`

- [ ] **Step 1: Write the failing test**

```go
package plugin

import "testing"

func TestBump(t *testing.T) {
	cases := []struct {
		in, level, want string
	}{
		{"2.11.0", "patch", "2.11.1"},
		{"2.11.0", "minor", "2.12.0"},
		{"2.11.3", "major", "3.0.0"},
	}
	for _, c := range cases {
		got, err := Bump(c.in, c.level)
		if err != nil {
			t.Fatalf("Bump(%q,%q): %v", c.in, c.level, err)
		}
		if got != c.want {
			t.Errorf("Bump(%q,%q)=%q want %q", c.in, c.level, got, c.want)
		}
	}
	if _, err := Bump("2.11", "patch"); err == nil {
		t.Error("expected error on malformed version")
	}
	if _, err := Bump("2.11.0", "sideways"); err == nil {
		t.Error("expected error on unknown level")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/plugin/ -run TestBump -v`
Expected: FAIL — `undefined: Bump`.

- [ ] **Step 3: Implement**

```go
package plugin

import (
	"fmt"
	"strconv"
	"strings"
)

// Bump increments a MAJOR.MINOR.PATCH version by the given level
// ("major", "minor", or "patch").
func Bump(version, level string) (string, error) {
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("plugin: version %q is not MAJOR.MINOR.PATCH", version)
	}
	nums := make([]int, 3)
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return "", fmt.Errorf("plugin: version %q has non-numeric part %q", version, p)
		}
		nums[i] = n
	}
	switch level {
	case "major":
		nums[0], nums[1], nums[2] = nums[0]+1, 0, 0
	case "minor":
		nums[1], nums[2] = nums[1]+1, 0
	case "patch":
		nums[2]++
	default:
		return "", fmt.Errorf("plugin: unknown bump level %q", level)
	}
	return fmt.Sprintf("%d.%d.%d", nums[0], nums[1], nums[2]), nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/plugin/ -run TestBump -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/plugin/semver.go internal/plugin/semver_test.go
git commit -m "feat(plugin): semver bump"
```

---

## Task 6: Render plugin.json

**Files:**
- Create: `internal/plugin/render.go`
- Test: `internal/plugin/render_test.go`

- [ ] **Step 1: Write the failing test**

```go
package plugin

import (
	"encoding/json"
	"testing"

	"github.com/MHilhorst/ainfra/internal/manifest"
)

func TestRenderPluginJSON(t *testing.T) {
	p := manifest.PluginBuild{
		Name:        "tvt-config",
		Description: "Team config",
		Marketplace: "trein-vertraging",
		Author:      manifest.PluginAuthor{Name: "Trein-Vertraging", URL: "https://github.com/trein-vertraging"},
		Repository:  "https://github.com/trein-vertraging/claude-config",
		License:     "UNLICENSED",
	}
	out, err := RenderPluginJSON(p, "2.11.0")
	if err != nil {
		t.Fatal(err)
	}
	// Trailing newline, 2-space indent.
	if len(out) == 0 || out[len(out)-1] != '\n' {
		t.Error("expected trailing newline")
	}

	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if doc["name"] != "tvt-config" || doc["version"] != "2.11.0" {
		t.Errorf("got name=%v version=%v", doc["name"], doc["version"])
	}
	skills, ok := doc["skills"].([]any)
	if !ok || len(skills) != 1 || skills[0] != "./skills/" {
		t.Errorf("expected skills [./skills/], got %v", doc["skills"])
	}
	if _, ok := doc["agents"].([]any); !ok {
		t.Errorf("agents must render as a (possibly empty) array, got %v", doc["agents"])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/plugin/ -run TestRenderPluginJSON -v`
Expected: FAIL — `undefined: RenderPluginJSON`.

- [ ] **Step 3: Implement**

```go
package plugin

import (
	"encoding/json"

	"github.com/MHilhorst/ainfra/internal/manifest"
)

// pluginJSON is the on-disk shape of .claude-plugin/plugin.json. Field order
// here is the emitted key order.
type pluginJSON struct {
	Name        string                 `json:"name"`
	Version     string                 `json:"version"`
	Description string                 `json:"description"`
	Author      *manifest.PluginAuthor `json:"author,omitempty"`
	Repository  string                 `json:"repository,omitempty"`
	License     string                 `json:"license,omitempty"`
	Skills      []string               `json:"skills"`
	Agents      []string               `json:"agents"`
}

// RenderPluginJSON produces the bytes of .claude-plugin/plugin.json for the
// given build block and version (2-space indent, trailing newline).
func RenderPluginJSON(p manifest.PluginBuild, version string) ([]byte, error) {
	doc := pluginJSON{
		Name:        p.Name,
		Version:     version,
		Description: p.Description,
		Repository:  p.Repository,
		License:     p.License,
		Skills:      []string{"./skills/"},
		Agents:      []string{},
	}
	if p.Author.Name != "" || p.Author.URL != "" {
		a := p.Author
		doc.Author = &a
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/plugin/ -run TestRenderPluginJSON -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/plugin/render.go internal/plugin/render_test.go
git commit -m "feat(plugin): render plugin.json"
```

---

## Task 7: Merge the self-entry into marketplace.json

**Files:**
- Create: `internal/plugin/marketplace.go`
- Test: `internal/plugin/marketplace_test.go`

- [ ] **Step 1: Write the failing test**

```go
package plugin

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/MHilhorst/ainfra/internal/manifest"
)

const sampleMarketplace = `{
  "name": "trein-vertraging",
  "owner": { "name": "Trein-Vertraging" },
  "plugins": [
    {
      "name": "tvt-config",
      "source": "./",
      "description": "OLD description",
      "license": "UNLICENSED"
    },
    {
      "name": "claude-ads",
      "source": { "source": "github", "repo": "AgriciDaniel/claude-ads" },
      "description": "third party"
    }
  ]
}`

func TestMergeMarketplaceEntry(t *testing.T) {
	p := manifest.PluginBuild{
		Name:        "tvt-config",
		Description: "NEW description",
		Marketplace: "trein-vertraging",
	}
	out, err := MergeMarketplaceEntry([]byte(sampleMarketplace), p)
	if err != nil {
		t.Fatal(err)
	}

	var doc struct {
		Plugins []map[string]any `json:"plugins"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if len(doc.Plugins) != 2 {
		t.Fatalf("expected 2 plugins, got %d", len(doc.Plugins))
	}

	var self, third map[string]any
	for _, e := range doc.Plugins {
		switch e["name"] {
		case "tvt-config":
			self = e
		case "claude-ads":
			third = e
		}
	}
	if self["description"] != "NEW description" {
		t.Errorf("self description not updated: %v", self["description"])
	}
	if self["license"] != "UNLICENSED" || self["source"] != "./" {
		t.Errorf("self other fields not preserved: %v", self)
	}
	if third["description"] != "third party" {
		t.Errorf("third-party entry was modified: %v", third)
	}
	if out[len(out)-1] != '\n' {
		t.Error("expected trailing newline")
	}
}

func TestMergeMarketplaceEntry_MissingSelf(t *testing.T) {
	p := manifest.PluginBuild{Name: "absent", Marketplace: "m"}
	if _, err := MergeMarketplaceEntry([]byte(sampleMarketplace), p); err == nil ||
		!strings.Contains(err.Error(), "no marketplace entry") {
		t.Errorf("expected missing-entry error, got %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/plugin/ -run TestMergeMarketplaceEntry -v`
Expected: FAIL — `undefined: MergeMarketplaceEntry`.

- [ ] **Step 3: Implement**

```go
package plugin

import (
	"encoding/json"
	"fmt"

	"github.com/MHilhorst/ainfra/internal/manifest"
)

// marketplaceDoc preserves top-level key order and passes every plugin entry
// through as raw JSON, so non-self entries are emitted unchanged.
type marketplaceDoc struct {
	Name     string            `json:"name"`
	Owner    json.RawMessage   `json:"owner,omitempty"`
	Metadata json.RawMessage   `json:"metadata,omitempty"`
	Plugins  []json.RawMessage `json:"plugins"`
}

// MergeMarketplaceEntry updates only the plugins[] entry whose name matches
// p.Name (its `name` and `description`), preserving that entry's other fields
// and every other entry verbatim. Returns an error if no self-entry exists.
func MergeMarketplaceEntry(existing []byte, p manifest.PluginBuild) ([]byte, error) {
	var doc marketplaceDoc
	if err := json.Unmarshal(existing, &doc); err != nil {
		return nil, fmt.Errorf("plugin: parse marketplace.json: %w", err)
	}

	found := false
	for i, raw := range doc.Plugins {
		var probe struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(raw, &probe); err != nil {
			return nil, fmt.Errorf("plugin: parse marketplace entry: %w", err)
		}
		if probe.Name != p.Name {
			continue
		}
		found = true

		// Decode the self-entry into an ordered map-like via a generic map,
		// override the two synced fields, and re-marshal preserving the rest.
		var entry map[string]json.RawMessage
		if err := json.Unmarshal(raw, &entry); err != nil {
			return nil, err
		}
		entry["name"] = mustRaw(p.Name)
		entry["description"] = mustRaw(p.Description)
		merged, err := json.Marshal(entry)
		if err != nil {
			return nil, err
		}
		doc.Plugins[i] = merged
	}
	if !found {
		return nil, fmt.Errorf("plugin: no marketplace entry named %q in marketplace.json", p.Name)
	}

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

func mustRaw(s string) json.RawMessage {
	b, _ := json.Marshal(s)
	return b
}
```

> Note: the self-entry's key order is normalized (alphabetical) on update; non-self
> entries pass through unchanged. This is the one-time cosmetic normalization called
> out in the spec's dogfooding gate. Values are preserved.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/plugin/ -run TestMergeMarketplaceEntry -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/plugin/marketplace.go internal/plugin/marketplace_test.go
git commit -m "feat(plugin): merge self-entry into marketplace.json"
```

---

## Task 8: Release decision (drift guard)

**Files:**
- Create: `internal/plugin/release.go`
- Test: `internal/plugin/release_test.go`

- [ ] **Step 1: Write the failing test**

```go
package plugin

import "testing"

func TestDecide(t *testing.T) {
	// Unchanged content, no bump -> no-op.
	d, err := Decide("h", "h", "2.11.0", "")
	if err != nil || d.Action != ActionNoop {
		t.Fatalf("noop case: action=%q err=%v", d.Action, err)
	}

	// Changed content, no bump -> drift error.
	if _, err := Decide("new", "old", "2.11.0", ""); err == nil {
		t.Error("expected drift error when content changed without bump")
	}

	// Changed content, patch bump -> release.
	d, err = Decide("new", "old", "2.11.0", "patch")
	if err != nil {
		t.Fatal(err)
	}
	if d.Action != ActionRelease || d.NewVersion != "2.11.1" || d.OldVersion != "2.11.0" {
		t.Errorf("got %+v", d)
	}

	// Unchanged content but explicit bump -> still releases (metadata-only).
	d, err = Decide("h", "h", "2.11.0", "minor")
	if err != nil || d.Action != ActionRelease || d.NewVersion != "2.12.0" {
		t.Errorf("explicit bump on unchanged content: %+v err=%v", d, err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/plugin/ -run TestDecide -v`
Expected: FAIL — `undefined: Decide` / `undefined: ActionNoop`.

- [ ] **Step 3: Implement**

```go
package plugin

import "fmt"

// Release decision actions.
const (
	ActionNoop    = "noop"
	ActionRelease = "release"
)

// Decision is the outcome of evaluating a release request.
type Decision struct {
	Action     string
	OldVersion string
	NewVersion string
}

// Decide implements the release state machine. With no bump level: unchanged
// content is a no-op, changed content is a drift error. With a bump level it
// always releases, computing the next version.
func Decide(currentHash, baselineHash, baselineVersion, bumpLevel string) (Decision, error) {
	if bumpLevel == "" {
		if currentHash == baselineHash {
			return Decision{Action: ActionNoop, OldVersion: baselineVersion, NewVersion: baselineVersion}, nil
		}
		return Decision{}, fmt.Errorf(
			"plugin content changed since v%s but version not bumped; pass --patch, --minor, or --major",
			baselineVersion)
	}
	newV, err := Bump(baselineVersion, bumpLevel)
	if err != nil {
		return Decision{}, err
	}
	return Decision{Action: ActionRelease, OldVersion: baselineVersion, NewVersion: newV}, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/plugin/ -run TestDecide -v`
Expected: PASS.

- [ ] **Step 5: Run the whole plugin package**

Run: `go test ./internal/plugin/`
Expected: PASS (ok).

- [ ] **Step 6: Commit**

```bash
git add internal/plugin/release.go internal/plugin/release_test.go
git commit -m "feat(plugin): release decision with drift guard"
```

---

## Task 9: Wire the `ainfra plugin` command

**Files:**
- Create: `cmd/ainfra/cmd_plugin.go`
- Modify: `cmd/ainfra/main.go`
- Test: `cmd/ainfra/cmd_plugin_test.go`

This task assembles the pure pieces into `build` and `release`. The command:
- `build`: load manifest → read current version (from existing plugin.json, else lock baseline, else `0.0.0`) → write plugin.json + merged marketplace.json.
- `release`: load manifest → `claude plugin validate` → hash content → `Decide` → on release, bump + write both files + update lock baseline.

- [ ] **Step 1: Write the failing test**

```go
package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newPluginRepo writes a minimal repo with a plugin: block, a skill, an empty
// marketplace.json self-entry, and an ainfra.lock baseline at 1.0.0.
func newPluginRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	must := func(rel, body string) {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	must("ainfra.yaml", `version: 1
agent: claude-code
plugin:
  name: tvt-config
  description: "Team config"
  marketplace: trein-vertraging
  content: [ skills/ ]
`)
	must("skills/demo/SKILL.md", "---\ndescription: demo\n---\nbody\n")
	must(".claude-plugin/marketplace.json", `{
  "name": "trein-vertraging",
  "plugins": [
    { "name": "tvt-config", "source": "./", "description": "old" }
  ]
}`)
	must("ainfra.lock", `version: 1
plugin:
  name: tvt-config
  version: 1.0.0
  contentHash: deadbeef
`)
	return dir
}

func TestPlugin_ReleaseDriftGuard(t *testing.T) {
	dir := newPluginRepo(t)
	var errOut bytes.Buffer
	// Baseline hash "deadbeef" won't match real content -> drift, no bump flag.
	code := run([]string{"--chdir", dir, "plugin", "release"}, &bytes.Buffer{}, &errOut)
	if code == 0 {
		t.Fatal("expected non-zero exit on drift without bump")
	}
	if !strings.Contains(errOut.String(), "changed since v1.0.0") {
		t.Errorf("want drift message, got %q", errOut.String())
	}
}

func TestPlugin_ReleasePatch(t *testing.T) {
	dir := newPluginRepo(t)
	var out bytes.Buffer
	code := run([]string{"--chdir", dir, "plugin", "release", "--patch"}, &out, &bytes.Buffer{})
	if code != 0 {
		t.Fatalf("release --patch failed: code=%d out=%s", code, out.String())
	}

	// plugin.json written at 1.0.1.
	pj, err := os.ReadFile(filepath.Join(dir, ".claude-plugin", "plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(pj, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["version"] != "1.0.1" {
		t.Errorf("plugin.json version = %v want 1.0.1", doc["version"])
	}

	// lock baseline updated to 1.0.1 with a real hash.
	lock, _ := os.ReadFile(filepath.Join(dir, "ainfra.lock"))
	if !strings.Contains(string(lock), "version: 1.0.1") {
		t.Errorf("lock not updated: %s", lock)
	}
	if strings.Contains(string(lock), "deadbeef") {
		t.Errorf("lock still has stale hash: %s", lock)
	}

	// marketplace self-entry description synced.
	mk, _ := os.ReadFile(filepath.Join(dir, ".claude-plugin", "marketplace.json"))
	if !strings.Contains(string(mk), "Team config") {
		t.Errorf("marketplace description not synced: %s", mk)
	}
}
```

> The test relies on `claude plugin validate` succeeding. Step 3 makes validation
> tolerant when the `claude` binary is absent (treated as a skipped check with a
> warning) so tests and offline maintainers are not blocked. A present binary that
> returns an error still fails the release.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/ainfra/ -run TestPlugin -v`
Expected: FAIL — `plugin` is not a known command (and the test file won't compile until the command exists).

- [ ] **Step 3: Implement the command**

Create `cmd/ainfra/cmd_plugin.go`:

```go
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/MHilhorst/ainfra/internal/cli"
	"github.com/MHilhorst/ainfra/internal/lockfile"
	"github.com/MHilhorst/ainfra/internal/manifest"
	"github.com/MHilhorst/ainfra/internal/plugin"
	"github.com/MHilhorst/ainfra/internal/ui"
)

// newPluginCommand wires `ainfra plugin build|release`.
func newPluginCommand() *cli.Command {
	var patch, minor, major bool
	return &cli.Command{
		Name:      "plugin",
		Summary:   "Build and release this repo's own Claude Code plugin",
		UsageLine: "ainfra plugin <build|release> [--patch|--minor|--major]",
		Example:   "ainfra plugin release --patch",
		SetFlags: func(fs *flag.FlagSet) {
			fs.BoolVar(&patch, "patch", false, "bump the patch version on release")
			fs.BoolVar(&minor, "minor", false, "bump the minor version on release")
			fs.BoolVar(&major, "major", false, "bump the major version on release")
		},
		Run: func(ctx cli.Context) int {
			level := ""
			switch {
			case major:
				level = "major"
			case minor:
				level = "minor"
			case patch:
				level = "patch"
			}
			return runPlugin(ctx, level)
		},
	}
}

func runPlugin(ctx cli.Context, level string) int {
	errColor := ui.NewColorizer(ctx.Stderr, ctx.NoColor)
	if len(ctx.Args) < 1 {
		ui.RenderError(ctx.Stderr, errColor, errors.New("usage: ainfra plugin <build|release>"))
		return 2
	}
	action := ctx.Args[0]

	layers, err := manifest.LoadLayers(ctx.Dir)
	if err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}
	repo := layers[manifest.LayerRepo]
	if repo == nil || repo.Plugin == nil {
		ui.RenderError(ctx.Stderr, errColor, errors.New("no plugin: block in ainfra.yaml"))
		return 1
	}
	pb := *repo.Plugin

	switch action {
	case "build":
		version := currentPluginVersion(ctx.Dir, pb.Name)
		if err := writePluginFiles(ctx.Dir, pb, version); err != nil {
			ui.RenderError(ctx.Stderr, errColor, err)
			return 1
		}
		fmt.Fprintf(ctx.Stdout, "Built plugin %s at version %s.\n", pb.Name, version)
		return 0

	case "release":
		return runPluginRelease(ctx, pb, level, errColor)

	default:
		ui.RenderError(ctx.Stderr, errColor, fmt.Errorf("unknown plugin action %q (want build or release)", action))
		return 2
	}
}

func runPluginRelease(ctx cli.Context, pb manifest.PluginBuild, level string, errColor ui.Colorizer) int {
	// Validate the manifest with the claude CLI when available.
	if warn, err := validatePlugin(ctx.Dir); err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	} else if warn != "" {
		fmt.Fprintln(ctx.Stderr, warn)
	}

	hash, err := plugin.ContentHash(ctx.Dir, pb.ContentPaths())
	if err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}

	lockPath := filepath.Join(ctx.Dir, "ainfra.lock")
	lock, err := lockfile.Read(lockPath)
	if err != nil {
		ui.RenderError(ctx.Stderr, errColor, fmt.Errorf("ainfra.lock not found — run `ainfra lock` first"))
		return 1
	}
	base := lock.Plugin
	if base == nil {
		base = &lockfile.PluginBaseline{Name: pb.Name, Version: "0.0.0", ContentHash: ""}
	}

	decision, err := plugin.Decide(hash, base.ContentHash, base.Version, level)
	if err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}
	if decision.Action == plugin.ActionNoop {
		fmt.Fprintf(ctx.Stdout, "Nothing changed since v%s.\n", base.Version)
		return 0
	}

	if err := writePluginFiles(ctx.Dir, pb, decision.NewVersion); err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}
	lock.Plugin = &lockfile.PluginBaseline{Name: pb.Name, Version: decision.NewVersion, ContentHash: hash}
	if err := lockfile.Write(lockPath, lock); err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}
	fmt.Fprintf(ctx.Stdout, "Released %s %s -> %s.\n", pb.Name, decision.OldVersion, decision.NewVersion)
	return 0
}

// writePluginFiles renders plugin.json and merges the marketplace self-entry.
func writePluginFiles(dir string, pb manifest.PluginBuild, version string) error {
	pjPath := filepath.Join(dir, ".claude-plugin", "plugin.json")
	if err := os.MkdirAll(filepath.Dir(pjPath), 0o755); err != nil {
		return err
	}
	pj, err := plugin.RenderPluginJSON(pb, version)
	if err != nil {
		return err
	}
	if err := os.WriteFile(pjPath, pj, 0o644); err != nil {
		return err
	}

	mkPath := filepath.Join(dir, ".claude-plugin", "marketplace.json")
	existing, err := os.ReadFile(mkPath)
	if err != nil {
		return fmt.Errorf("plugin: read marketplace.json: %w", err)
	}
	merged, err := plugin.MergeMarketplaceEntry(existing, pb)
	if err != nil {
		return err
	}
	return os.WriteFile(mkPath, merged, 0o644)
}

// currentPluginVersion reads the version from an existing plugin.json, falling
// back to "0.0.0" when none exists.
func currentPluginVersion(dir, name string) string {
	raw, err := os.ReadFile(filepath.Join(dir, ".claude-plugin", "plugin.json"))
	if err != nil {
		return "0.0.0"
	}
	var doc struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil || doc.Version == "" {
		return "0.0.0"
	}
	return doc.Version
}

// validatePlugin runs `claude plugin validate` in dir. A missing claude binary
// returns a warning string (not an error) so offline maintainers and tests are
// not blocked; a present binary that fails returns an error.
func validatePlugin(dir string) (warn string, err error) {
	path, lookErr := exec.LookPath("claude")
	if lookErr != nil {
		return "warning: claude CLI not found; skipped `claude plugin validate`.", nil
	}
	cmd := exec.Command(path, "plugin", "validate")
	cmd.Dir = dir
	out, runErr := cmd.CombinedOutput()
	if runErr != nil {
		return "", fmt.Errorf("claude plugin validate failed: %s", string(out))
	}
	return "", nil
}
```

Then register it in `cmd/ainfra/main.go` next to the other `reg.Add(...)` calls:

```go
	reg.Add(newPluginCommand())
```

> If `ui.Colorizer` is not the exact exported type name, run
> `grep -n "func NewColorizer" internal/ui/*.go` and match the returned type in the
> `runPluginRelease` signature.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/ainfra/ -run TestPlugin -v`
Expected: PASS (both `TestPlugin_ReleaseDriftGuard` and `TestPlugin_ReleasePatch`).

- [ ] **Step 5: Build and run the whole suite**

Run: `go build ./... && go test ./...`
Expected: PASS across all packages.

- [ ] **Step 6: Commit**

```bash
git add cmd/ainfra/cmd_plugin.go cmd/ainfra/main.go cmd/ainfra/cmd_plugin_test.go
git commit -m "feat(cli): ainfra plugin build/release command"
```

---

## Task 10: Dogfood on claude-config (acceptance gate)

This task is run manually against the real `~/projects/tvt-nl/claude-config` repo; it is the spec's validation gate. It changes a different repo — make changes on a branch there and do not commit without the user's say-so.

**Files:**
- Modify (in claude-config): `ainfra.yaml` (add `plugin:` block), `ainfra.lock` (seed baseline).

- [ ] **Step 1: Build the updated ainfra binary**

Run: `go build -o /tmp/ainfra ./cmd/ainfra`
Expected: builds cleanly.

- [ ] **Step 2: Add the `plugin:` block to claude-config/ainfra.yaml**

Append (values copied from the existing `.claude-plugin/plugin.json`):

```yaml
plugin:
  name: tvt-config
  description: "Team configuration for TVT-NL and AirHelp rail projects. Skills, hooks, MCP servers for Laravel, Node.js, Python, Go, and Rust repos."
  marketplace: trein-vertraging
  author: { name: Trein-Vertraging, url: https://github.com/trein-vertraging }
  repository: https://github.com/trein-vertraging/claude-config
  license: UNLICENSED
  content: [ skills/, commands/, hooks/, .mcp.json ]
```

- [ ] **Step 3: Generate and compare (non-destructive check)**

Run:
```bash
cd ~/projects/tvt-nl/claude-config
cp .claude-plugin/plugin.json /tmp/plugin.before.json
/tmp/ainfra plugin build
python3 -c "import json;a=json.load(open('/tmp/plugin.before.json'));b=json.load(open('.claude-plugin/plugin.json'));print('SEMANTICALLY EQUAL' if a==b else 'DIFFERS');import sys;sys.exit(0 if a==b else 1)"
git --no-pager diff --stat .claude-plugin/
```
Expected: `SEMANTICALLY EQUAL`. The only `git diff` should be cosmetic (formatting/key order). If values differ, adjust the generator or the `plugin:` block until semantically equal before proceeding.

- [ ] **Step 4: Seed the lock baseline at the current version**

Add to `claude-config/ainfra.lock` (version copied from plugin.json, hash from a dry release):
```bash
cd ~/projects/tvt-nl/claude-config
/tmp/ainfra plugin release        # expect drift error printing the current hash, OR
                                  # "Nothing changed" if baseline already matches
```
If it reports drift, hand-seed the baseline `plugin:` block in `ainfra.lock` with `version: 2.11.0` and the printed hash, then re-run to confirm a clean "Nothing changed since v2.11.0".

- [ ] **Step 5: Exercise the guard and a real bump**

Run:
```bash
cd ~/projects/tvt-nl/claude-config
# touch a skill, expect the guard:
echo "" >> skills/using-ddev-laravel/SKILL.md
/tmp/ainfra plugin release        # expect: "content changed since v2.11.0 ... pass --patch ..."
/tmp/ainfra plugin release --patch
grep '"version"' .claude-plugin/plugin.json   # expect 2.11.1
git -C . checkout -- skills/using-ddev-laravel/SKILL.md  # revert the test edit
```
Expected: guard fires without a flag; `--patch` bumps to `2.11.1`, updates `plugin.json`, the marketplace self-entry, and the lock baseline.

- [ ] **Step 6: Report results to the user**

Summarize the diff from Step 3 and the bump from Step 5. Do **not** commit the claude-config changes unless the user asks. The version bump done in Step 5 was a test — leave claude-config at `2.11.0` (revert the plugin.json/lock bump) unless the user wants a real release.

---

## Self-Review

- **Spec coverage:** `plugin:` block (T1), validation (T2), lock baseline (T3), content hash (T4), semver (T5), plugin.json generation (T6), marketplace self-entry merge preserving third-party (T7), drift-guard state machine (T8), subcommand not in apply (T9), claude-config dogfood gate (T10). All spec sections map to a task.
- **Type consistency:** `manifest.PluginBuild`/`manifest.PluginAuthor`, `lockfile.PluginBaseline`, `plugin.ContentHash/Bump/RenderPluginJSON/MergeMarketplaceEntry/Decide`, and `Decision{Action,OldVersion,NewVersion}` with `ActionNoop`/`ActionRelease` are used identically across tasks.
- **Placeholders:** none; every code step is complete. Two callouts flag where to confirm a local name (`ui.Colorizer`, the `ValidateAll` call site) — these are verification notes, not placeholders.
```
