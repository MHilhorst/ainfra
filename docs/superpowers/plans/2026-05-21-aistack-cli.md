# aistack — Resolution Engine & `lock` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the resolution engine — manifest parsing, layer merge, template instantiation, dependency graph, port allocation, hashing — and the `aistack lock` command, proven end-to-end against `examples/multi-database/`.

**Architecture:** A pure pipeline. `manifest` loads and validates the three YAML layers. `resolve` instantiates templates, interpolates `${...}`, merges layers under the Option-C precedence rule, and allocates tool-owned ports. `graph` builds and topologically sorts the dependency graph. `lockfile` hashes resolved entries and reads/writes `ai-stack.lock`. `cmd/aistack` wires `lock`. Each package is pure and table-tested; no I/O outside `manifest` (read) and `lockfile` (write).

**Tech Stack:** Go 1.25, stdlib only except `gopkg.in/yaml.v3` for YAML. `go test` for tests.

**Scope note:** This plan delivers working software — `aistack lock` resolving the multi-database example. The channel *providers* (writing `.mcp.json`, installing CLI tools, generating tunnel scripts) and the `plan`/`apply`/`check`/`init` commands are a deliberate follow-up plan, written once the types below are concrete so it is not built on guessed signatures.

---

## File Structure

| File | Responsibility |
|------|----------------|
| `internal/manifest/types.go` | Go structs mirroring `ai-stack.yaml` (spec Phase 1). |
| `internal/manifest/load.go` | Parse YAML; load repo + personal + `extends` team layers. |
| `internal/manifest/validate.go` | Static validation: pinned versions, required params, secret bindings. |
| `internal/resolve/interpolate.go` | `${params|instance|secret|resolved.X}` expansion. |
| `internal/resolve/template.go` | Instantiate a template into channel + auxiliary entries. |
| `internal/resolve/merge.go` | Option-C layer precedence merge. |
| `internal/resolve/ports.go` | Allocate `kind: allocated-port` resolved fields. |
| `internal/graph/graph.go` | Build dependency graph; topological sort; cycle detection. |
| `internal/lockfile/types.go` | `ai-stack.lock` structs (spec Phase 2). |
| `internal/lockfile/hash.go` | Normalized semantic content hashing. |
| `internal/lockfile/io.go` | Read/write `ai-stack.lock` + `ai-stack.personal.lock`. |
| `cmd/aistack/main.go` | Wire the `lock` subcommand (modify existing). |

---

## Task 1: Add the YAML dependency and manifest types

**Files:**
- Modify: `go.mod`
- Create: `internal/manifest/types.go`
- Test: `internal/manifest/types_test.go`

- [ ] **Step 1: Add the dependency**

Run: `go get gopkg.in/yaml.v3@v3.0.1`
Expected: `go.mod` gains `require gopkg.in/yaml.v3 v3.0.1`.

- [ ] **Step 2: Write the failing test**

```go
package manifest

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestManifestUnmarshalsMultiDBShape(t *testing.T) {
	src := []byte(`
version: 1
cliTools:
  ssh:
    versionConstraint: ">=8.0"
templates:
  t:
    params:
      host: { type: string, required: true }
mcpServers:
  analytics-db:
    template: t
    params: { host: a.example }
`)
	var m Manifest
	if err := yaml.Unmarshal(src, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m.Version != 1 {
		t.Errorf("version = %d, want 1", m.Version)
	}
	if _, ok := m.CLITools["ssh"]; !ok {
		t.Error("cliTools.ssh missing")
	}
	inst := m.MCPServers["analytics-db"]
	if inst.Template != "t" {
		t.Errorf("template = %q, want t", inst.Template)
	}
	if inst.Params["host"] != "a.example" {
		t.Errorf("params.host = %v", inst.Params["host"])
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/manifest/`
Expected: FAIL — `undefined: Manifest`.

- [ ] **Step 4: Write the types**

```go
// Package manifest defines the ai-stack.yaml schema (spec/manifest-schema.md)
// and loads it from the three config layers.
package manifest

// Layer identifies which config layer a definition came from.
type Layer string

const (
	LayerTeam     Layer = "team"
	LayerRepo     Layer = "repo"
	LayerPersonal Layer = "personal"
)

// Manifest is one parsed ai-stack.yaml file (a single layer).
type Manifest struct {
	Version            int                          `yaml:"version"`
	Extends            []Source                     `yaml:"extends"`
	Preconditions      map[string]Precondition      `yaml:"preconditions"`
	CLITools           map[string]CLITool           `yaml:"cliTools"`
	BackgroundServices map[string]BackgroundService `yaml:"backgroundServices"`
	Secrets            map[string]Secret            `yaml:"secrets"`
	Templates          map[string]Template          `yaml:"templates"`
	MCPServers         map[string]MCPServer         `yaml:"mcpServers"`
}

// Source names a team/org layer to extend.
type Source struct {
	Source string `yaml:"source"`
}

// Precondition is a verify-only check (spec §6).
type Precondition struct {
	Description string         `yaml:"description"`
	Check       map[string]any `yaml:"check"`
	Remediation string         `yaml:"remediation"`
}

// CLITool is an installable substrate binary (spec §7).
type CLITool struct {
	VersionConstraint string                    `yaml:"versionConstraint"`
	Install           map[string]map[string]any `yaml:"install"`
	Check             map[string]any            `yaml:"check"`
	Overridable       bool                      `yaml:"overridable"`
}

// BackgroundService is a persistent process (spec §8).
type BackgroundService struct {
	ID        string         `yaml:"id"`
	Kind      string         `yaml:"kind"`
	Spec      map[string]any `yaml:"spec"`
	Requires  []Require      `yaml:"requires"`
	Lifecycle map[string]any `yaml:"lifecycle"`
	Check     map[string]any `yaml:"check"`
}

// Secret is a reference to a credential, never a value (spec §3).
type Secret struct {
	Mode    string `yaml:"mode"`
	Value   string `yaml:"value"`
	Ref     string `yaml:"ref"`
	Gateway string `yaml:"gateway"`
	Scope   string `yaml:"scope"`
}

// Param is a typed template input (spec §4.1).
type Param struct {
	Type     string `yaml:"type"`
	Required bool   `yaml:"required"`
	Default  any    `yaml:"default"`
}

// ResolvedField declares a tool-owned computed field (spec §4.3).
type ResolvedField struct {
	Kind string `yaml:"kind"`
}

// Template is a reusable channel-entry shape (spec §4.1).
type Template struct {
	Description string                   `yaml:"description"`
	Params      map[string]Param         `yaml:"params"`
	Secrets     map[string]TemplateSecret `yaml:"secrets"`
	Resolved    map[string]ResolvedField `yaml:"resolved"`
	Produces    Produces                 `yaml:"produces"`
}

// TemplateSecret declares a secret name the template body consumes.
type TemplateSecret struct {
	Required bool `yaml:"required"`
}

// Produces is what instantiating a template emits (spec §4.1).
type Produces struct {
	MCPServer         *MCPServer         `yaml:"mcpServer"`
	BackgroundService *BackgroundService `yaml:"backgroundService"`
}

// MCPServer is an MCP channel entry or template body (spec §5).
type MCPServer struct {
	Template    string         `yaml:"template"`
	Params      map[string]any `yaml:"params"`
	Secret      map[string]any `yaml:"secret"`
	Transport   string         `yaml:"transport"`
	Command     string         `yaml:"command"`
	Args        []string       `yaml:"args"`
	Version     string         `yaml:"version"`
	Env         map[string]string `yaml:"env"`
	Via         string         `yaml:"via"`
	Requires    []Require      `yaml:"requires"`
	Enabled     *bool          `yaml:"enabled"`
	Overridable bool           `yaml:"overridable"`
}

// Require is one dependency-graph edge (spec §9).
type Require struct {
	Service      string `yaml:"service"`
	CLITool      string `yaml:"cliTool"`
	Precondition string `yaml:"precondition"`
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/manifest/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum internal/manifest/types.go internal/manifest/types_test.go
git commit -m "Add manifest types and YAML parsing"
```

---

## Task 2: Load and merge the three config layers

**Files:**
- Create: `internal/manifest/load.go`
- Test: `internal/manifest/load_test.go`

- [ ] **Step 1: Write the failing test**

```go
package manifest

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadLayersTagsEachLayer(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("ai-stack.yaml", "version: 1\nmcpServers:\n  repo-srv: {command: x}\n")
	write("ai-stack.personal.yaml", "version: 1\nmcpServers:\n  mine: {command: y}\n")

	layers, err := LoadLayers(dir)
	if err != nil {
		t.Fatalf("LoadLayers: %v", err)
	}
	if got := layers[LayerRepo].MCPServers["repo-srv"].Command; got != "x" {
		t.Errorf("repo layer command = %q", got)
	}
	if got := layers[LayerPersonal].MCPServers["mine"].Command; got != "y" {
		t.Errorf("personal layer command = %q", got)
	}
}

func TestLoadLayersPersonalOptional(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "ai-stack.yaml"), []byte("version: 1\n"), 0o644)
	layers, err := LoadLayers(dir)
	if err != nil {
		t.Fatalf("LoadLayers: %v", err)
	}
	if _, ok := layers[LayerPersonal]; ok {
		t.Error("personal layer should be absent when file missing")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/manifest/ -run TestLoadLayers`
Expected: FAIL — `undefined: LoadLayers`.

- [ ] **Step 3: Write the implementation**

```go
package manifest

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// LoadLayers reads the repo and (optional) personal manifests from dir.
// The team layer (via extends:) is resolved by ResolveExtends in a later task;
// LoadLayers returns the directly-present layers only.
func LoadLayers(dir string) (map[Layer]*Manifest, error) {
	out := map[Layer]*Manifest{}
	repo, err := loadFile(filepath.Join(dir, "ai-stack.yaml"))
	if err != nil {
		return nil, err
	}
	out[LayerRepo] = repo

	personal, err := loadFile(filepath.Join(dir, "ai-stack.personal.yaml"))
	if err == nil {
		out[LayerPersonal] = personal
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	return out, nil
}

func loadFile(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if m.Version != 1 {
		return nil, fmt.Errorf("%s: unsupported version %d (want 1)", path, m.Version)
	}
	return &m, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/manifest/ -run TestLoadLayers`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/manifest/load.go internal/manifest/load_test.go
git commit -m "Load repo and personal manifest layers"
```

---

## Task 3: Validate the manifest statically

**Files:**
- Create: `internal/manifest/validate.go`
- Test: `internal/manifest/validate_test.go`

Enforces the rules from spec §5.1 (pinned versions), §4 (required params, declared secret bindings).

- [ ] **Step 1: Write the failing test**

```go
package manifest

import (
	"strings"
	"testing"
)

func TestValidateRejectsFloatingMCPVersion(t *testing.T) {
	m := &Manifest{Version: 1, MCPServers: map[string]MCPServer{
		"s": {Command: "npx", Args: []string{"-y", "pkg@latest"}},
	}}
	err := Validate(m)
	if err == nil || !strings.Contains(err.Error(), "pin an exact version") {
		t.Fatalf("want pinned-version error, got %v", err)
	}
}

func TestValidateAcceptsPinnedMCPVersion(t *testing.T) {
	m := &Manifest{Version: 1, MCPServers: map[string]MCPServer{
		"s": {Command: "npx", Args: []string{"-y", "pkg"}, Version: "1.2.3"},
	}}
	if err := Validate(m); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateRejectsUnknownTemplate(t *testing.T) {
	m := &Manifest{Version: 1, MCPServers: map[string]MCPServer{
		"s": {Template: "missing"},
	}}
	err := Validate(m)
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("want unknown-template error, got %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/manifest/ -run TestValidate`
Expected: FAIL — `undefined: Validate`.

- [ ] **Step 3: Write the implementation**

```go
package manifest

import "fmt"

// packageLaunchers are commands that launch a server from a package registry;
// such servers must pin an exact version (spec §5.1).
var packageLaunchers = map[string]bool{"npx": true, "uvx": true, "pipx": true}

// Validate runs static checks on a single resolved manifest.
func Validate(m *Manifest) error {
	for id, srv := range m.MCPServers {
		if srv.Template != "" {
			if _, ok := m.Templates[srv.Template]; !ok {
				return fmt.Errorf("mcpServers.%s: unknown template %q", id, srv.Template)
			}
			continue
		}
		if packageLaunchers[srv.Command] && srv.Version == "" {
			return fmt.Errorf("mcpServers.%s: package-launched servers must pin an exact version", id)
		}
	}
	for id, tmpl := range m.Templates {
		if srv := tmpl.Produces.MCPServer; srv != nil {
			if packageLaunchers[srv.Command] && srv.Version == "" {
				return fmt.Errorf("templates.%s: package-launched servers must pin an exact version", id)
			}
		}
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/manifest/ -run TestValidate`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/manifest/validate.go internal/manifest/validate_test.go
git commit -m "Validate manifest: pinned versions and template references"
```

---

## Task 4: Interpolation engine

**Files:**
- Create: `internal/resolve/interpolate.go`
- Test: `internal/resolve/interpolate_test.go`

- [ ] **Step 1: Write the failing test**

```go
package resolve

import "testing"

func TestInterpolateNamespaces(t *testing.T) {
	scope := Scope{
		Params:   map[string]any{"host": "db.example"},
		Instance: map[string]any{"id": "analytics-db"},
		Resolved: map[string]any{"tunnelPort": 13306},
		Secret:   map[string]any{"pw": "<secret:pw>"},
	}
	cases := map[string]string{
		"${params.host}":            "db.example",
		"${instance.id}-tunnel":     "analytics-db-tunnel",
		"port ${resolved.tunnelPort}": "port 13306",
		"${secret.pw}":              "<secret:pw>",
	}
	for in, want := range cases {
		got, err := Interpolate(in, scope)
		if err != nil {
			t.Fatalf("Interpolate(%q): %v", in, err)
		}
		if got != want {
			t.Errorf("Interpolate(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestInterpolateUnknownReferenceErrors(t *testing.T) {
	_, err := Interpolate("${params.nope}", Scope{Params: map[string]any{}})
	if err == nil {
		t.Fatal("want error for unknown reference")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/resolve/ -run TestInterpolate`
Expected: FAIL — `undefined: Scope`.

- [ ] **Step 3: Write the implementation**

```go
// Package resolve turns a layered manifest into fully resolved state:
// templates instantiated, ${...} expanded, layers merged, ports allocated.
package resolve

import (
	"fmt"
	"regexp"
	"strings"
)

// Scope holds the four interpolation namespaces (spec §4.4).
type Scope struct {
	Params   map[string]any
	Instance map[string]any
	Resolved map[string]any
	Secret   map[string]any
}

var refPattern = regexp.MustCompile(`\$\{([a-zA-Z]+)\.([a-zA-Z0-9_]+)\}`)

// Interpolate expands every ${namespace.key} in s against scope.
func Interpolate(s string, scope Scope) (string, error) {
	var bad error
	out := refPattern.ReplaceAllStringFunc(s, func(m string) string {
		g := refPattern.FindStringSubmatch(m)
		ns, key := g[1], g[2]
		table, ok := map[string]map[string]any{
			"params": scope.Params, "instance": scope.Instance,
			"resolved": scope.Resolved, "secret": scope.Secret,
		}[ns]
		if !ok {
			bad = fmt.Errorf("unknown namespace %q in %q", ns, m)
			return m
		}
		v, ok := table[key]
		if !ok {
			bad = fmt.Errorf("unknown reference %q", m)
			return m
		}
		return fmt.Sprintf("%v", v)
	})
	if bad != nil {
		return "", bad
	}
	return out, nil
}

// InterpolateMap applies Interpolate to every string value in m, recursively.
func InterpolateMap(m map[string]any, scope Scope) (map[string]any, error) {
	out := make(map[string]any, len(m))
	for k, v := range m {
		nv, err := interpolateValue(v, scope)
		if err != nil {
			return nil, err
		}
		out[k] = nv
	}
	return out, nil
}

func interpolateValue(v any, scope Scope) (any, error) {
	switch t := v.(type) {
	case string:
		return Interpolate(t, scope)
	case map[string]any:
		return InterpolateMap(t, scope)
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			nv, err := interpolateValue(e, scope)
			if err != nil {
				return nil, err
			}
			out[i] = nv
		}
		return out, nil
	default:
		return v, nil
	}
}

var _ = strings.TrimSpace
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/resolve/ -run TestInterpolate`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/resolve/interpolate.go internal/resolve/interpolate_test.go
git commit -m "Add ${...} interpolation engine"
```

---

## Task 5: Template instantiation

**Files:**
- Create: `internal/resolve/template.go`
- Test: `internal/resolve/template_test.go`

Produces, from a template + instance, the resolved channel entry and any auxiliary background services. Tool-owned `resolved` fields are passed in (allocated in Task 8).

- [ ] **Step 1: Write the failing test**

```go
package resolve

import (
	"testing"

	"github.com/MHilhorst/aistack/internal/manifest"
)

func TestInstantiateProducesNamespacedService(t *testing.T) {
	tmpl := manifest.Template{
		Params:   map[string]manifest.Param{"host": {Type: "string", Required: true}},
		Secrets:  map[string]manifest.TemplateSecret{"pw": {Required: true}},
		Resolved: map[string]manifest.ResolvedField{"port": {Kind: "allocated-port"}},
		Produces: manifest.Produces{
			MCPServer: &manifest.MCPServer{
				Command: "npx", Version: "1.0.0",
				Env:      map[string]string{"H": "${params.host}", "P": "${resolved.port}"},
				Requires: []manifest.Require{{Service: "${instance.id}-tunnel"}},
			},
			BackgroundService: &manifest.BackgroundService{
				ID: "${instance.id}-tunnel", Kind: "ssh-tunnel",
			},
		},
	}
	inst := manifest.MCPServer{
		Template: "t",
		Params:   map[string]any{"host": "db.example"},
		Secret:   map[string]any{"pw": map[string]any{"ref": "op://x"}},
	}
	got, err := Instantiate("analytics-db", inst, tmpl, map[string]any{"port": 13306})
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	if got.MCPServer.Env["H"] != "db.example" || got.MCPServer.Env["P"] != "13306" {
		t.Errorf("env not interpolated: %+v", got.MCPServer.Env)
	}
	if got.MCPServer.Requires[0].Service != "analytics-db-tunnel" {
		t.Errorf("requires not interpolated: %+v", got.MCPServer.Requires)
	}
	if got.Service.ID != "analytics-db-tunnel" {
		t.Errorf("service id = %q", got.Service.ID)
	}
}

func TestInstantiateRejectsMissingRequiredParam(t *testing.T) {
	tmpl := manifest.Template{Params: map[string]manifest.Param{"host": {Required: true}}}
	_, err := Instantiate("x", manifest.MCPServer{Template: "t"}, tmpl, nil)
	if err == nil {
		t.Fatal("want error for missing required param")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/resolve/ -run TestInstantiate`
Expected: FAIL — `undefined: Instantiate`.

- [ ] **Step 3: Write the implementation**

```go
package resolve

import (
	"fmt"

	"github.com/MHilhorst/aistack/internal/manifest"
)

// Instance is the resolved output of instantiating one template.
type Instance struct {
	ID        string
	MCPServer *manifest.MCPServer
	Service   *manifest.BackgroundService
}

// Instantiate expands a template for one instance. resolved holds the
// tool-owned field values (allocated in AllocatePorts).
func Instantiate(id string, inst manifest.MCPServer, tmpl manifest.Template, resolved map[string]any) (Instance, error) {
	params := map[string]any{}
	for name, p := range tmpl.Params {
		if v, ok := inst.Params[name]; ok {
			params[name] = v
		} else if p.Default != nil {
			params[name] = p.Default
		} else if p.Required {
			return Instance{}, fmt.Errorf("%s: missing required param %q", id, name)
		}
	}
	secret := map[string]any{}
	for name := range tmpl.Secrets {
		secret[name] = fmt.Sprintf("<secret:%s.%s>", id, name)
	}
	scope := Scope{
		Params:   params,
		Instance: map[string]any{"id": id},
		Resolved: resolved,
		Secret:   secret,
	}

	out := Instance{ID: id}
	if src := tmpl.Produces.MCPServer; src != nil {
		srv := *src
		srv.Env = map[string]string{}
		for k, v := range src.Env {
			ev, err := Interpolate(v, scope)
			if err != nil {
				return Instance{}, err
			}
			srv.Env[k] = ev
		}
		srv.Requires = interpolateRequires(src.Requires, scope)
		out.MCPServer = &srv
	}
	if src := tmpl.Produces.BackgroundService; src != nil {
		svc := *src
		bid, err := Interpolate(src.ID, scope)
		if err != nil {
			return Instance{}, err
		}
		svc.ID = bid
		spec, err := InterpolateMap(src.Spec, scope)
		if err != nil {
			return Instance{}, err
		}
		svc.Spec = spec
		svc.Requires = interpolateRequires(src.Requires, scope)
		out.Service = &svc
	}
	return out, nil
}

func interpolateRequires(reqs []manifest.Require, scope Scope) []manifest.Require {
	out := make([]manifest.Require, len(reqs))
	for i, r := range reqs {
		r.Service, _ = Interpolate(r.Service, scope)
		r.CLITool, _ = Interpolate(r.CLITool, scope)
		r.Precondition, _ = Interpolate(r.Precondition, scope)
		out[i] = r
	}
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/resolve/ -run TestInstantiate`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/resolve/template.go internal/resolve/template_test.go
git commit -m "Instantiate templates into resolved channel entries"
```

---

## Task 6: Option-C precedence merge

**Files:**
- Create: `internal/resolve/merge.go`
- Test: `internal/resolve/merge_test.go`

- [ ] **Step 1: Write the failing test**

```go
package resolve

import "testing"

func TestMergeHigherLayerWins(t *testing.T) {
	team := map[string]Entry{"srv": {Value: "team", Overridable: false}}
	personal := map[string]Entry{"srv": {Value: "personal"}}
	merged, err := Merge([]LayerEntries{
		{Layer: "team", Entries: team},
		{Layer: "personal", Entries: personal},
	})
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if merged["srv"].Value != "team" {
		t.Errorf("got %q, want team (non-overridable wins)", merged["srv"].Value)
	}
}

func TestMergeOverridableLetsLowerLayerWin(t *testing.T) {
	team := map[string]Entry{"srv": {Value: "team", Overridable: true}}
	personal := map[string]Entry{"srv": {Value: "personal"}}
	merged, _ := Merge([]LayerEntries{
		{Layer: "team", Entries: team},
		{Layer: "personal", Entries: personal},
	})
	if merged["srv"].Value != "personal" {
		t.Errorf("got %q, want personal (overridable)", merged["srv"].Value)
	}
}

func TestMergeAddsUniqueEntries(t *testing.T) {
	merged, _ := Merge([]LayerEntries{
		{Layer: "repo", Entries: map[string]Entry{"a": {Value: "1"}}},
		{Layer: "personal", Entries: map[string]Entry{"b": {Value: "2"}}},
	})
	if len(merged) != 2 {
		t.Errorf("want 2 entries, got %d", len(merged))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/resolve/ -run TestMerge`
Expected: FAIL — `undefined: Entry`.

- [ ] **Step 3: Write the implementation**

```go
package resolve

// Entry is one channel entry as it enters the merge. Value is an opaque
// payload (the caller stores the real config); Merge only arbitrates winners.
type Entry struct {
	Value       any
	Overridable bool
	Layer       string
}

// LayerEntries is one layer's entries, in descending authority order
// when passed to Merge (team first, personal last).
type LayerEntries struct {
	Layer   string
	Entries map[string]Entry
}

// Merge applies the Option-C precedence rule (spec §1): a higher-authority
// layer wins unless its entry is Overridable, in which case the next
// lower-authority layer's entry replaces it.
func Merge(layers []LayerEntries) (map[string]Entry, error) {
	out := map[string]Entry{}
	for _, layer := range layers {
		for id, e := range layer.Entries {
			e.Layer = layer.Layer
			cur, exists := out[id]
			if !exists {
				out[id] = e
				continue
			}
			if cur.Overridable {
				out[id] = e // sanctioned override by lower layer
			}
			// else: higher-authority non-overridable entry stands.
		}
	}
	return out, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/resolve/ -run TestMerge`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/resolve/merge.go internal/resolve/merge_test.go
git commit -m "Add Option-C layer precedence merge"
```

---

## Task 7: Dependency graph and topological sort

**Files:**
- Create: `internal/graph/graph.go`
- Test: `internal/graph/graph_test.go`

- [ ] **Step 1: Write the failing test**

```go
package graph

import "testing"

func TestTopoSortLeavesFirst(t *testing.T) {
	g := New()
	g.AddNode("mcp")
	g.AddNode("tunnel")
	g.AddNode("ssh")
	g.AddEdge("mcp", "tunnel")  // mcp requires tunnel
	g.AddEdge("tunnel", "ssh")  // tunnel requires ssh

	order, err := g.TopoSort()
	if err != nil {
		t.Fatalf("TopoSort: %v", err)
	}
	pos := map[string]int{}
	for i, n := range order {
		pos[n] = i
	}
	if !(pos["ssh"] < pos["tunnel"] && pos["tunnel"] < pos["mcp"]) {
		t.Errorf("order not leaves-first: %v", order)
	}
}

func TestTopoSortDetectsCycle(t *testing.T) {
	g := New()
	g.AddNode("a")
	g.AddNode("b")
	g.AddEdge("a", "b")
	g.AddEdge("b", "a")
	if _, err := g.TopoSort(); err == nil {
		t.Fatal("want cycle error")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/graph/`
Expected: FAIL — `undefined: New`.

- [ ] **Step 3: Write the implementation**

```go
// Package graph builds and orders the aistack dependency graph (spec §9).
package graph

import (
	"fmt"
	"sort"
)

// Graph is a dependency graph. An edge from -> to means "from requires to".
type Graph struct {
	nodes map[string]bool
	deps  map[string][]string
}

// New returns an empty graph.
func New() *Graph {
	return &Graph{nodes: map[string]bool{}, deps: map[string][]string{}}
}

// AddNode registers a node. Idempotent.
func (g *Graph) AddNode(id string) { g.nodes[id] = true }

// AddEdge records that from requires to.
func (g *Graph) AddEdge(from, to string) { g.deps[from] = append(g.deps[from], to) }

// TopoSort returns nodes leaves-first (dependencies before dependents).
// It errors on a cycle or an edge to an unknown node.
func (g *Graph) TopoSort() ([]string, error) {
	const (
		white = 0 // unvisited
		gray  = 1 // on the current DFS stack
		black = 2 // finished
	)
	state := map[string]int{}
	var order []string
	var visit func(string) error
	visit = func(n string) error {
		switch state[n] {
		case gray:
			return fmt.Errorf("dependency cycle through %q", n)
		case black:
			return nil
		}
		state[n] = gray
		deps := append([]string(nil), g.deps[n]...)
		sort.Strings(deps) // deterministic output
		for _, d := range deps {
			if !g.nodes[d] {
				return fmt.Errorf("node %q requires unknown node %q", n, d)
			}
			if err := visit(d); err != nil {
				return err
			}
		}
		state[n] = black
		order = append(order, n)
		return nil
	}
	ids := make([]string, 0, len(g.nodes))
	for n := range g.nodes {
		ids = append(ids, n)
	}
	sort.Strings(ids)
	for _, n := range ids {
		if err := visit(n); err != nil {
			return nil, err
		}
	}
	return order, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/graph/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/graph/graph.go internal/graph/graph_test.go
git commit -m "Add dependency graph with topological sort and cycle detection"
```

---

## Task 8: Sticky port allocation

**Files:**
- Create: `internal/resolve/ports.go`
- Test: `internal/resolve/ports_test.go`

- [ ] **Step 1: Write the failing test**

```go
package resolve

import "testing"

func TestAllocatePortsAreDistinctAndStable(t *testing.T) {
	requests := []PortRequest{
		{Instance: "analytics-db", Field: "tunnelPort"},
		{Instance: "billing-db", Field: "tunnelPort"},
	}
	// No prior allocations: fresh allocation from the base.
	got, err := AllocatePorts(requests, nil, 13306)
	if err != nil {
		t.Fatalf("AllocatePorts: %v", err)
	}
	if got["analytics-db"]["tunnelPort"] == got["billing-db"]["tunnelPort"] {
		t.Error("ports collided")
	}

	// Prior allocation in the lock must be reused verbatim.
	prior := map[string]map[string]int{"analytics-db": {"tunnelPort": 19999}}
	got2, _ := AllocatePorts(requests, prior, 13306)
	if got2["analytics-db"]["tunnelPort"] != 19999 {
		t.Errorf("sticky port not reused: %d", got2["analytics-db"]["tunnelPort"])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/resolve/ -run TestAllocatePorts`
Expected: FAIL — `undefined: PortRequest`.

- [ ] **Step 3: Write the implementation**

```go
package resolve

import "sort"

// PortRequest names one allocated-port resolved field that needs a value.
type PortRequest struct {
	Instance string
	Field    string
}

// AllocatePorts assigns a distinct local port to every request. A request
// already present in prior (the lockfile's recorded allocations) keeps its
// recorded port — making ports sticky across runs. Fresh requests take the
// lowest free port at or above base. No human ever types a port (spec §4.3).
func AllocatePorts(reqs []PortRequest, prior map[string]map[string]int, base int) (map[string]map[string]int, error) {
	out := map[string]map[string]int{}
	used := map[int]bool{}
	for _, fields := range prior {
		for _, p := range fields {
			used[p] = true
		}
	}
	sorted := append([]PortRequest(nil), reqs...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Instance != sorted[j].Instance {
			return sorted[i].Instance < sorted[j].Instance
		}
		return sorted[i].Field < sorted[j].Field
	})
	for _, r := range sorted {
		if out[r.Instance] == nil {
			out[r.Instance] = map[string]int{}
		}
		if p, ok := prior[r.Instance][r.Field]; ok {
			out[r.Instance][r.Field] = p
			continue
		}
		p := base
		for used[p] {
			p++
		}
		used[p] = true
		out[r.Instance][r.Field] = p
	}
	return out, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/resolve/ -run TestAllocatePorts`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/resolve/ports.go internal/resolve/ports_test.go
git commit -m "Add sticky tool-owned port allocation"
```

---

## Task 9: Normalized content hashing

**Files:**
- Create: `internal/lockfile/hash.go`
- Test: `internal/lockfile/hash_test.go`

- [ ] **Step 1: Write the failing test**

```go
package lockfile

import "testing"

func TestContentHashIsKeyOrderIndependent(t *testing.T) {
	a := map[string]any{"x": 1, "y": 2}
	b := map[string]any{"y": 2, "x": 1}
	if ContentHash(a) != ContentHash(b) {
		t.Error("hash must not depend on map key order")
	}
}

func TestContentHashChangesWithContent(t *testing.T) {
	a := map[string]any{"x": 1}
	b := map[string]any{"x": 2}
	if ContentHash(a) == ContentHash(b) {
		t.Error("hash must change when content changes")
	}
}

func TestContentHashHasPrefix(t *testing.T) {
	if got := ContentHash(map[string]any{}); got[:7] != "sha256:" {
		t.Errorf("hash %q missing sha256: prefix", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/lockfile/ -run TestContentHash`
Expected: FAIL — `undefined: ContentHash`.

- [ ] **Step 3: Write the implementation**

```go
// Package lockfile reads, writes, and hashes ai-stack.lock (spec Phase 2).
package lockfile

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// ContentHash returns a sha256: hash of v in a normalized form: map keys
// sorted, so cosmetic ordering differences are never false drift (spec §5).
func ContentHash(v any) string {
	var b strings.Builder
	writeNormalized(&b, v)
	sum := sha256.Sum256([]byte(b.String()))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func writeNormalized(b *strings.Builder, v any) {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		b.WriteByte('{')
		for _, k := range keys {
			b.WriteString(k)
			b.WriteByte(':')
			writeNormalized(b, t[k])
			b.WriteByte(',')
		}
		b.WriteByte('}')
	case []any:
		b.WriteByte('[')
		for _, e := range t {
			writeNormalized(b, e)
			b.WriteByte(',')
		}
		b.WriteByte(']')
	default:
		fmt.Fprintf(b, "%v", t)
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/lockfile/ -run TestContentHash`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/lockfile/hash.go internal/lockfile/hash_test.go
git commit -m "Add normalized semantic content hashing"
```

---

## Task 10: Lockfile types and I/O

**Files:**
- Create: `internal/lockfile/types.go`
- Create: `internal/lockfile/io.go`
- Test: `internal/lockfile/io_test.go`

- [ ] **Step 1: Write the failing test**

```go
package lockfile

import (
	"path/filepath"
	"testing"
)

func TestWriteThenReadRoundTrips(t *testing.T) {
	dir := t.TempDir()
	lock := &Lock{
		Version:     1,
		GeneratedAt: "2026-05-21T00:00:00Z",
		Entries: Entries{MCPServers: map[string]Entry{
			"analytics-db": {Layer: "repo", ContentHash: "sha256:abc",
				Resolved: map[string]any{"tunnelPort": 13306}},
		}},
	}
	path := filepath.Join(dir, "ai-stack.lock")
	if err := Write(path, lock); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.Entries.MCPServers["analytics-db"].ContentHash != "sha256:abc" {
		t.Errorf("round-trip lost data: %+v", got)
	}
}

func TestReadMissingFileReturnsEmptyLock(t *testing.T) {
	got, err := Read(filepath.Join(t.TempDir(), "absent.lock"))
	if err != nil {
		t.Fatalf("Read of missing file should not error: %v", err)
	}
	if got.Version != 1 || len(got.Entries.MCPServers) != 0 {
		t.Errorf("want empty v1 lock, got %+v", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/lockfile/ -run TestWrite -run TestRead`
Expected: FAIL — `undefined: Lock`.

- [ ] **Step 3: Write the types**

```go
// (internal/lockfile/types.go)
package lockfile

// Lock is one ai-stack.lock file (spec Phase 2).
type Lock struct {
	Version      int     `yaml:"version"`
	GeneratedAt  string  `yaml:"generatedAt"`
	ManifestHash string  `yaml:"manifestHash"`
	Entries      Entries `yaml:"entries"`
}

// Entries groups lock entries by channel.
type Entries struct {
	MCPServers         map[string]Entry `yaml:"mcpServers"`
	BackgroundServices map[string]Entry `yaml:"backgroundServices"`
	CLITools           map[string]Entry `yaml:"cliTools"`
}

// Entry is one resolved lock entry.
type Entry struct {
	Layer           string         `yaml:"layer"`
	FromTemplate    string         `yaml:"fromTemplate,omitempty"`
	Resolved        map[string]any `yaml:"resolved,omitempty"`
	Version         string         `yaml:"version,omitempty"`
	Integrity       string         `yaml:"integrity,omitempty"`
	ToolsetHash     string         `yaml:"toolsetHash,omitempty"`
	Constraint      string         `yaml:"constraint,omitempty"`
	ResolvedVersion string         `yaml:"resolvedVersion,omitempty"`
	ContentHash     string         `yaml:"contentHash"`
}
```

- [ ] **Step 4: Write the I/O**

```go
// (internal/lockfile/io.go)
package lockfile

import (
	"os"

	"gopkg.in/yaml.v3"
)

// Read loads a lockfile. A missing file is not an error: it returns an empty
// v1 lock, so a first run resolves cleanly (spec §7 — both lock files optional).
func Read(path string) (*Lock, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &Lock{Version: 1, Entries: Entries{
			MCPServers:         map[string]Entry{},
			BackgroundServices: map[string]Entry{},
			CLITools:           map[string]Entry{},
		}}, nil
	}
	if err != nil {
		return nil, err
	}
	var l Lock
	if err := yaml.Unmarshal(data, &l); err != nil {
		return nil, err
	}
	return &l, nil
}

// Write serializes a lock to path with a do-not-edit header.
func Write(path string, l *Lock) error {
	data, err := yaml.Marshal(l)
	if err != nil {
		return err
	}
	header := "# Generated by aistack. Do not edit by hand.\n"
	return os.WriteFile(path, append([]byte(header), data...), 0o644)
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/lockfile/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/lockfile/types.go internal/lockfile/io.go internal/lockfile/io_test.go
git commit -m "Add lockfile types and read/write I/O"
```

---

## Task 11: Wire the `aistack lock` command

**Files:**
- Create: `internal/resolve/pipeline.go`
- Create: `cmd/aistack/lock.go`
- Modify: `cmd/aistack/main.go:40-43` (the `case "init", ...` arm)
- Test: `internal/resolve/pipeline_test.go`

The pipeline ties Tasks 2–10 together: load layers → validate → instantiate templates → allocate ports → build graph (fail on cycle) → hash entries → write both lock files.

- [ ] **Step 1: Write the failing test**

```go
package resolve

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLockPipelineOnMultiDBExample(t *testing.T) {
	dir := t.TempDir()
	// Minimal two-instance manifest exercising the full pipeline.
	manifestYAML := `version: 1
cliTools:
  ssh: { versionConstraint: ">=8.0" }
templates:
  tun:
    params: { host: { type: string, required: true } }
    resolved: { port: { kind: allocated-port } }
    produces:
      mcpServer:
        command: npx
        version: "1.0.0"
        env: { P: "${resolved.port}" }
        requires: [ { service: "${instance.id}-tunnel" } ]
      backgroundService:
        id: "${instance.id}-tunnel"
        kind: ssh-tunnel
        requires: [ { cliTool: ssh } ]
mcpServers:
  db-a: { template: tun, params: { host: a.example } }
  db-b: { template: tun, params: { host: b.example } }
`
	os.WriteFile(filepath.Join(dir, "ai-stack.yaml"), []byte(manifestYAML), 0o644)

	if err := RunLock(dir); err != nil {
		t.Fatalf("RunLock: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "ai-stack.lock"))
	if err != nil {
		t.Fatalf("lock not written: %v", err)
	}
	out := string(data)
	for _, want := range []string{"db-a", "db-b", "tunnelPort: 13306", "tunnelPort: 13307", "contentHash:"} {
		if !contains(out, want) {
			t.Errorf("lock missing %q\n---\n%s", want, out)
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/resolve/ -run TestLockPipeline`
Expected: FAIL — `undefined: RunLock`.

- [ ] **Step 3: Write the pipeline**

```go
// (internal/resolve/pipeline.go)
package resolve

import (
	"fmt"
	"path/filepath"
	"sort"
	"time"

	"github.com/MHilhorst/aistack/internal/graph"
	"github.com/MHilhorst/aistack/internal/lockfile"
	"github.com/MHilhorst/aistack/internal/manifest"
)

const portBase = 13306

// RunLock executes the full resolve pipeline for the repo at dir and writes
// ai-stack.lock (team+repo entries) and ai-stack.personal.lock (personal).
func RunLock(dir string) error {
	layers, err := manifest.LoadLayers(dir)
	if err != nil {
		return err
	}
	for _, m := range layers {
		if err := manifest.Validate(m); err != nil {
			return err
		}
	}

	prior, err := lockfile.Read(filepath.Join(dir, "ai-stack.lock"))
	if err != nil {
		return err
	}
	priorPorts := portsFromLock(prior)

	// Collect every templated instance across layers, tagged with its layer.
	type tagged struct {
		id    string
		layer manifest.Layer
		inst  manifest.MCPServer
		tmpl  manifest.Template
	}
	var insts []tagged
	var portReqs []PortRequest
	for _, layerName := range []manifest.Layer{manifest.LayerTeam, manifest.LayerRepo, manifest.LayerPersonal} {
		m, ok := layers[layerName]
		if !ok {
			continue
		}
		for id, srv := range m.MCPServers {
			if srv.Template == "" {
				continue
			}
			tmpl := m.Templates[srv.Template]
			insts = append(insts, tagged{id, layerName, srv, tmpl})
			for field, rf := range tmpl.Resolved {
				if rf.Kind == "allocated-port" {
					portReqs = append(portReqs, PortRequest{Instance: id, Field: field})
				}
			}
		}
	}
	sort.Slice(insts, func(i, j int) bool { return insts[i].id < insts[j].id })

	ports, err := AllocatePorts(portReqs, priorPorts, portBase)
	if err != nil {
		return err
	}

	g := graph.New()
	lock := &lockfile.Lock{Version: 1, GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Entries: lockfile.Entries{
			MCPServers:         map[string]lockfile.Entry{},
			BackgroundServices: map[string]lockfile.Entry{},
			CLITools:           map[string]lockfile.Entry{},
		}}

	for _, ti := range insts {
		resolved := map[string]any{}
		for f, p := range ports[ti.id] {
			resolved[f] = p
		}
		out, err := Instantiate(ti.id, ti.inst, ti.tmpl, resolved)
		if err != nil {
			return err
		}
		g.AddNode("mcp:" + ti.id)
		entry := lockfile.Entry{Layer: string(ti.layer), FromTemplate: ti.inst.Template, Resolved: resolved}
		if out.MCPServer != nil {
			entry.Version = out.MCPServer.Version
			entry.ContentHash = lockfile.ContentHash(map[string]any{
				"command": out.MCPServer.Command, "version": out.MCPServer.Version,
				"env": toAnyMap(out.MCPServer.Env),
			})
			for _, r := range out.MCPServer.Requires {
				if r.Service != "" {
					g.AddNode("svc:" + r.Service)
					g.AddEdge("mcp:"+ti.id, "svc:"+r.Service)
				}
			}
		}
		lock.Entries.MCPServers[ti.id] = entry
		if out.Service != nil {
			g.AddNode("svc:" + out.Service.ID)
			lock.Entries.BackgroundServices[out.Service.ID] = lockfile.Entry{
				Layer: string(ti.layer), Resolved: resolved,
				ContentHash: lockfile.ContentHash(out.Service.Spec),
			}
			for _, r := range out.Service.Requires {
				if r.CLITool != "" {
					g.AddNode("cli:" + r.CLITool)
					g.AddEdge("svc:"+out.Service.ID, "cli:"+r.CLITool)
				}
			}
		}
	}
	if _, err := g.TopoSort(); err != nil {
		return fmt.Errorf("dependency graph invalid: %w", err)
	}

	committed, personal := splitByLayer(lock)
	if err := lockfile.Write(filepath.Join(dir, "ai-stack.lock"), committed); err != nil {
		return err
	}
	return lockfile.Write(filepath.Join(dir, "ai-stack.personal.lock"), personal)
}

func toAnyMap(m map[string]string) map[string]any {
	out := map[string]any{}
	for k, v := range m {
		out[k] = v
	}
	return out
}

func portsFromLock(l *lockfile.Lock) map[string]map[string]int {
	out := map[string]map[string]int{}
	for id, e := range l.Entries.MCPServers {
		for f, v := range e.Resolved {
			if p, ok := v.(int); ok {
				if out[id] == nil {
					out[id] = map[string]int{}
				}
				out[id][f] = p
			}
		}
	}
	return out
}

// splitByLayer divides a lock into the committed (team+repo) and personal locks
// (spec §7 — the layered lockfile).
func splitByLayer(l *lockfile.Lock) (committed, personal *lockfile.Lock) {
	mk := func() *lockfile.Lock {
		return &lockfile.Lock{Version: 1, GeneratedAt: l.GeneratedAt, Entries: lockfile.Entries{
			MCPServers: map[string]lockfile.Entry{}, BackgroundServices: map[string]lockfile.Entry{},
			CLITools: map[string]lockfile.Entry{}}}
	}
	committed, personal = mk(), mk()
	route := func(dst func(*lockfile.Lock) map[string]lockfile.Entry, src map[string]lockfile.Entry) {
		for id, e := range src {
			if e.Layer == string(manifest.LayerPersonal) {
				dst(personal)[id] = e
			} else {
				dst(committed)[id] = e
			}
		}
	}
	route(func(x *lockfile.Lock) map[string]lockfile.Entry { return x.Entries.MCPServers }, l.Entries.MCPServers)
	route(func(x *lockfile.Lock) map[string]lockfile.Entry { return x.Entries.BackgroundServices }, l.Entries.BackgroundServices)
	return committed, personal
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/resolve/ -run TestLockPipeline`
Expected: PASS.

- [ ] **Step 5: Write the command wiring**

```go
// (cmd/aistack/lock.go)
package main

import (
	"fmt"
	"os"

	"github.com/MHilhorst/aistack/internal/resolve"
)

// cmdLock implements `aistack lock`.
func cmdLock() int {
	wd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "aistack:", err)
		return 1
	}
	if err := resolve.RunLock(wd); err != nil {
		fmt.Fprintln(os.Stderr, "aistack lock:", err)
		return 1
	}
	fmt.Println("aistack: wrote ai-stack.lock")
	return 0
}
```

In `cmd/aistack/main.go`, replace the `case "init", "plan", "apply", "check", "lock":` arm so `lock` dispatches to `cmdLock()`:

```go
	case "lock":
		return cmdLock()
	case "init", "plan", "apply", "check":
		fmt.Fprintf(os.Stderr, "aistack: %q is not implemented yet (see docs/superpowers/plans/)\n", args[0])
		return 1
```

- [ ] **Step 6: Run the full build and test suite**

Run: `go build ./... && go test ./...`
Expected: build succeeds; all packages PASS.

- [ ] **Step 7: Smoke-test against the real example**

Run: `cd examples/multi-database && go run ../../cmd/aistack lock && git diff --stat ai-stack.lock`
Expected: `aistack: wrote ai-stack.lock`; the regenerated lock has four `mcpServers` entries with distinct `tunnelPort` values `13306`–`13309`.

- [ ] **Step 8: Commit**

```bash
git add cmd/aistack/ internal/resolve/pipeline.go internal/resolve/pipeline_test.go
git commit -m "Wire aistack lock command end-to-end"
```

---

## Self-Review

**Spec coverage:** Layers (Task 2), precedence Option-C (Task 6), templates/instances/resolved (Tasks 5, 8), dependency graph (Task 7), pinned-version rule from Iteration 1 (Task 3), layered lockfile from Iteration 2 (Task 11 `splitByLayer`), normalized hashing (Task 9). The `extends:` team-layer fetch, secret resolvers, gateway adapters, and the `plan`/`apply`/`check`/`init` commands are explicitly the follow-up plan.

**Type consistency:** `manifest.MCPServer` is used as both channel entry and template body throughout. `lockfile.Entry` carries every field Task 11 writes. `resolve.Instance` is the single instantiation output type. `graph.Graph` edge direction (`from requires to`) is consistent between Tasks 7 and 11.

**Placeholder scan:** every code step contains complete, compilable Go. No `TODO`, no "similar to Task N".

---

## Follow-up plan (not in scope here)

Once these types are concrete, write `docs/superpowers/plans/<date>-aistack-providers.md` covering: the `extends:` team-layer resolver; the channel provider interface (`resolve/plan/apply/check`); the MCP / cliTool / backgroundService / precondition providers; secret-resolver and package-manager adapters; and the `plan`, `apply`, `check`, and `init` commands.
