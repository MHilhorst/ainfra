package plugin

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"

	"github.com/MHilhorst/ainfra/internal/manifest"
)

// pluginJSON is the on-disk shape of .claude-plugin/plugin.json. Field order
// here is the emitted key order.
type pluginJSON struct {
	Name string `json:"name"`
	// Version is omitted when empty so Claude Code falls through to the next
	// resolver in its precedence chain (marketplace entry, then commit SHA).
	// Emitting "" would pin every user to the empty string forever.
	Version     string                 `json:"version,omitempty"`
	Description string                 `json:"description"`
	Author      *manifest.PluginAuthor `json:"author,omitempty"`
	Repository  string                 `json:"repository,omitempty"`
	License     string                 `json:"license,omitempty"`
	Skills      []string               `json:"skills"`
	Agents      []string               `json:"agents"`
}

// AgentsRefs returns the plugin.json agents entries: one "./agents/<file>.md"
// per markdown file under root/agents, when the plugin's content paths declare
// the agents directory. The plugin.json schema requires explicit .md file
// paths (a bare directory fails `claude plugin validate`), so the directory is
// enumerated at build time; sorted for deterministic output.
func AgentsRefs(root string, p manifest.PluginBuild) []string {
	refs := []string{}
	for _, c := range p.ContentPaths() {
		if strings.Trim(strings.TrimPrefix(c, "./"), "/") != "agents" {
			continue
		}
		matches, err := filepath.Glob(filepath.Join(root, "agents", "*.md"))
		if err != nil {
			return refs
		}
		for _, m := range matches {
			refs = append(refs, "./agents/"+filepath.Base(m))
		}
		sort.Strings(refs)
		return refs
	}
	return refs
}

// RenderPluginJSON produces the bytes of .claude-plugin/plugin.json for the
// given build block, version, and agents entries (from AgentsRefs), with
// 2-space indent and a trailing newline.
func RenderPluginJSON(p manifest.PluginBuild, version string, agents []string) ([]byte, error) {
	if agents == nil {
		agents = []string{}
	}
	doc := pluginJSON{
		Name:        p.Name,
		Version:     version,
		Description: p.Description,
		Repository:  p.Repository,
		License:     p.License,
		Skills:      []string{"./skills/"},
		Agents:      agents,
	}
	if p.Author.Name != "" || p.Author.URL != "" {
		a := p.Author
		doc.Author = &a
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}
