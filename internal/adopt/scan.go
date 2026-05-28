package adopt

import (
	"os"
	"path/filepath"

	"github.com/MHilhorst/ainfra/internal/manifest"
)

// Layout describes where on disk each adoptable artefact lives. It lets Scan
// work uniformly against either a repo (artefacts under <dir>/.claude/) or the
// user-global Claude config (artefacts under <home>/.claude/), without each
// reader needing to know which mode it's in.
type Layout struct {
	// MCPFile is the .mcp.json path. Empty means skip MCP scanning entirely.
	MCPFile string
	// MCPFileFallback is an alternate MCP file consulted when MCPFile is
	// missing or yields no servers. Older Claude Code conventions kept this
	// at <repo>/.claude/mcp.json; the rest of ainfra prefers <repo>/.mcp.json.
	MCPFileFallback string
	// SettingsFile is the Claude Code settings.json path containing the
	// "hooks" block. Empty means skip hook scanning.
	SettingsFile string
	// CommandsDir is the directory containing per-command markdown files.
	// Empty means skip command scanning.
	CommandsDir string
	// SkillsDir is the directory containing one skill per subdirectory.
	// Each subdirectory becomes a Skill entry. Empty means skip skill
	// scanning.
	SkillsDir string
	// SkillsSourceBase is the path prefix written into Skill.Source for
	// each scanned directory (relative for repo scope, absolute for user
	// scope).
	SkillsSourceBase string
	// CommandsSourceBase is the path prefix written into Command.Source for
	// each scanned file ("./.claude/commands" for repo scope, an absolute
	// path for user scope).
	CommandsSourceBase string
	// HooksScriptDir is the directory that, if present, triggers a warning
	// about bundled hook scripts that must be re-declared.
	HooksScriptDir string
	// Rules is the list of context files to materialize as rules entries.
	Rules []RuleSource
	// Agent is the agent identifier to record (e.g. "claude-code", "codex").
	// May be empty to defer to detection inside Scan.
	Agent string
	// AgentDetectDir is consulted only when Agent is empty: presence of
	// <dir>/.claude or <dir>/.codex/config.toml selects the agent.
	AgentDetectDir string
	// ExtraWarnings are emitted alongside the scan output. Used for
	// scope-specific notes the readers don't surface (e.g. user-scope MCP
	// adoption deferred).
	ExtraWarnings []Warning
}

// RuleSource is one context file slot to consider during adoption.
type RuleSource struct {
	ID     string // manifest rule id
	Path   string // filesystem path used for the existence check
	Source string // value written to manifest.Rule.Source
	Target string // value written to manifest.Rule.Target
}

// RepoLayout returns the conventional repo layout: artefacts live under
// <dir>/.claude/ with CLAUDE.md / AGENTS.md at the repo root. This preserves
// the original Scan(dir) behaviour.
func RepoLayout(dir string) Layout {
	return Layout{
		MCPFile:            filepath.Join(dir, ".mcp.json"),
		MCPFileFallback:    filepath.Join(dir, ".claude", "mcp.json"),
		SettingsFile:       filepath.Join(dir, ".claude", "settings.json"),
		CommandsDir:        filepath.Join(dir, ".claude", "commands"),
		CommandsSourceBase: "./.claude/commands",
		SkillsDir:          filepath.Join(dir, ".claude", "skills"),
		SkillsSourceBase:   "./.claude/skills",
		HooksScriptDir:     filepath.Join(dir, ".claude", "hooks"),
		Rules: []RuleSource{
			{ID: "claude-md", Path: filepath.Join(dir, "CLAUDE.md"), Source: "./CLAUDE.md", Target: "CLAUDE.md"},
			{ID: "agents-md", Path: filepath.Join(dir, "AGENTS.md"), Source: "./AGENTS.md", Target: "AGENTS.md"},
		},
		AgentDetectDir: dir,
	}
}

// UserLayout returns the user-global Claude Code layout rooted at home. MCP
// scanning is intentionally disabled (the user-level config lives in
// ~/.claude.json with a different schema than .mcp.json and would need a
// separate ingester); a warning is emitted so the user knows what was skipped.
// Rule and command Source values are absolute paths because the resulting
// manifest lives outside any repo tree and has no usable relative root.
func UserLayout(home string) Layout {
	claude := filepath.Join(home, ".claude")
	var warnings []Warning
	if _, err := os.Stat(filepath.Join(home, ".claude.json")); err == nil {
		warnings = append(warnings, Warning{
			Message: "adopt: user-scope MCP servers in ~/.claude.json are not auto-adopted; declare them in personal.yaml manually if needed",
		})
	}
	return Layout{
		MCPFile:            "",
		SettingsFile:       filepath.Join(claude, "settings.json"),
		CommandsDir:        filepath.Join(claude, "commands"),
		CommandsSourceBase: filepath.Join(claude, "commands"),
		SkillsDir:          filepath.Join(claude, "skills"),
		SkillsSourceBase:   filepath.Join(claude, "skills"),
		HooksScriptDir:     filepath.Join(claude, "hooks"),
		Rules: []RuleSource{
			{
				ID:     "claude-md",
				Path:   filepath.Join(claude, "CLAUDE.md"),
				Source: filepath.Join(claude, "CLAUDE.md"),
				Target: "CLAUDE.md",
			},
		},
		Agent:         "claude-code",
		ExtraWarnings: warnings,
	}
}

// Scan reads the repo at dir and returns a draft manifest. It is a thin
// wrapper over ScanLayout(RepoLayout(dir)) preserved for callers that pre-date
// the layout split.
func Scan(dir string) (manifest.Manifest, []Warning, error) {
	return ScanLayout(RepoLayout(dir))
}

// ScanLayout walks the artefacts named by layout and returns a draft manifest.
// Channels remain nil when no source is present so emit can omit them.
func ScanLayout(layout Layout) (manifest.Manifest, []Warning, error) {
	var warnings []Warning
	m := manifest.Manifest{Version: 1}

	if layout.MCPFile != "" {
		mcpServers, secrets, ws, err := readMCP(layout.MCPFile)
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
	}
	// Older Claude Code conventions stored MCP config at .claude/mcp.json
	// (often with a "servers" key). Read that as a fallback whenever the
	// primary file produced nothing — so adopt captures these repos out of
	// the box instead of forcing users to relocate or hand-edit.
	if layout.MCPFileFallback != "" && len(m.MCPServers) == 0 {
		mcpServers, secrets, ws, err := readMCP(layout.MCPFileFallback)
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
	}

	if layout.SettingsFile != "" {
		hooks, ws, err := readHooks(layout.SettingsFile)
		if err != nil {
			return manifest.Manifest{}, nil, err
		}
		warnings = append(warnings, ws...)
		if len(hooks) > 0 {
			m.Hooks = hooks
		}
	}
	if layout.HooksScriptDir != "" && dirExists(layout.HooksScriptDir) {
		warnings = append(warnings, Warning{
			Message: "adopt: found " + layout.HooksScriptDir + " — bundled hook scripts must be re-declared via ainfra hook entries; review manually",
		})
	}

	if layout.CommandsDir != "" {
		cmds, err := readCommands(layout.CommandsDir, layout.CommandsSourceBase)
		if err != nil {
			return manifest.Manifest{}, nil, err
		}
		if len(cmds) > 0 {
			m.Commands = cmds
		}
	}

	if layout.SkillsDir != "" {
		skills, err := readSkills(layout.SkillsDir, layout.SkillsSourceBase)
		if err != nil {
			return manifest.Manifest{}, nil, err
		}
		if len(skills) > 0 {
			m.Skills = skills
		}
	}

	if rules := readRules(layout.Rules); len(rules) > 0 {
		m.Rules = rules
	}

	if layout.Agent != "" {
		m.Agent = layout.Agent
	} else if layout.AgentDetectDir != "" {
		if hasClaudeDir(layout.AgentDetectDir) {
			m.Agent = "claude-code"
		} else if hasCodexMarker(layout.AgentDetectDir) {
			m.Agent = "codex"
		}
	}

	if layout.SettingsFile != "" {
		if _, err := os.Stat(layout.SettingsFile); err == nil {
			warnings = append(warnings, Warning{
				Message: "adopt: tool permission ingestion deferred — review " + layout.SettingsFile + " manually",
			})
		}
	}

	warnings = append(warnings, layout.ExtraWarnings...)
	return m, warnings, nil
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
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
