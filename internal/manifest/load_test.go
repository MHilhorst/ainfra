package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MHilhorst/ainfra/internal/diag"
	"github.com/MHilhorst/ainfra/internal/version"
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

// manifestDir drops an ainfra.yaml into a fresh temp dir and returns the dir.
func manifestDir(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// withVersion pins the reported build version for one test.
func withVersion(t *testing.T, v string) {
	t.Helper()
	prev := version.Version
	version.Version = v
	t.Cleanup(func() { version.Version = prev })
}

func TestLoadLayersBlamesTheBinaryWhenTheRepoPinsANewerAinfra(t *testing.T) {
	// The field is not a typo — this build is old. Reporting "field target not
	// found" sends a whole team hunting a manifest bug that does not exist.
	withVersion(t, "0.2.27")
	// `notYetAField` stands in for whatever a future release adds. Using a
	// field this build genuinely lacks is the point: pick one it already has
	// and the decode succeeds, testing nothing.
	dir := manifestDir(t, "version: 1\nainfraVersion: \"0.3.0\"\ncommands:\n  pr: {source: ./p.md, notYetAField: x}\n")

	_, err := LoadLayers(dir)
	d, ok := err.(*diag.Diagnostic)
	if !ok {
		t.Fatalf("error is %T, want *diag.Diagnostic: %v", err, err)
	}
	if !strings.Contains(d.Summary, "0.3.0") || !strings.Contains(d.Summary, "0.2.27") {
		t.Errorf("summary = %q, want both the required and the running version", d.Summary)
	}
	if !strings.Contains(d.Hint, "brew upgrade") {
		t.Errorf("hint = %q, want the actual recovery command", d.Hint)
	}
	if !strings.Contains(d.Detail, "notYetAField") {
		t.Errorf("detail = %q, should still name the field so a real typo stays diagnosable", d.Detail)
	}
}

func TestLoadLayersStillReportsATypoWhenTheBinaryIsCurrent(t *testing.T) {
	// Same shape of failure, but the pin is satisfied — so the manifest really
	// is wrong and an upgrade prompt would be a lie.
	withVersion(t, "0.2.28")
	dir := manifestDir(t, "version: 1\nainfraVersion: \"0.2.28\"\nmcpServer:\n  oops: {command: x}\n")

	_, err := LoadLayers(dir)
	d, ok := err.(*diag.Diagnostic)
	if !ok {
		t.Fatalf("error is %T, want *diag.Diagnostic: %v", err, err)
	}
	if d.Summary != "manifest could not be parsed" {
		t.Errorf("summary = %q, want the parse error", d.Summary)
	}
}

func TestLoadLayersDoesNotTellADevBuildToUpgrade(t *testing.T) {
	// 0.0.0-dev is not a release. Ordering it against a pin would send someone
	// working on ainfra itself to Homebrew to "fix" their own working tree.
	withVersion(t, "0.0.0-dev")
	dir := manifestDir(t, "version: 1\nainfraVersion: \"0.2.28\"\nmcpServer:\n  oops: {command: x}\n")

	_, err := LoadLayers(dir)
	d, ok := err.(*diag.Diagnostic)
	if !ok {
		t.Fatalf("error is %T, want *diag.Diagnostic: %v", err, err)
	}
	if strings.Contains(d.Hint, "brew upgrade") {
		t.Errorf("a dev build must not be told to upgrade, hint = %q", d.Hint)
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
