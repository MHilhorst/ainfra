package adopt

import (
	"os"
	"path/filepath"

	"github.com/MHilhorst/ainfra/internal/manifest"
)

// Scan reads dir and returns a draft manifest derived from any .mcp.json,
// .claude/, CLAUDE.md, or AGENTS.md content it finds. The returned manifest is
// always at least the empty version: 1 shell; channel maps remain nil when no
// corresponding source is present so emit can omit them entirely.
func Scan(dir string) (manifest.Manifest, []Warning, error) {
	var warnings []Warning
	m := manifest.Manifest{Version: 1}

	mcpServers, secrets, ws, err := readMCP(dir)
	if err != nil {
		return manifest.Manifest{}, nil, err
	}
	warnings = append(warnings, ws...)
	if len(mcpServers) > 0 {
		m.MCPServers = mcpServers
	}
	if len(secrets) > 0 {
		m.Secrets = secrets
	}

	hooks, ws, err := readHooks(dir)
	if err != nil {
		return manifest.Manifest{}, nil, err
	}
	warnings = append(warnings, ws...)
	if len(hooks) > 0 {
		m.Hooks = hooks
	}
	if hooksDirExists(dir) {
		warnings = append(warnings, Warning{
			Message: "adopt: found .claude/hooks/ — bundled hook scripts must be re-declared via ainfra hook entries; review manually",
		})
	}

	cmds, err := readCommands(dir)
	if err != nil {
		return manifest.Manifest{}, nil, err
	}
	if len(cmds) > 0 {
		m.Commands = cmds
	}

	if rules := readRules(dir); len(rules) > 0 {
		m.Rules = rules
	}

	// Agent detection: prefer claude-code when .claude/ is present; otherwise
	// look for a Codex marker. The marker is intentionally repo-local so tests
	// can drive detection from a fixture.
	if hasClaudeDir(dir) {
		m.Agent = "claude-code"
	} else if hasCodexMarker(dir) {
		m.Agent = "codex"
	}

	// Standing notes about ingestion gaps the caller should know about.
	if settingsExists(dir) {
		warnings = append(warnings, Warning{
			Message: "adopt: tool permission ingestion deferred — review .claude/settings.json manually",
		})
	}

	return m, warnings, nil
}

func hasClaudeDir(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, ".claude"))
	return err == nil && info.IsDir()
}

func hasCodexMarker(dir string) bool {
	if _, err := os.Stat(filepath.Join(dir, ".codex", "config.toml")); err == nil {
		return true
	}
	return false
}

func settingsExists(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".claude", "settings.json"))
	return err == nil
}
