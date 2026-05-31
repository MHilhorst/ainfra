package manifest

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_PluginBlock(t *testing.T) {
	dir := t.TempDir()
	src := `version: 1
agent: claude-code
plugin:
  name: tvt-config
  description: "Team config"
  marketplace: trein-vertraging
  author: { name: Trein-Vertraging, url: https://github.com/trein-vertraging }
  repository: https://github.com/trein-vertraging/claude-config
  license: UNLICENSED
  content: [ skills/, .mcp.json ]
`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	layers, err := LoadLayers(dir)
	if err != nil {
		t.Fatalf("LoadLayers: %v", err)
	}
	repo := layers[LayerRepo]
	if repo == nil || repo.Plugin == nil {
		t.Fatal("expected plugin block to be parsed")
	}
	if repo.Plugin.Name != "tvt-config" || repo.Plugin.Marketplace != "trein-vertraging" {
		t.Errorf("got name=%q marketplace=%q", repo.Plugin.Name, repo.Plugin.Marketplace)
	}
	if repo.Plugin.Author.Name != "Trein-Vertraging" {
		t.Errorf("got author name %q", repo.Plugin.Author.Name)
	}
	if len(repo.Plugin.Content) != 2 || repo.Plugin.Content[0] != "skills/" {
		t.Errorf("got content %v", repo.Plugin.Content)
	}
}
