package plugin

import (
	"encoding/json"

	"github.com/MHilhorst/ainfra/internal/manifest"
)

// pluginJSON is the on-disk shape of .claude-plugin/plugin.json. Field order
// here is the emitted key order.
type pluginJSON struct {
	Name        string                 `json:"name"`
	Version     string                 `json:"version"`
	Description string                 `json:"description"`
	Author      *manifest.PluginAuthor `json:"author,omitempty"`
	Repository  string                 `json:"repository,omitempty"`
	License     string                 `json:"license,omitempty"`
	Skills      []string               `json:"skills"`
	Agents      []string               `json:"agents"`
}

// RenderPluginJSON produces the bytes of .claude-plugin/plugin.json for the
// given build block and version (2-space indent, trailing newline).
func RenderPluginJSON(p manifest.PluginBuild, version string) ([]byte, error) {
	doc := pluginJSON{
		Name:        p.Name,
		Version:     version,
		Description: p.Description,
		Repository:  p.Repository,
		License:     p.License,
		Skills:      []string{"./skills/"},
		Agents:      []string{},
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
