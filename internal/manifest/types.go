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
	// Location is a path, git+https:// URL, or npm: reference to the layer (spec §1).
	Location string `yaml:"source"`
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
	Template    string            `yaml:"template"`
	Params      map[string]any    `yaml:"params"`
	Secret      map[string]any    `yaml:"secret"`
	Transport   string            `yaml:"transport"`
	Command     string            `yaml:"command"`
	Args        []string          `yaml:"args"`
	Version     string            `yaml:"version"`
	Env         map[string]string `yaml:"env"`
	Via         string            `yaml:"via"`
	Requires    []Require         `yaml:"requires"`
	Enabled     *bool             `yaml:"enabled"`
	Overridable bool              `yaml:"overridable"`
}

// Require is one dependency-graph edge (spec §9).
type Require struct {
	Service      string `yaml:"service"`
	CLITool      string `yaml:"cliTool"`
	Precondition string `yaml:"precondition"`
}
