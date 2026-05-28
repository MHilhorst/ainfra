package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/MHilhorst/ainfra/internal/adopt"
	"github.com/MHilhorst/ainfra/internal/manifest"
)

// scanGlobal enumerates every channel under home/.claude/ for the Global
// layer. Composes adopt's existing scanner (mcps/hooks/commands/rules) with
// channel-specific scanners for skills, plugins, agents, and settings.
func scanGlobal(home string) ([]Row, []FooterNote, error) {
	claude := filepath.Join(home, ".claude")
	if _, err := os.Stat(claude); err != nil {
		// No global .claude — return an empty inventory, not an error.
		// Per R2, audit must work on machines with no Claude config at all.
		return nil, nil, nil
	}
	layout := adopt.UserLayout(home)
	return scanLayer(LayerGlobal, layout, claude)
}

// scanProject enumerates every channel under dir/.claude/ for the Project
// layer. Reuses adopt.RepoLayout for adopt-known channels.
func scanProject(dir string) ([]Row, []FooterNote, error) {
	claude := filepath.Join(dir, ".claude")
	layout := adopt.RepoLayout(dir)
	return scanLayer(LayerProject, layout, claude)
}

// scanLayer is the shared body of scanGlobal/scanProject.
func scanLayer(layer Layer, layout adopt.Layout, claudeDir string) ([]Row, []FooterNote, error) {
	var rows []Row
	var notes []FooterNote

	// 1) adopt-known channels via ScanLayout. Note: adopt only enumerates
	// disk artifacts it knows how to round-trip into a manifest. For Global
	// scope adopt.UserLayout intentionally disables MCP scanning (see
	// adopt/scan.go) — audit accepts that and surfaces a detection-pending
	// note for the channel.
	m, _, err := adopt.ScanLayout(layout)
	if err != nil {
		return nil, nil, err
	}
	rows = append(rows, manifestToRows(layer, m)...)

	// 2) Skills / plugins / agents / settings — channels adopt does not
	// scan today. Per R7/R8 audit surfaces these from disk so unmanaged
	// entries appear in the inventory.
	rows = append(rows, scanSkills(layer, claudeDir)...)
	rows = append(rows, scanPlugins(layer, claudeDir)...)
	rows = append(rows, scanAgents(layer, claudeDir)...)
	rows = append(rows, scanSettings(layer, claudeDir)...)

	// 3) Detection-pending hint when the layout deliberately skipped a
	// channel (e.g. global MCP needs a different ingester per adopt
	// design). Surface it as a footer note rather than a row so the
	// inventory stays honest.
	if layer == LayerGlobal && layout.MCPFile == "" {
		notes = append(notes, FooterNote{
			Layer:   LayerGlobal,
			Message: "mcpServers detection pending — global MCP config (~/.claude.json) needs a separate ingester",
		})
	}

	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Channel != rows[j].Channel {
			return rows[i].Channel < rows[j].Channel
		}
		return rows[i].ID < rows[j].ID
	})
	return rows, notes, nil
}

// manifestToRows turns a scanned draft manifest (the shape adopt emits) into
// audit rows. Status defaults to Unmanaged; Reconcile rewrites it after
// cross-referencing with the lockfile.
func manifestToRows(layer Layer, m manifest.Manifest) []Row {
	var rows []Row
	for id := range m.MCPServers {
		rows = append(rows, Row{Layer: layer, Channel: "mcpServers", ID: id, Status: Status{Unmanaged: true}})
	}
	for id := range m.Hooks {
		rows = append(rows, Row{Layer: layer, Channel: "hooks", ID: id, Status: Status{Unmanaged: true}})
	}
	for id := range m.Commands {
		rows = append(rows, Row{Layer: layer, Channel: "commands", ID: id, Status: Status{Unmanaged: true}})
	}
	for id := range m.Rules {
		rows = append(rows, Row{Layer: layer, Channel: "rules", ID: id, Status: Status{Unmanaged: true}})
	}
	return rows
}

// scanSkills enumerates top-level entries under <claudeDir>/skills/. Each
// directory is one row; the presence of a SKILL.md surfaces in Detail.
func scanSkills(layer Layer, claudeDir string) []Row {
	dir := filepath.Join(claudeDir, "skills")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var rows []Row
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		row := Row{Layer: layer, Channel: "skills", ID: e.Name(), Status: Status{Unmanaged: true}}
		if _, err := os.Stat(filepath.Join(dir, e.Name(), "SKILL.md")); err == nil {
			row.Detail = "SKILL.md"
		}
		rows = append(rows, row)
	}
	return rows
}

// scanPlugins enumerates top-level entries under <claudeDir>/plugins/.
func scanPlugins(layer Layer, claudeDir string) []Row {
	dir := filepath.Join(claudeDir, "plugins")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var rows []Row
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		rows = append(rows, Row{Layer: layer, Channel: "plugins", ID: e.Name(), Status: Status{Unmanaged: true}})
	}
	return rows
}

// scanAgents enumerates .md files directly under <claudeDir>/agents/ and
// <claudeDir>/agents/<name>/agent.md. Each agent surfaces as one row keyed
// by its stem.
func scanAgents(layer Layer, claudeDir string) []Row {
	dir := filepath.Join(claudeDir, "agents")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var rows []Row
	for _, e := range entries {
		name := e.Name()
		switch {
		case !e.IsDir() && strings.HasSuffix(name, ".md"):
			id := strings.TrimSuffix(name, ".md")
			if seen[id] {
				continue
			}
			seen[id] = true
			rows = append(rows, Row{Layer: layer, Channel: "agents", ID: id, Status: Status{Unmanaged: true}})
		case e.IsDir():
			if _, err := os.Stat(filepath.Join(dir, name, "agent.md")); err == nil {
				if seen[name] {
					continue
				}
				seen[name] = true
				rows = append(rows, Row{Layer: layer, Channel: "agents", ID: name, Status: Status{Unmanaged: true}})
			}
		}
	}
	return rows
}

// scanSettings emits one row per settings file found under <claudeDir>/.
// settings.json is checked always; settings.local.json only at the Project
// layer per the Claude convention (it's the gitignored per-developer file).
// Detail summarizes notable fields without dumping file contents (R12).
func scanSettings(layer Layer, claudeDir string) []Row {
	var rows []Row
	if row, ok := readSettingsFile(layer, filepath.Join(claudeDir, "settings.json"), "settings.json", false); ok {
		rows = append(rows, row)
	}
	if layer == LayerProject {
		if row, ok := readSettingsFile(layer, filepath.Join(claudeDir, "settings.local.json"), "settings.local.json", true); ok {
			rows = append(rows, row)
		}
	}
	return rows
}

// readSettingsFile reads one settings*.json and returns a row summarizing
// notable fields. Missing file → (Row{}, false).
func readSettingsFile(layer Layer, path, id string, gitignored bool) (Row, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Row{}, false
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return Row{Layer: layer, Channel: "settings", ID: id, Status: Status{Unmanaged: true, Gitignored: gitignored}, Detail: "parse error"}, true
	}

	var parts []string

	// Permission allowlist count: settings.permissions.allow / deny / ask.
	if perm, ok := raw["permissions"].(map[string]any); ok {
		allow := lengthOfStringSlice(perm["allow"])
		deny := lengthOfStringSlice(perm["deny"])
		ask := lengthOfStringSlice(perm["ask"])
		total := allow + deny + ask
		if total > 0 {
			parts = append(parts, plural(total, "permission", "permissions"))
		}
	}

	if hooks, ok := raw["hooks"].(map[string]any); ok && len(hooks) > 0 {
		parts = append(parts, "hooks block")
	}
	if model, ok := raw["model"].(string); ok && model != "" {
		parts = append(parts, "model override")
	}
	if env, ok := raw["env"].(map[string]any); ok && len(env) > 0 {
		parts = append(parts, plural(len(env), "env var", "env vars"))
	}
	if len(parts) == 0 {
		parts = append(parts, "empty")
	}
	return Row{
		Layer:   layer,
		Channel: "settings",
		ID:      id,
		Status:  Status{Unmanaged: true, Gitignored: gitignored},
		Detail:  strings.Join(parts, " · "),
	}, true
}

func lengthOfStringSlice(v any) int {
	s, ok := v.([]any)
	if !ok {
		return 0
	}
	return len(s)
}

func plural(n int, singular, plural string) string {
	if n == 1 {
		return formatCount(n, singular)
	}
	return formatCount(n, plural)
}

func formatCount(n int, word string) string {
	return itoa(n) + " " + word
}

// itoa avoids pulling in strconv just for one call.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
