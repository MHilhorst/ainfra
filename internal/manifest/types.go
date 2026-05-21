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
	Extends            []Source                     `yaml:"extends"`
	Preconditions      map[string]Precondition      `yaml:"preconditions"`
	CLITools           map[string]CLITool           `yaml:"cliTools"`
	BackgroundServices map[string]BackgroundService `yaml:"backgroundServices"`
	Secrets            map[string]Secret            `yaml:"secrets"`
	Templates          map[string]Template          `yaml:"templates"`
	MCPServers         map[string]MCPServer         `yaml:"mcpServers"`
	Hooks              map[string]Hook              `yaml:"hooks"`
	Commands           map[string]Command           `yaml:"commands"`
	ScheduledJobs      map[string]ScheduledJob      `yaml:"scheduledJobs"`
	// Targets is the governed vocabulary of machine-target labels (spec §13).
	Targets []string `yaml:"targets"`
	Host    Host     `yaml:"host"`
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
}

// Command is a Claude Code slash command — a sourced markdown file (spec §12).
type Command struct {
	Source      string    `yaml:"source"`
	Description string    `yaml:"description"`
	Version     string    `yaml:"version"`
	Requires    []Require `yaml:"requires"`
	Enabled     *bool     `yaml:"enabled"`
	Overridable bool      `yaml:"overridable"`
}

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
	// Targets are the labels THIS machine carries — a subset of the
	// manifest-level Targets vocabulary.
	Targets []string `yaml:"targets"`
}

// Require is one dependency-graph edge (spec §9).
type Require struct {
	Service      string `yaml:"service"`
	CLITool      string `yaml:"cliTool"`
	Precondition string `yaml:"precondition"`
}
