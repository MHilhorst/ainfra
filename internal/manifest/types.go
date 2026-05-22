// Package manifest defines the ainfra.yaml schema (spec/manifest-schema.md)
// and loads it from the three config layers.
package manifest

// Layer identifies which config layer a definition came from.
type Layer string

const (
	LayerTeam     Layer = "team"
	LayerRepo     Layer = "repo"
	LayerPersonal Layer = "personal"
)

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
	Marketplaces       map[string]Marketplace       `yaml:"marketplaces"`
	Plugins            map[string]Plugin            `yaml:"plugins"`
	Rules              map[string]Rule              `yaml:"rules"`
	Tools              *Tools                       `yaml:"tools"`
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
}

// Marketplace is a Claude Code plugin marketplace registration (spec §10).
type Marketplace struct {
	Source string `yaml:"source"`
}

// Plugin is an installable plugin bundle (spec §10).
type Plugin struct {
	Marketplace string    `yaml:"marketplace"`
	Version     string    `yaml:"version"`
	Requires    []Require `yaml:"requires"`
	Enabled     *bool     `yaml:"enabled"`
	Overridable bool      `yaml:"overridable"`
	Agents      []string  `yaml:"agents,omitempty"`
}

// Rule is a static context file — CLAUDE.md or similar (spec §10).
type Rule struct {
	Target      string    `yaml:"target"`
	Source      string    `yaml:"source"`
	Version     string    `yaml:"version"`
	Requires    []Require `yaml:"requires"`
	Enabled     *bool     `yaml:"enabled"`
	Overridable bool      `yaml:"overridable"`
	Agents      []string  `yaml:"agents,omitempty"`
}

// Tools is the built-in tooling channel — a singleton, not an id-keyed map
// (spec §10). Its list fields union-merge across layers (spec §1.1).
type Tools struct {
	Builtins    *Builtins    `yaml:"builtins"`
	Permissions *Permissions `yaml:"permissions"`
	Agents      []string     `yaml:"agents,omitempty"`
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
