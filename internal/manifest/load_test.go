package manifest

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadLayersTagsEachLayer(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("ainfra.yaml", "version: 1\nmcpServers:\n  repo-srv: {command: x}\n")
	write("ainfra.personal.yaml", "version: 1\nmcpServers:\n  mine: {command: y}\n")

	layers, err := LoadLayers(dir)
	if err != nil {
		t.Fatalf("LoadLayers: %v", err)
	}
	if got := layers[LayerRepo].MCPServers["repo-srv"].Command; got != "x" {
		t.Errorf("repo layer command = %q", got)
	}
	if got := layers[LayerPersonal].MCPServers["mine"].Command; got != "y" {
		t.Errorf("personal layer command = %q", got)
	}
}

func TestLoadLayersPersonalOptional(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte("version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	layers, err := LoadLayers(dir)
	if err != nil {
		t.Fatalf("LoadLayers: %v", err)
	}
	if _, ok := layers[LayerPersonal]; ok {
		t.Error("personal layer should be absent when file missing")
	}
}
