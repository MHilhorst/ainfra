package lockfile

// Lock is one ainfra.lock file (spec Phase 2).
type Lock struct {
	Version      int     `yaml:"version"`
	GeneratedAt  string  `yaml:"generatedAt"`
	ManifestHash string  `yaml:"manifestHash,omitempty"`
	Entries      Entries `yaml:"entries"`
}

// Entries groups lock entries by channel.
type Entries struct {
	MCPServers         map[string]Entry `yaml:"mcpServers"`
	BackgroundServices map[string]Entry `yaml:"backgroundServices"`
	Hooks              map[string]Entry `yaml:"hooks"`
	Commands           map[string]Entry `yaml:"commands"`
	CLITools           map[string]Entry `yaml:"cliTools"`
	Skills             map[string]Entry `yaml:"skills"`
	Plugins            map[string]Entry `yaml:"plugins"`
	Rules              map[string]Entry `yaml:"rules"`
	Tools              map[string]Entry `yaml:"tools"`
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
	Requires        []string       `yaml:"requires,omitempty"`
	ContentHash     string         `yaml:"contentHash"`
}
