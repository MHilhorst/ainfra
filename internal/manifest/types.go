// Package manifest defines the ainfra.yaml schema (spec/manifest-schema.md)
// and loads it from the three config layers.
package manifest

import "gopkg.in/yaml.v3"

// Layer identifies which config layer a definition came from.
type Layer string

const (
	LayerTeam     Layer = "team"
	LayerRepo     Layer = "repo"
	LayerPersonal Layer = "personal"
)

// Var is one template variable. It is written either as a scalar (a literal
// value) or as a mapping declaring how the value is sourced.
type Var struct {
	From    string `yaml:"from"` // "value" | "env" | "command"
	Value   string `yaml:"value"`
	Env     string `yaml:"env"`
	Command string `yaml:"command"`
}

// UnmarshalYAML accepts a scalar (literal value) or a mapping form.
func (v *Var) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		v.From, v.Value = "value", node.Value
		return nil
	}
	type rawVar Var
	var r rawVar
	if err := node.Decode(&r); err != nil {
		return err
	}
	*v = Var(r)
	if v.From == "" {
		v.From = "value"
	}
	return nil
}

// Manifest is one parsed ainfra.yaml file (a single layer).
type Manifest struct {
	Version int `yaml:"version"`
	// AinfraVersion optionally pins the ainfra binary version this repo
	// expects. `ainfra install` warns when the running binary does not
	// match (exact-string match in v1; semver ranges deferred).
	AinfraVersion string `yaml:"ainfraVersion,omitempty"`
	Agent         string `yaml:"agent,omitempty"`
	// StalenessWarning controls whether `ainfra install` auto-emits a
	// SessionStart hook into .claude/settings.json that warns when the
	// manifest has changed since the last apply. Default (nil) = enabled.
	// Set explicitly to false to opt out.
	StalenessWarning   *bool                        `yaml:"stalenessWarning,omitempty"`
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
	Marketplaces       map[string]Marketplace       `yaml:"marketplaces"`
	Plugins            map[string]Plugin            `yaml:"plugins"`
	Plugin             *PluginBuild                 `yaml:"plugin,omitempty"`
	Rules              map[string]Rule              `yaml:"rules"`
	Vars               map[string]Var               `yaml:"vars"`
	Tools              *Tools                       `yaml:"tools"`
	Publish            *Publish                     `yaml:"publish,omitempty"`
}

// Source names a team/org layer to extend.
type Source struct {
	// Location is a path, git+https:// URL, or npm: reference to the layer (spec §1).
	Location string `yaml:"source"`
}

// Precondition is a verify-only check (spec §6).
type Precondition struct {
	Description string         `yaml:"description"`
	Check       map[string]any `yaml:"check"`
	Remediation string         `yaml:"remediation"`
}

// Selector gates an entry's resolution to a subset of invocations. It is the
// general form of the existing agents: gate: identities: filters by the
// caller (`--identity` / AINFRA_IDENTITY); paths: filters by where ainfra was
// invoked from inside the repo. A nil or fully-empty Selector matches every
// invocation (the historical default). Glob syntax follows path.Match.
type Selector struct {
	Identities []string `yaml:"identities,omitempty"`
	Paths      []string `yaml:"paths,omitempty"`
}

// CLITool is an installable substrate binary (spec §7). Env, Secret, and
// Requires let a tool carry credentials and declare precondition dependencies.
type CLITool struct {
	VersionConstraint string                    `yaml:"versionConstraint"`
	Install           map[string]map[string]any `yaml:"install"`
	Check             map[string]any            `yaml:"check"`
	Env               map[string]string         `yaml:"env"`
	Secret            map[string]any            `yaml:"secret"`
	Requires          []Require                 `yaml:"requires"`
	Overridable       bool                      `yaml:"overridable"`
	Agents            []string                  `yaml:"agents,omitempty"`
	Scope             *Selector                 `yaml:"scope,omitempty"`
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
	Mode string `yaml:"mode"`
	// Value holds a literal credential only in mode: direct with an inline
	// literal (spec §3). Empty for reference (Ref) and brokered modes.
	Value   string `yaml:"value"`
	Ref     string `yaml:"ref"`
	Gateway string `yaml:"gateway"`
	Scope   string `yaml:"scope"`
	// Env is the environment-variable name `ainfra exec` exports this secret
	// under. When set, it replaces the generated AINFRA_SECRET_* name, so the
	// exported environment matches an MCP config that already expects a
	// specific variable name (e.g. FLARE_API_TOKEN).
	Env string `yaml:"env"`
	// EnvFile, when true, marks a secret whose resolved value is a .env blob
	// (KEY=value lines). ainfra expands every line into its own environment
	// variable — one reference standing in for a whole environment.
	EnvFile bool `yaml:"envFile"`
	// Path, when set, materializes this secret as a file: `ainfra sync`
	// resolves the ref and writes the value verbatim to this path (0600).
	// It is the file-destination counterpart of Env, for a tool that reads a
	// credential file rather than an environment variable. ainfra moves an
	// opaque blob from the ref to the path — it never composes file content.
	Path string `yaml:"path"`
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
	Description string                    `yaml:"description"`
	Params      map[string]Param          `yaml:"params"`
	Secrets     map[string]TemplateSecret `yaml:"secrets"`
	Resolved    map[string]ResolvedField  `yaml:"resolved"`
	Produces    Produces                  `yaml:"produces"`
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
	Template     string            `yaml:"template"`
	Params       map[string]any    `yaml:"params"`
	Secret       map[string]any    `yaml:"secret"`
	Transport    string            `yaml:"transport"`
	URL          string            `yaml:"url"`
	Command      string            `yaml:"command"`
	Args         []string          `yaml:"args"`
	Version      string            `yaml:"version"`
	Env          map[string]string `yaml:"env"`
	Headers      map[string]string `yaml:"headers"`
	Capabilities map[string]any    `yaml:"capabilities"`
	Via          string            `yaml:"via"`
	Requires     []Require         `yaml:"requires"`
	Enabled      *bool             `yaml:"enabled"`
	Overridable  bool              `yaml:"overridable"`
	Agents       []string          `yaml:"agents,omitempty"`
	Scope        *Selector         `yaml:"scope,omitempty"`
}

// Hook is a Claude Code hook — automation bound to a lifecycle event (spec §11).
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
	Scope       *Selector `yaml:"scope,omitempty"`
}

// Command is a Claude Code slash command — a sourced markdown file (spec §12).
type Command struct {
	Source      string    `yaml:"source"`
	Description string    `yaml:"description"`
	Version     string    `yaml:"version"`
	Requires    []Require `yaml:"requires"`
	Enabled     *bool     `yaml:"enabled"`
	Overridable bool      `yaml:"overridable"`
	Agents      []string  `yaml:"agents,omitempty"`
	Scope       *Selector `yaml:"scope,omitempty"`
}

// Require is one dependency-graph edge (spec §9).
type Require struct {
	Service      string `yaml:"service"`
	CLITool      string `yaml:"cliTool"`
	Precondition string `yaml:"precondition"`
}

// Skill is an externally-sourced SKILL.md bundle ainfra materializes into
// .claude/skills/ (spec §10). Skills a repo commits to its own .claude/skills/
// arrive with git clone and are out of scope.
type Skill struct {
	Source      string    `yaml:"source"`
	Version     string    `yaml:"version"`
	Requires    []Require `yaml:"requires"`
	Enabled     *bool     `yaml:"enabled"`
	Overridable bool      `yaml:"overridable"`
	Agents      []string  `yaml:"agents,omitempty"`
	Scope       *Selector `yaml:"scope,omitempty"`
}

// Marketplace is a Claude Code plugin marketplace registration (spec §10).
type Marketplace struct {
	Source string `yaml:"source"`
}

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

// Plugin is an installable plugin bundle (spec §10).
type Plugin struct {
	Marketplace string    `yaml:"marketplace"`
	Version     string    `yaml:"version"`
	Requires    []Require `yaml:"requires"`
	Enabled     *bool     `yaml:"enabled"`
	Overridable bool      `yaml:"overridable"`
	Agents      []string  `yaml:"agents,omitempty"`
	Scope       *Selector `yaml:"scope,omitempty"`
}

// Rule is a static context file — CLAUDE.md or similar (spec §10).
type Rule struct {
	Target      string    `yaml:"target"`
	Source      string    `yaml:"source"`
	Template    bool      `yaml:"template"`
	Version     string    `yaml:"version"`
	Requires    []Require `yaml:"requires"`
	Enabled     *bool     `yaml:"enabled"`
	Overridable bool      `yaml:"overridable"`
	Agents      []string  `yaml:"agents,omitempty"`
	Scope       *Selector `yaml:"scope,omitempty"`
}

// Tools is the built-in tooling channel — a singleton, not an id-keyed map
// (spec §10). Its list fields union-merge across layers (spec §1.1).
type Tools struct {
	Builtins    *Builtins    `yaml:"builtins"`
	Permissions *Permissions `yaml:"permissions"`
	Agents      []string     `yaml:"agents,omitempty"`
}

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

// Builtins toggles Claude Code's built-in tools.
type Builtins struct {
	Disabled []string `yaml:"disabled"`
}

// Permissions is the three-tier tool permission policy. When a pattern lands
// in more than one tier after the layer union, the stricter tier wins:
// deny > ask > allow (spec §1.1).
type Permissions struct {
	Allow []string `yaml:"allow"`
	Ask   []string `yaml:"ask"`
	Deny  []string `yaml:"deny"`
}
