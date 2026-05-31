package lockfile

// Lock is one ainfra.lock file (spec Phase 2).
type Lock struct {
	Version      int                  `yaml:"version"`
	GeneratedAt  string               `yaml:"generatedAt"`
	ManifestHash string               `yaml:"manifestHash,omitempty"`
	Entries      Entries              `yaml:"entries"`
	Secrets      map[string]SecretRef `yaml:"secrets,omitempty"`
	Plugin       *PluginBaseline      `yaml:"plugin,omitempty"`
}

// Entries groups lock entries by channel.
type Entries struct {
	MCPServers         map[string]Entry `yaml:"mcpServers"`
	BackgroundServices map[string]Entry `yaml:"backgroundServices"`
	Hooks              map[string]Entry `yaml:"hooks"`
	Commands           map[string]Entry `yaml:"commands"`
	CLITools           map[string]Entry `yaml:"cliTools"`
	Skills             map[string]Entry `yaml:"skills"`
	Marketplaces       map[string]Entry `yaml:"marketplaces,omitempty"`
	Plugins            map[string]Entry `yaml:"plugins"`
	Rules              map[string]Entry `yaml:"rules"`
	Tools              map[string]Entry `yaml:"tools"`
}

// Entry is one resolved lock entry.
type Entry struct {
	Layer           string            `yaml:"layer"`
	FromTemplate    string            `yaml:"fromTemplate,omitempty"`
	Resolved        map[string]any    `yaml:"resolved,omitempty"`
	Version         string            `yaml:"version,omitempty"`
	Integrity       string            `yaml:"integrity,omitempty"`
	ToolsetHash     string            `yaml:"toolsetHash,omitempty"`
	LockedTools     []LockedTool      `yaml:"lockedTools,omitempty"`
	Command         string            `yaml:"command,omitempty"`
	Args            []string          `yaml:"args,omitempty"`
	Env             map[string]string `yaml:"env,omitempty"`
	Constraint      string            `yaml:"constraint,omitempty"`
	ResolvedVersion string            `yaml:"resolvedVersion,omitempty"`
	Requires        []string          `yaml:"requires,omitempty"`
	ContentHash     string            `yaml:"contentHash"`
}

// LockedTool records the per-tool fingerprint of an MCP server's toolset at
// lock time. ToolsetHash is the integrity primary; LockedTool is for
// diagnostic purposes — it lets `ainfra check` identify by name which tool
// changed when the toolset hash drifts.
type LockedTool struct {
	Name            string `yaml:"name"`
	DescriptionHash string `yaml:"descriptionHash,omitempty"`
	InputSchemaHash string `yaml:"inputSchemaHash,omitempty"`
}

// PluginBaseline records the last released state of this repo's own plugin so
// `ainfra plugin release` can detect content drift. Written only by
// `ainfra plugin`; preserved untouched by lock/apply.
type PluginBaseline struct {
	Name        string `yaml:"name"`
	Version     string `yaml:"version"`
	ContentHash string `yaml:"contentHash"`
}

// SecretRef is a resolved secret placeholder recorded in the lockfile. It
// holds a reference only — never a value. ainfra exec resolves Ref at session
// time and exports Var into the child environment.
type SecretRef struct {
	Var    string `yaml:"var"`    // the AINFRA_SECRET_* environment variable name
	Ref    string `yaml:"ref"`    // the secret reference, e.g. op://Vault/item/field
	Scheme string `yaml:"scheme"` // op, env, ...
	Scope  string `yaml:"scope"`  // shared | personal
	Layer  string `yaml:"layer"`  // team | repo | personal
}
