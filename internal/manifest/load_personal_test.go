package manifest

import (
	"os"
	"path/filepath"
	"testing"
)

// writeManifest is a thin helper that writes a YAML manifest body to path,
// creating parent directories as needed.
func writeManifest(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// withGlobalPersonal points XDG_CONFIG_HOME at xdg for the test's lifetime, so
// loadGlobalPersonal reads xdg/ainfra/personal.yaml. The os.Setenv is undone
// automatically by t.Setenv.
func withGlobalPersonal(t *testing.T, xdg string) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", xdg)
}

func TestLoadLayersGlobalPersonalAbsent(t *testing.T) {
	dir := t.TempDir()
	xdg := t.TempDir() // empty XDG dir — no global file
	withGlobalPersonal(t, xdg)

	writeManifest(t, filepath.Join(dir, "ainfra.yaml"), "version: 1\n")
	layers, err := LoadLayers(dir)
	if err != nil {
		t.Fatalf("LoadLayers: %v", err)
	}
	if _, ok := layers[LayerPersonal]; ok {
		t.Errorf("missing personal sources must produce no LayerPersonal entry")
	}
}

func TestLoadLayersGlobalPersonalAlone(t *testing.T) {
	dir := t.TempDir()
	xdg := t.TempDir()
	withGlobalPersonal(t, xdg)

	writeManifest(t, filepath.Join(dir, "ainfra.yaml"), "version: 1\n")
	writeManifest(t, filepath.Join(xdg, "ainfra", "personal.yaml"), `version: 1
rules:
  my-style:
    source: ./style.md
`)
	layers, err := LoadLayers(dir)
	if err != nil {
		t.Fatalf("LoadLayers: %v", err)
	}
	p, ok := layers[LayerPersonal]
	if !ok {
		t.Fatal("global-only personal file must populate LayerPersonal")
	}
	if _, ok := p.Rules["my-style"]; !ok {
		t.Errorf("global rule not present in merged personal layer: %+v", p.Rules)
	}
}

func TestLoadLayersRepoPersonalOverridesGlobal(t *testing.T) {
	dir := t.TempDir()
	xdg := t.TempDir()
	withGlobalPersonal(t, xdg)

	writeManifest(t, filepath.Join(dir, "ainfra.yaml"), "version: 1\n")
	writeManifest(t, filepath.Join(xdg, "ainfra", "personal.yaml"), `version: 1
rules:
  shared:
    source: ./global-style.md
  global-only:
    source: ./global-only.md
`)
	writeManifest(t, filepath.Join(dir, "ainfra.personal.yaml"), `version: 1
rules:
  shared:
    source: ./repo-style.md
  repo-only:
    source: ./repo-only.md
`)
	layers, err := LoadLayers(dir)
	if err != nil {
		t.Fatalf("LoadLayers: %v", err)
	}
	p := layers[LayerPersonal]
	if p == nil {
		t.Fatal("LayerPersonal missing")
	}
	if got := p.Rules["shared"].Source; got != "./repo-style.md" {
		t.Errorf("repo personal must override global: got source %q", got)
	}
	if _, ok := p.Rules["global-only"]; !ok {
		t.Error("global-only rule lost after merge")
	}
	if _, ok := p.Rules["repo-only"]; !ok {
		t.Error("repo-only rule lost after merge")
	}
}

func TestLoadLayersGlobalPersonalDoesNotTouchRepoLayer(t *testing.T) {
	// A global personal entry must not leak into LayerRepo. Drift detection
	// of the committed lockfile relies on the team+repo layers being pristine.
	dir := t.TempDir()
	xdg := t.TempDir()
	withGlobalPersonal(t, xdg)

	writeManifest(t, filepath.Join(dir, "ainfra.yaml"), `version: 1
rules:
  team-style:
    source: ./team.md
`)
	writeManifest(t, filepath.Join(xdg, "ainfra", "personal.yaml"), `version: 1
rules:
  my-style:
    source: ./mine.md
`)
	layers, err := LoadLayers(dir)
	if err != nil {
		t.Fatalf("LoadLayers: %v", err)
	}
	repo := layers[LayerRepo]
	if repo == nil {
		t.Fatal("LayerRepo missing")
	}
	if _, ok := repo.Rules["my-style"]; ok {
		t.Error("global personal rule leaked into LayerRepo")
	}
	if _, ok := repo.Rules["team-style"]; !ok {
		t.Error("repo rule lost")
	}
}
