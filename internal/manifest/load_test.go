package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MHilhorst/ainfra/internal/diag"
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

// A misspelled key must be a hard error, not a silent drop — the core
// config-as-code promise (design §13).
func TestLoadLayersRejectsUnknownKey(t *testing.T) {
	dir := t.TempDir()
	// "mcpServer" — missing the trailing s — is a classic, costly typo.
	body := "version: 1\nmcpServer:\n  oops: {command: x}\n"
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadLayers(dir)
	if err == nil {
		t.Fatal("expected an error for the unknown key mcpServer")
	}
	d, ok := err.(*diag.Diagnostic)
	if !ok {
		t.Fatalf("error is %T, want *diag.Diagnostic", err)
	}
	if !strings.Contains(d.Detail, "mcpServer") {
		t.Errorf("detail = %q, want it to name the offending key", d.Detail)
	}
}

func TestLoadLayersRejectsWrongVersion(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte("version: 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadLayers(dir)
	d, ok := err.(*diag.Diagnostic)
	if !ok {
		t.Fatalf("error is %T, want *diag.Diagnostic: %v", err, err)
	}
	if !strings.Contains(d.Summary, "unsupported manifest version") {
		t.Errorf("summary = %q", d.Summary)
	}
}
