# Agent Selection and Capability Gating Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the target AI agent a chooseable axis of `ainfra.yaml` — a manifest selects `agent: claude-code` or `agent: codex`, and `validate` rejects any channel the chosen agent cannot render unless the entry is explicitly gated away.

**Architecture:** A new `internal/agent` package holds the agent capability registry — which of the eight channels each agent supports. The `manifest` package gains a scalar `agent` field and a per-entry `agents` gating list, plus cross-layer validation that resolves the agent and checks every channel entry against the registry. No rendering is built here; this is the schema-and-validation foundation that the later renderer plan rests on.

**Tech Stack:** Go 1.x, standard library, `gopkg.in/yaml.v3`. Tests are standard `go test`.

**Scope note:** This is the first of two plans from `docs/superpowers/specs/2026-05-21-multi-agent-renderers-design.md`. It delivers spec §3 (manifest changes), the `validate`/`init` parts of §6, and the capability registry that §2.2's `Renderer.Capabilities()` will later be checked against. The `Renderer` interface itself, the Claude Code and Codex renderers (§2.2, §5), `plan`/`apply`/`check` wiring (§6), and golden-file render tests (§8) are the **second plan** — they depend on the Phase 3 channel provider layer, which is not yet built. Building them here would produce inert interfaces with no caller.

---

## File Structure

**Created:**
- `internal/agent/agent.go` — agent IDs, the channel-capability registry, lookup functions. Depends on nothing.
- `internal/agent/agent_test.go` — tests for the registry.
- `internal/manifest/agent.go` — `ResolveAgent`, resolving the scalar `agent` field across layers.
- `internal/manifest/agent_test.go` — tests for `ResolveAgent`.

**Modified:**
- `internal/manifest/types.go` — add `Agent` field to `Manifest`; add `Agents` field to the eight channel-entry structs.
- `internal/manifest/validate.go` — add `validateAgentCapabilities`; call it from `ValidateAll`; relax the `rules[].target` requirement (spec §3.3).
- `internal/manifest/validate_test.go` — add capability-gating tests; replace the rule-needs-target test.
- `internal/manifest/types_test.go` — add a strict-decode test for the new `agent`/`agents` fields.
- `cmd/ainfra/cmd_init.go` — scaffold `agent: claude-code` in the starter manifest.
- `cmd/ainfra/cmd_init_test.go` — assert the scaffold contains the `agent` line.
- `spec/manifest-schema.md` — document the `agent` field and the `agents` gating field.

---

## Task 1: The `agent` capability registry

**Files:**
- Create: `internal/agent/agent.go`
- Test: `internal/agent/agent_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/agent/agent_test.go`:

```go
package agent

import "testing"

func TestKnownRecognizesRegisteredAgents(t *testing.T) {
	for _, id := range []string{"claude-code", "codex"} {
		if !Known(id) {
			t.Errorf("Known(%q) = false, want true", id)
		}
	}
	if Known("emacs-doctor") {
		t.Error(`Known("emacs-doctor") = true, want false`)
	}
}

func TestClaudeCodeSupportsEveryChannel(t *testing.T) {
	for _, ch := range []string{
		ChannelMCPServers, ChannelSkills, ChannelPlugins, ChannelRules,
		ChannelTools, ChannelCLITools, ChannelHooks, ChannelCommands,
	} {
		if !Supports(ClaudeCode, ch) {
			t.Errorf("Supports(ClaudeCode, %q) = false, want true", ch)
		}
	}
}

func TestCodexSupportsOnlyPortableChannels(t *testing.T) {
	supported := map[string]bool{
		ChannelMCPServers: true, ChannelRules: true, ChannelCLITools: true,
	}
	for _, ch := range []string{
		ChannelMCPServers, ChannelSkills, ChannelPlugins, ChannelRules,
		ChannelTools, ChannelCLITools, ChannelHooks, ChannelCommands,
	} {
		if got := Supports(Codex, ch); got != supported[ch] {
			t.Errorf("Supports(Codex, %q) = %v, want %v", ch, got, supported[ch])
		}
	}
}

func TestDefaultIsClaudeCode(t *testing.T) {
	if Default != ClaudeCode {
		t.Errorf("Default = %q, want %q", Default, ClaudeCode)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/`
Expected: FAIL — build error, `agent.go` and its identifiers do not exist yet.

- [ ] **Step 3: Write the implementation**

Create `internal/agent/agent.go`:

```go
// Package agent is the registry of AI coding agents ainfra can target and the
// channels each one supports. It is the seam that makes ainfra agnostic to the
// agent: resolution stays target-neutral, and this registry decides which
// channels a chosen agent can render. See
// docs/superpowers/specs/2026-05-21-multi-agent-renderers-design.md.
package agent

// ID identifies a target AI coding agent.
type ID string

const (
	ClaudeCode ID = "claude-code"
	Codex      ID = "codex"
)

// Default is the agent ainfra targets when no manifest layer names one.
const Default = ClaudeCode

// Channel names — the wire keys ainfra.yaml uses for each configurable channel.
const (
	ChannelMCPServers = "mcpServers"
	ChannelSkills     = "skills"
	ChannelPlugins    = "plugins"
	ChannelRules      = "rules"
	ChannelTools      = "tools"
	ChannelCLITools   = "cliTools"
	ChannelHooks      = "hooks"
	ChannelCommands   = "commands"
)

// capabilities records, per agent, which channels that agent can render. An
// agent missing from this map is unknown; a channel missing from an agent's
// set is one that agent cannot render.
var capabilities = map[ID]map[string]bool{
	ClaudeCode: {
		ChannelMCPServers: true, ChannelSkills: true, ChannelPlugins: true,
		ChannelRules: true, ChannelTools: true, ChannelCLITools: true,
		ChannelHooks: true, ChannelCommands: true,
	},
	Codex: {
		ChannelMCPServers: true, ChannelRules: true, ChannelCLITools: true,
	},
}

// Known reports whether id names an agent ainfra can target.
func Known(id string) bool {
	_, ok := capabilities[ID(id)]
	return ok
}

// Supports reports whether agent id can render the named channel.
func Supports(id ID, channel string) bool {
	return capabilities[id][channel]
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/agent/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agent/
git commit -m "Add the agent capability registry"
```

---

## Task 2: The `agent` field and `ResolveAgent`

**Files:**
- Modify: `internal/manifest/types.go:15-30` (the `Manifest` struct)
- Create: `internal/manifest/agent.go`
- Test: `internal/manifest/agent_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/manifest/agent_test.go`:

```go
package manifest

import "testing"

func TestResolveAgentDefaultsToClaudeCode(t *testing.T) {
	layers := map[Layer]*Manifest{
		LayerRepo: {Version: 1},
	}
	id, _, explicit := ResolveAgent(layers)
	if id != "claude-code" {
		t.Errorf("id = %q, want claude-code", id)
	}
	if explicit {
		t.Error("explicit = true, want false when no layer sets agent")
	}
}

func TestResolveAgentUsesPersonalWhenRepoSilent(t *testing.T) {
	layers := map[Layer]*Manifest{
		LayerRepo:     {Version: 1},
		LayerPersonal: {Version: 1, Agent: "codex"},
	}
	id, layer, explicit := ResolveAgent(layers)
	if id != "codex" {
		t.Errorf("id = %q, want codex", id)
	}
	if layer != LayerPersonal {
		t.Errorf("layer = %q, want personal", layer)
	}
	if !explicit {
		t.Error("explicit = false, want true")
	}
}

func TestResolveAgentRepoBeatsPersonal(t *testing.T) {
	layers := map[Layer]*Manifest{
		LayerRepo:     {Version: 1, Agent: "claude-code"},
		LayerPersonal: {Version: 1, Agent: "codex"},
	}
	id, layer, _ := ResolveAgent(layers)
	if id != "claude-code" {
		t.Errorf("id = %q, want claude-code (repo outranks personal)", id)
	}
	if layer != LayerRepo {
		t.Errorf("layer = %q, want repo", layer)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/manifest/ -run TestResolveAgent`
Expected: FAIL — `Manifest` has no `Agent` field and `ResolveAgent` is undefined.

- [ ] **Step 3: Add the `Agent` field to the `Manifest` struct**

In `internal/manifest/types.go`, in the `Manifest` struct, add the `Agent` field as the first field after `Version`:

```go
// Manifest is one parsed ainfra.yaml file (a single layer).
type Manifest struct {
	Version            int                          `yaml:"version"`
	Agent              string                       `yaml:"agent,omitempty"`
	Extends            []Source                     `yaml:"extends"`
	Preconditions      map[string]Precondition      `yaml:"preconditions"`
	CLITools           map[string]CLITool           `yaml:"cliTools"`
	BackgroundServices map[string]BackgroundService `yaml:"backgroundServices"`
	Secrets            map[string]Secret            `yaml:"secrets"`
	Templates          map[string]Template          `yaml:"templates"`
	MCPServers         map[string]MCPServer         `yaml:"mcpServers"`
	Hooks              map[string]Hook              `yaml:"hooks"`
	Commands           map[string]Command           `yaml:"commands"`
	Skills             map[string]Skill             `yaml:"skills"`
	Plugins            map[string]Plugin            `yaml:"plugins"`
	Rules              map[string]Rule              `yaml:"rules"`
	Tools              *Tools                       `yaml:"tools"`
}
```

- [ ] **Step 4: Write `ResolveAgent`**

Create `internal/manifest/agent.go`:

```go
package manifest

import "github.com/MHilhorst/ainfra/internal/agent"

// ResolveAgent determines the target agent across the config layers. The
// highest-authority layer that declares a non-empty agent wins (team, then
// repo, then personal); when no layer declares one the default agent is used.
// It returns the agent id, the layer that set it (empty when defaulted), and
// whether any layer set it explicitly. ResolveAgent does not check that the
// id is a known agent — validateAgentCapabilities does.
func ResolveAgent(layers map[Layer]*Manifest) (id string, layer Layer, explicit bool) {
	for _, ln := range []Layer{LayerTeam, LayerRepo, LayerPersonal} {
		if m, ok := layers[ln]; ok && m.Agent != "" {
			return m.Agent, ln, true
		}
	}
	return string(agent.Default), "", false
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/manifest/ -run TestResolveAgent`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/manifest/types.go internal/manifest/agent.go internal/manifest/agent_test.go
git commit -m "Add the agent field and cross-layer resolution"
```

---

## Task 3: The `agents` gating field on channel entries

**Files:**
- Modify: `internal/manifest/types.go` — `MCPServer`, `Hook`, `Command`, `Skill`, `Plugin`, `Rule`, `CLITool`, `Tools` structs
- Test: `internal/manifest/types_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/manifest/types_test.go`:

```go
func TestStrictDecodeAcceptsAgentsGatingField(t *testing.T) {
	src := `
version: 1
agent: codex
mcpServers:
  github:
    command: npx
    args: ["-y", "@modelcontextprotocol/server-github"]
    version: "0.6.2"
    agents: [claude-code, codex]
hooks:
  gofmt:
    event: PostToolUse
    command: gofmt -w .
    agents: [claude-code]
tools:
  builtins:
    disabled: [WebFetch]
  agents: [claude-code]
`
	var m Manifest
	dec := yaml.NewDecoder(strings.NewReader(src))
	dec.KnownFields(true)
	if err := dec.Decode(&m); err != nil {
		t.Fatalf("strict decode rejected the agents field: %v", err)
	}
	if got := m.MCPServers["github"].Agents; len(got) != 2 {
		t.Errorf("mcpServers.github.agents = %v, want 2 entries", got)
	}
	if got := m.Hooks["gofmt"].Agents; len(got) != 1 || got[0] != "claude-code" {
		t.Errorf("hooks.gofmt.agents = %v, want [claude-code]", got)
	}
	if got := m.Tools.Agents; len(got) != 1 || got[0] != "claude-code" {
		t.Errorf("tools.agents = %v, want [claude-code]", got)
	}
}
```

Ensure `internal/manifest/types_test.go` imports `strings`, `testing`, and `gopkg.in/yaml.v3`. If the file has no import block yet, add:

```go
import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)
```

If an import block already exists, add only the names it is missing.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/manifest/ -run TestStrictDecodeAcceptsAgentsGatingField`
Expected: FAIL — strict decoding rejects the unknown `agents` key.

- [ ] **Step 3: Add the `Agents` field to each channel-entry struct**

In `internal/manifest/types.go`, add `Agents []string` to each of the eight structs below. Add it as the last field of each struct, with this exact tag:

```go
Agents []string `yaml:"agents,omitempty"`
```

The structs to modify, each gaining that one line:

- `MCPServer` — after `Overridable bool` (line ~120)
- `Hook` — after `Overridable bool` (line ~132)
- `Command` — after `Overridable bool` (line ~142)
- `Skill` — after `Overridable bool` (line ~160)
- `Plugin` — after `Overridable bool` (line ~169)
- `Rule` — after `Overridable bool` (line ~179)
- `CLITool` — after `Overridable bool` (line ~50)
- `Tools` — after `Permissions *Permissions` (line ~186)

For example, `Hook` becomes:

```go
// Hook is a hook — automation bound to an agent lifecycle event (spec §11).
type Hook struct {
	Event       string    `yaml:"event"`
	Matcher     string    `yaml:"matcher"`
	Command     string    `yaml:"command"`
	Source      string    `yaml:"source"`
	Timeout     int       `yaml:"timeout"`
	Requires    []Require `yaml:"requires"`
	Enabled     *bool     `yaml:"enabled"`
	Overridable bool      `yaml:"overridable"`
	Agents      []string  `yaml:"agents,omitempty"`
}
```

and `Tools` becomes:

```go
// Tools is the built-in tooling channel — a singleton, not an id-keyed map
// (spec §10). Its list fields union-merge across layers (spec §1.1).
type Tools struct {
	Builtins    *Builtins    `yaml:"builtins"`
	Permissions *Permissions `yaml:"permissions"`
	Agents      []string     `yaml:"agents,omitempty"`
}
```

Apply the same one-line addition to the other six structs.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/manifest/ -run TestStrictDecodeAcceptsAgentsGatingField`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/manifest/types.go internal/manifest/types_test.go
git commit -m "Add the per-entry agents gating field to channel structs"
```

---

## Task 4: Reject an unknown `agent` value

**Files:**
- Modify: `internal/manifest/validate.go` — add `validateAgentCapabilities`, call it from `ValidateAll`
- Test: `internal/manifest/validate_test.go`

This task adds `validateAgentCapabilities` with only the unknown-agent check. Task 5 extends the same function with the per-entry capability check.

- [ ] **Step 1: Write the failing test**

Add to `internal/manifest/validate_test.go`:

```go
func TestValidateAllRejectsUnknownAgent(t *testing.T) {
	layers := map[Layer]*Manifest{
		LayerRepo: {Version: 1, Agent: "emacs-doctor"},
	}
	d := asDiagnostic(t, ValidateAll(layers))
	if !strings.Contains(d.Summary, "unknown agent") {
		t.Errorf("summary = %q, want it to mention an unknown agent", d.Summary)
	}
	if d.Path != "agent" {
		t.Errorf("path = %q, want agent", d.Path)
	}
	if d.File != "ainfra.yaml" {
		t.Errorf("file = %q, want ainfra.yaml", d.File)
	}
	if d.Hint == "" {
		t.Error("expected a hint listing valid agents")
	}
}

func TestValidateAllAcceptsKnownAgent(t *testing.T) {
	layers := map[Layer]*Manifest{
		LayerRepo: {Version: 1, Agent: "codex"},
	}
	if err := ValidateAll(layers); err != nil {
		t.Fatalf("unexpected error for a known agent: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/manifest/ -run TestValidateAllRejectsUnknownAgent`
Expected: FAIL — `ValidateAll` does not yet check the agent value.

- [ ] **Step 3: Add `validateAgentCapabilities` and call it**

In `internal/manifest/validate.go`, add `"github.com/MHilhorst/ainfra/internal/agent"` to the import block.

Add this function to the file:

```go
// agentFileFor names the source file for each layer, used to tag diagnostics
// raised by the cross-layer agent checks.
var agentFileFor = map[Layer]string{
	LayerRepo:     "ainfra.yaml",
	LayerPersonal: "ainfra.personal.yaml",
	LayerTeam:     "(team layer)",
}

// validateAgentCapabilities resolves the target agent and rejects an unknown
// agent id. Task 5 extends it with the per-entry capability check.
func validateAgentCapabilities(layers map[Layer]*Manifest) error {
	id, setLayer, _ := ResolveAgent(layers)
	if !agent.Known(id) {
		return &diag.Diagnostic{
			Summary: fmt.Sprintf("unknown agent %q", id),
			File:    agentFileFor[setLayer],
			Path:    "agent",
			Detail:  fmt.Sprintf("The agent field selects which AI agent ainfra renders for; %q is not one ainfra knows.", id),
			Hint:    "Valid agents: claude-code, codex.",
		}
	}
	return nil
}
```

In `ValidateAll`, replace the final `return nil` with a call to the new function. The end of `ValidateAll` becomes:

```go
	for _, ln := range order {
		m, ok := layers[ln]
		if !ok {
			continue
		}
		toValidate := m
		if len(m.Templates) < len(allTemplates) {
			copied := *m
			copied.Templates = allTemplates
			toValidate = &copied
		}
		if err := Validate(toValidate); err != nil {
			if d, ok := err.(*diag.Diagnostic); ok && d.File == "" {
				d.File = fileFor[ln]
			}
			return err
		}
	}
	return validateAgentCapabilities(layers)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/manifest/ -run TestValidateAll`
Expected: PASS (both new tests and the existing `TestValidateAll*` tests).

- [ ] **Step 5: Commit**

```bash
git add internal/manifest/validate.go internal/manifest/validate_test.go
git commit -m "Reject an unknown agent value in ValidateAll"
```

---

## Task 5: Capability-gate channel entries against the resolved agent

**Files:**
- Modify: `internal/manifest/validate.go` — extend `validateAgentCapabilities`
- Test: `internal/manifest/validate_test.go`

This task implements spec §3.2: an entry in a channel the resolved agent cannot render is a hard error unless its `agents:` list gates it away.

- [ ] **Step 1: Write the failing test for an ungated unsupported channel**

Add to `internal/manifest/validate_test.go`:

```go
func TestValidateAllRejectsUngatedChannelUnsupportedByAgent(t *testing.T) {
	layers := map[Layer]*Manifest{
		LayerRepo: {Version: 1, Agent: "codex",
			Hooks: map[string]Hook{
				"gofmt": {Event: "PostToolUse", Command: "gofmt -w ."},
			}},
	}
	d := asDiagnostic(t, ValidateAll(layers))
	if !strings.Contains(d.Summary, "hooks") {
		t.Errorf("summary = %q, want it to name the hooks channel", d.Summary)
	}
	if d.Path != "hooks.gofmt" {
		t.Errorf("path = %q, want hooks.gofmt", d.Path)
	}
	if d.Hint == "" {
		t.Error("expected a hint suggesting agents: gating")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/manifest/ -run TestValidateAllRejectsUngatedChannelUnsupportedByAgent`
Expected: FAIL — `validateAgentCapabilities` does not yet inspect channel entries.

- [ ] **Step 3: Add the entry walk and the capability check**

In `internal/manifest/validate.go`, add the `channelEntry` type, the `collectEntries` and `checkEntryAgent` helpers, and extend `validateAgentCapabilities` to walk entries. Replace the whole `validateAgentCapabilities` function and add the helpers below it:

```go
// channelEntry is one channel entry flattened for the capability check.
type channelEntry struct {
	channel string
	id      string // empty for the singleton tools channel
	agents  []string
}

// path renders the diagnostic Path for an entry.
func (e channelEntry) path() string {
	if e.id == "" {
		return e.channel
	}
	return e.channel + "." + e.id
}

// collectEntries flattens every channel entry of m into a deterministic,
// sorted slice so the capability check reports a stable first error.
func collectEntries(m *Manifest) []channelEntry {
	var out []channelEntry
	for _, id := range slices.Sorted(maps.Keys(m.MCPServers)) {
		out = append(out, channelEntry{agent.ChannelMCPServers, id, m.MCPServers[id].Agents})
	}
	for _, id := range slices.Sorted(maps.Keys(m.Skills)) {
		out = append(out, channelEntry{agent.ChannelSkills, id, m.Skills[id].Agents})
	}
	for _, id := range slices.Sorted(maps.Keys(m.Plugins)) {
		out = append(out, channelEntry{agent.ChannelPlugins, id, m.Plugins[id].Agents})
	}
	for _, id := range slices.Sorted(maps.Keys(m.Rules)) {
		out = append(out, channelEntry{agent.ChannelRules, id, m.Rules[id].Agents})
	}
	for _, id := range slices.Sorted(maps.Keys(m.CLITools)) {
		out = append(out, channelEntry{agent.ChannelCLITools, id, m.CLITools[id].Agents})
	}
	for _, id := range slices.Sorted(maps.Keys(m.Hooks)) {
		out = append(out, channelEntry{agent.ChannelHooks, id, m.Hooks[id].Agents})
	}
	for _, id := range slices.Sorted(maps.Keys(m.Commands)) {
		out = append(out, channelEntry{agent.ChannelCommands, id, m.Commands[id].Agents})
	}
	if m.Tools != nil {
		out = append(out, channelEntry{agent.ChannelTools, "", m.Tools.Agents})
	}
	return out
}

// checkEntryAgent applies the spec §3.2 gating rules to one entry against the
// resolved target agent. It returns nil when the entry is acceptable.
func checkEntryAgent(e channelEntry, target agent.ID) *diag.Diagnostic {
	for _, a := range e.agents {
		if !agent.Known(a) {
			return &diag.Diagnostic{
				Summary: fmt.Sprintf("unknown agent %q in agents:", a),
				Path:    e.path(),
				Detail:  fmt.Sprintf("Entry %q gates to agent %q, which ainfra does not know.", e.path(), a),
				Hint:    "Valid agents: claude-code, codex.",
			}
		}
	}
	// A non-empty agents: list that omits the target deliberately scopes this
	// entry away from the target — cleanly skipped, not an error.
	if len(e.agents) > 0 && !slices.Contains(e.agents, string(target)) {
		return nil
	}
	if agent.Supports(target, e.channel) {
		return nil
	}
	if len(e.agents) > 0 {
		// agents: lists the target, yet the target cannot render this channel.
		return &diag.Diagnostic{
			Summary: fmt.Sprintf("agent %q cannot render the %s channel", target, e.channel),
			Path:    e.path(),
			Detail:  fmt.Sprintf("Entry %q is gated to agent %q, but %q has no %s channel.", e.path(), target, target, e.channel),
			Hint:    fmt.Sprintf("Remove %q from this entry's agents: list.", target),
		}
	}
	return &diag.Diagnostic{
		Summary: fmt.Sprintf("the %s channel is not supported by agent %q", e.channel, target),
		Path:    e.path(),
		Detail:  fmt.Sprintf("The resolved agent is %q, which cannot render the %s channel.", target, e.channel),
		Hint:    "Gate this entry away with  agents: [claude-code]  — or change the agent field.",
	}
}

// validateAgentCapabilities resolves the target agent, rejects an unknown
// agent id, and checks every channel entry against the agent's capabilities
// (spec §3.1, §3.2).
func validateAgentCapabilities(layers map[Layer]*Manifest) error {
	id, setLayer, _ := ResolveAgent(layers)
	if !agent.Known(id) {
		return &diag.Diagnostic{
			Summary: fmt.Sprintf("unknown agent %q", id),
			File:    agentFileFor[setLayer],
			Path:    "agent",
			Detail:  fmt.Sprintf("The agent field selects which AI agent ainfra renders for; %q is not one ainfra knows.", id),
			Hint:    "Valid agents: claude-code, codex.",
		}
	}
	target := agent.ID(id)
	for _, ln := range []Layer{LayerTeam, LayerRepo, LayerPersonal} {
		m, ok := layers[ln]
		if !ok {
			continue
		}
		for _, e := range collectEntries(m) {
			if d := checkEntryAgent(e, target); d != nil {
				d.File = agentFileFor[ln]
				return d
			}
		}
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/manifest/ -run TestValidateAllRejectsUngatedChannelUnsupportedByAgent`
Expected: PASS

- [ ] **Step 5: Write the failing test for a gated-away entry**

Add to `internal/manifest/validate_test.go`:

```go
func TestValidateAllAcceptsChannelGatedAwayFromAgent(t *testing.T) {
	layers := map[Layer]*Manifest{
		LayerRepo: {Version: 1, Agent: "codex",
			Hooks: map[string]Hook{
				"gofmt": {Event: "PostToolUse", Command: "gofmt -w .", Agents: []string{"claude-code"}},
			}},
	}
	if err := ValidateAll(layers); err != nil {
		t.Fatalf("a hook gated to claude-code only must validate under agent codex: %v", err)
	}
}
```

- [ ] **Step 6: Run test to verify it passes**

Run: `go test ./internal/manifest/ -run TestValidateAllAcceptsChannelGatedAwayFromAgent`
Expected: PASS (the gating logic from Step 3 already covers this).

- [ ] **Step 7: Write the failing test for an entry gated TO an agent that cannot render it**

Add to `internal/manifest/validate_test.go`:

```go
func TestValidateAllRejectsEntryGatedToAnAgentThatCannotRenderIt(t *testing.T) {
	layers := map[Layer]*Manifest{
		LayerRepo: {Version: 1, Agent: "codex",
			Hooks: map[string]Hook{
				"gofmt": {Event: "PostToolUse", Command: "gofmt -w .", Agents: []string{"codex"}},
			}},
	}
	d := asDiagnostic(t, ValidateAll(layers))
	if !strings.Contains(d.Summary, "cannot render") {
		t.Errorf("summary = %q, want it to say the agent cannot render the channel", d.Summary)
	}
	if d.Path != "hooks.gofmt" {
		t.Errorf("path = %q, want hooks.gofmt", d.Path)
	}
}

func TestValidateAllRejectsUnknownAgentInGatingList(t *testing.T) {
	layers := map[Layer]*Manifest{
		LayerRepo: {Version: 1,
			MCPServers: map[string]MCPServer{
				"github": {Command: "npx", Version: "0.6.2", Agents: []string{"emacs-doctor"}},
			}},
	}
	d := asDiagnostic(t, ValidateAll(layers))
	if !strings.Contains(d.Summary, "unknown agent") {
		t.Errorf("summary = %q, want it to mention an unknown agent", d.Summary)
	}
	if d.Path != "mcpServers.github" {
		t.Errorf("path = %q, want mcpServers.github", d.Path)
	}
}
```

- [ ] **Step 8: Run test to verify it passes**

Run: `go test ./internal/manifest/ -run TestValidateAll`
Expected: PASS — all `TestValidateAll*` tests, new and existing.

- [ ] **Step 9: Commit**

```bash
git add internal/manifest/validate.go internal/manifest/validate_test.go
git commit -m "Capability-gate channel entries against the resolved agent"
```

---

## Task 6: Relax the `rules[].target` requirement

**Files:**
- Modify: `internal/manifest/validate.go:114-132` (the rules loop)
- Test: `internal/manifest/validate_test.go:146-152` (replace `TestValidateRejectsRuleWithoutTarget`)

Spec §3.3 makes a rule's destination renderer-owned — each renderer places a rule at its agent's instruction file (`CLAUDE.md` / `AGENTS.md`). An explicit `rules[].target` stays allowed as an override but is no longer required.

- [ ] **Step 1: Replace the rule-target test**

In `internal/manifest/validate_test.go`, replace `TestValidateRejectsRuleWithoutTarget` with:

```go
func TestValidateAcceptsRuleWithoutTarget(t *testing.T) {
	// A rule's destination is renderer-owned (multi-agent renderers spec §3.3);
	// an explicit target is an optional override, not a requirement.
	m := &Manifest{Version: 1, Rules: map[string]Rule{"r": {Source: "./r.md"}}}
	if err := Validate(m); err != nil {
		t.Fatalf("a rule without an explicit target must validate: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/manifest/ -run TestValidateAcceptsRuleWithoutTarget`
Expected: FAIL — `Validate` still errors with "rule declares no target".

- [ ] **Step 3: Remove the required-target check**

In `internal/manifest/validate.go`, in the `m.Rules` loop, delete the `if r.Target == ""` block. The loop becomes:

```go
	for _, id := range slices.Sorted(maps.Keys(m.Rules)) {
		r := m.Rules[id]
		if r.Source == "" {
			return &diag.Diagnostic{
				Summary: "rule declares no source",
				Path:    "rules." + id,
				Detail:  fmt.Sprintf("Rule %q has no source file.", id),
				Hint:    "Add a source field pointing at the context file (e.g. ./rules/team-claude.md).",
			}
		}
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/manifest/ -run 'TestValidateAcceptsRuleWithoutTarget|TestValidateAcceptsWellFormedChannels'`
Expected: PASS — `TestValidateAcceptsWellFormedChannels` still passes; it sets `target` explicitly, which remains valid.

- [ ] **Step 5: Commit**

```bash
git add internal/manifest/validate.go internal/manifest/validate_test.go
git commit -m "Make a rule's destination renderer-owned, not a required field"
```

---

## Task 7: Scaffold `agent` in `init` and document the fields

**Files:**
- Modify: `cmd/ainfra/cmd_init.go:16-29` (the `starterManifest` constant)
- Test: `cmd/ainfra/cmd_init_test.go`
- Modify: `spec/manifest-schema.md`

- [ ] **Step 1: Write the failing test**

Add to `cmd/ainfra/cmd_init_test.go` (match the package and import style already in that file; the test reads the file `init` writes and asserts on its content):

```go
func TestInitScaffoldsAgentField(t *testing.T) {
	dir := t.TempDir()
	ctx := cli.Context{Dir: dir, Stdout: io.Discard, Stderr: io.Discard, NoColor: true}
	if code := runInit(ctx, false, false); code != 0 {
		t.Fatalf("runInit exit code = %d, want 0", code)
	}
	data, err := os.ReadFile(filepath.Join(dir, "ainfra.yaml"))
	if err != nil {
		t.Fatalf("reading scaffolded manifest: %v", err)
	}
	if !strings.Contains(string(data), "agent: claude-code") {
		t.Errorf("scaffolded manifest does not declare  agent: claude-code\n%s", data)
	}
}
```

If `cmd_init_test.go` does not already import `io`, `os`, `path/filepath`, `strings`, or `github.com/MHilhorst/ainfra/internal/cli`, add the ones it is missing to its import block.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/ainfra/ -run TestInitScaffoldsAgentField`
Expected: FAIL — the scaffold has no `agent` line.

- [ ] **Step 3: Add `agent` to the starter manifest**

In `cmd/ainfra/cmd_init.go`, change the `starterManifest` constant to:

```go
// starterManifest is the ainfra.yaml a fresh `ainfra init` writes.
const starterManifest = `version: 1

# ainfra manifest — your team's AI coding agent setup as config-as-code.
# Schema: spec/manifest-schema.md   Guide: docs/quickstart.md

# Which AI coding agent ainfra renders for: claude-code (default) or codex.
agent: claude-code

# CLI tools the other channels depend on.
cliTools: {}

# MCP servers to land in each developer's agent config.
mcpServers: {}

# Hooks, commands, skills, plugins, and rules go here too —
# see spec/manifest-schema.md for the full schema.
`
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/ainfra/ -run TestInitScaffoldsAgentField`
Expected: PASS

- [ ] **Step 5: Document the `agent` and `agents` fields in the spec**

In `spec/manifest-schema.md`, make two edits.

First, in the "Top-level structure" code block (§2), add an `agent` line directly under `version: 1`:

```yaml
version: 1
agent:              claude-code  # which AI agent to render for (§2.1)
extends:            []      # team/org layer sources
```

Second, add this new section immediately after §1.1 (after the line "additive by default, with team guardrails a lower layer cannot lift." and before the `---` that precedes "## 2. Top-level structure"):

```markdown
### 1.2 The target agent

`agent` names the AI coding agent ainfra renders for: `claude-code` (the
default) or `codex`. It is a scalar, so the `overridable` mechanism — which
arbitrates id-keyed entries — does not apply. The highest-authority layer that
declares a non-empty `agent` wins (team, then repo, then personal): a repo that
sets `agent` standardizes the team on it; a repo that omits it leaves the
choice to each developer's personal layer.

Not every channel exists for every agent — Codex has no skills, plugins,
hooks, built-in toggles, or slash commands. Any channel entry may carry an
`agents:` list to scope it:

```yaml
hooks:
  gofmt-after-edit:
    event: PostToolUse
    command: gofmt -w .
    agents: [claude-code]   # this hook applies only when agent is claude-code
```

Under a resolved `agent`, an entry in a channel that agent cannot render is a
hard validation error — unless its `agents:` list omits that agent, which
cleanly scopes the entry away. An ungated entry never silently disappears.
```

- [ ] **Step 6: Commit**

```bash
git add cmd/ainfra/cmd_init.go cmd/ainfra/cmd_init_test.go spec/manifest-schema.md
git commit -m "Scaffold the agent field in init and document agent selection"
```

---

## Task 8: Full build, test, and behaviour verification

**Files:** none — this task verifies the whole change.

- [ ] **Step 1: Build everything**

Run: `go build ./...`
Expected: no output, exit 0.

- [ ] **Step 2: Run the full test suite**

Run: `go test ./...`
Expected: every package reports `ok`. The schema package needs no change — `internal/schema` reflects the manifest structs, so the new `agent` and `agents` fields appear in `ainfra schema` automatically; `TestGenerateCoversEveryChannel` still passes because it only checks that the listed channels are present.

- [ ] **Step 3: Build the CLI binary for the end-to-end checks**

From the repo root, run: `go build -o /tmp/ainfra-bin ./cmd/ainfra && mkdir -p /tmp/ainfra-agent-check`
Expected: no output, exit 0.

- [ ] **Step 4: Verify the unknown-agent error end to end**

Run:
```bash
printf 'version: 1\nagent: emacs-doctor\n' > /tmp/ainfra-agent-check/ainfra.yaml
/tmp/ainfra-bin --chdir /tmp/ainfra-agent-check validate
```
Expected: exit code 1, an error naming the unknown agent `emacs-doctor` with the hint `Valid agents: claude-code, codex.`

- [ ] **Step 5: Verify the capability-gating error end to end**

Run:
```bash
printf 'version: 1\nagent: codex\nhooks:\n  gofmt:\n    event: PostToolUse\n    command: gofmt -w .\n' > /tmp/ainfra-agent-check/ainfra.yaml
/tmp/ainfra-bin --chdir /tmp/ainfra-agent-check validate
```
Expected: exit code 1, an error that the `hooks` channel is not supported by agent `codex`, at path `hooks.gofmt`.

- [ ] **Step 6: Verify a gated manifest passes end to end**

Run:
```bash
printf 'version: 1\nagent: codex\nhooks:\n  gofmt:\n    event: PostToolUse\n    command: gofmt -w .\n    agents: [claude-code]\n' > /tmp/ainfra-agent-check/ainfra.yaml
/tmp/ainfra-bin --chdir /tmp/ainfra-agent-check validate
```
Expected: exit code 0, `Configuration is valid.`

- [ ] **Step 7: Clean up the scratch files**

Run: `rm -rf /tmp/ainfra-agent-check /tmp/ainfra-bin`
Expected: no output.

- [ ] **Step 8: Final commit (only if Steps 1-2 required any fix)**

If Steps 1-2 surfaced a compile or test failure that needed a fix, commit it:

```bash
git add -A
git commit -m "Fix build and test fallout from agent selection"
```

If Steps 1-2 passed clean, there is nothing to commit — skip this step.

---

## Self-Review

**Spec coverage** (against `2026-05-21-multi-agent-renderers-design.md`):
- §3.1 `agent` field + precedence → Tasks 2, 4 (resolution, unknown-agent rejection).
- §3.2 capability gating → Task 5 (all three outcomes: supported, gated-away, ungated-error; plus gated-to-unsupported and unknown-gating-agent).
- §3.3 renderer-owned `rules` destination → Task 6.
- §6 `validate` enforcement → Tasks 4-5; `init` scaffold → Task 7.
- §2.2 `Capabilities()` → Task 1 provides the capability registry the future `Renderer.Capabilities()` is checked against; the `Renderer` interface itself is out of scope (see Scope note).
- §2.1, §4, §5, §7 steps 2-4, §8 golden tests → out of scope; second plan.

**Placeholder scan:** none — every step has concrete code or an exact command.

**Type consistency:** `agent.ID`, `agent.Known(string) bool`, `agent.Supports(agent.ID, string) bool`, `agent.Default`, and the `agent.Channel*` constants are defined in Task 1 and used unchanged in Tasks 4-5. `ResolveAgent` returns `(string, Layer, bool)` in Task 2 and is consumed with that signature in Tasks 4-5. `channelEntry` and `checkEntryAgent` are defined and used within Task 5. The `Agents []string` field name is consistent across Tasks 3, 5, and 7.
