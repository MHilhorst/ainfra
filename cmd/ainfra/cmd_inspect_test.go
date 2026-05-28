package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MHilhorst/ainfra/internal/cli"
)

// runInspectIn drives the inspect command against dir with --no-color so
// status markers compare cleanly across terminals.
func runInspectIn(t *testing.T, dir string, args ...string) (string, string, int) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	fullArgs := append([]string{"--chdir", dir, "--no-color", "inspect"}, args...)
	code := run(fullArgs, &stdout, &stderr)
	_ = cli.Context{} // keep import for future use
	return stdout.String(), stderr.String(), code
}

// A virgin repo with no ainfra.yaml and no .claude/ should produce a no-op
// report that nudges the user toward `ainfra init --adopt`.
func TestInspect_VirginRepoSuggestsAdopt(t *testing.T) {
	dir := t.TempDir()

	stdout, stderr, code := runInspectIn(t, dir)
	if code != 0 {
		t.Fatalf("inspect exit code = %d, stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, "no ainfra.yaml") {
		t.Errorf("expected 'no ainfra.yaml' in stdout, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "ainfra init --adopt") {
		t.Errorf("expected 'ainfra init --adopt' hint in stdout, got:\n%s", stdout)
	}
}

// A repo where .mcp.json declares a server but ainfra.yaml is absent must
// classify that server as untracked and surface the adopt hint.
func TestInspect_UntrackedMCPServer(t *testing.T) {
	dir := t.TempDir()
	mcp := `{"mcpServers":{"demo":{"command":"npx","args":["@demo/pkg"]}}}`
	if err := os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(mcp), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := runInspectIn(t, dir)
	if code != 0 {
		t.Fatalf("inspect exit code = %d, stderr=%q", code, stderr)
	}
	for _, want := range []string{"demo", "local-only", "ainfra init --adopt"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("expected %q in stdout, got:\n%s", want, stdout)
		}
	}
}

// --json must emit a parseable report including the summary counts.
func TestInspect_JSONOutput(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(`{"mcpServers":{"x":{"command":"npx"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := runInspectIn(t, dir, "--json")
	if code != 0 {
		t.Fatalf("inspect --json exit code = %d, stderr=%q", code, stderr)
	}
	var report inspectReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("not valid JSON: %v\nstdout=%s", err, stdout)
	}
	if report.HasManifest {
		t.Errorf("HasManifest = true, want false (no ainfra.yaml in fixture)")
	}
	var foundX bool
	for _, r := range report.Rows {
		if r.Channel == "mcpServers" && r.ID == "x" {
			foundX = true
			if r.Status != statusUntracked {
				t.Errorf("server x status = %q, want %q", r.Status, statusUntracked)
			}
		}
	}
	if !foundX {
		t.Errorf("server 'x' not in report.Rows:\n%+v", report.Rows)
	}
}

// Personal-layer entries (from the user's global ~/.config/ainfra/personal.yaml)
// should be hidden by default — they aren't specific to the repo being
// inspected. --all opts back in. Reuses the package TestMain XDG isolation
// to seed a personal layer from a temp dir.
func TestInspect_HidesPersonalLayerByDefault(t *testing.T) {
	xdg := os.Getenv("XDG_CONFIG_HOME")
	if xdg == "" {
		t.Skip("XDG_CONFIG_HOME not isolated by TestMain")
	}
	ainfraDir := filepath.Join(xdg, "ainfra")
	if err := os.MkdirAll(ainfraDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Materialize the source file so the personal layer's command isn't
	// classified as missing.
	cmdFile := filepath.Join(ainfraDir, "mycmd.md")
	if err := os.WriteFile(cmdFile, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	personalYAML := "version: 1\ncommands:\n  mycmd:\n    source: " + cmdFile + "\n"
	if err := os.WriteFile(filepath.Join(ainfraDir, "personal.yaml"), []byte(personalYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(ainfraDir) })

	dir := t.TempDir()
	// Default run should not list the personal-layer command.
	stdout, stderr, code := runInspectIn(t, dir)
	if code != 0 {
		t.Fatalf("inspect exit code = %d, stderr=%q", code, stderr)
	}
	if strings.Contains(stdout, "mycmd") {
		t.Errorf("default inspect surfaced personal-layer command mycmd; got:\n%s", stdout)
	}
	// --all run must surface it.
	stdoutAll, stderrAll, codeAll := runInspectIn(t, dir, "--all")
	if codeAll != 0 {
		t.Fatalf("inspect --all exit code = %d, stderr=%q", codeAll, stderrAll)
	}
	if !strings.Contains(stdoutAll, "mycmd") {
		t.Errorf("inspect --all did not surface personal-layer command mycmd; got:\n%s", stdoutAll)
	}
}

// .claude/mcp.json with the older "servers" key (instead of root .mcp.json
// with "mcpServers") must surface — without this fallback, inspect silently
// reports a repo as having no MCP config when it actually has plenty.
func TestInspect_FallbackMCPPathAndServersKey(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"servers":{"postgres":{"command":"npx","args":["-y","@modelcontextprotocol/server-postgres"]}}}`
	if err := os.WriteFile(filepath.Join(dir, ".claude", "mcp.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := runInspectIn(t, dir)
	if code != 0 {
		t.Fatalf("inspect exit code = %d, stderr=%q", code, stderr)
	}
	for _, want := range []string{"postgres", "local-only"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("expected %q in stdout (fallback MCP path/schema), got:\n%s", want, stdout)
		}
	}
}

// A rule whose id collides with the personal layer (the canonical case is
// CLAUDE.md vs the personal claude-md rule) must still surface when the
// on-disk file lives inside the repo. Otherwise the default --all-off view
// hides the repo's own CLAUDE.md, which is exactly the file users come to
// inspect for.
func TestInspect_RepoLocalCLAUDEMDNotHiddenByPersonalCollision(t *testing.T) {
	xdg := os.Getenv("XDG_CONFIG_HOME")
	if xdg == "" {
		t.Skip("XDG_CONFIG_HOME not isolated by TestMain")
	}
	ainfraDir := filepath.Join(xdg, "ainfra")
	if err := os.MkdirAll(ainfraDir, 0o755); err != nil {
		t.Fatal(err)
	}
	homeRule := filepath.Join(ainfraDir, "fake-home-CLAUDE.md")
	if err := os.WriteFile(homeRule, []byte("home"), 0o644); err != nil {
		t.Fatal(err)
	}
	personalYAML := "version: 1\nrules:\n  claude-md:\n    source: " + homeRule + "\n    target: CLAUDE.md\n"
	if err := os.WriteFile(filepath.Join(ainfraDir, "personal.yaml"), []byte(personalYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(ainfraDir) })

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("repo content"), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := runInspectIn(t, dir)
	if code != 0 {
		t.Fatalf("inspect exit code = %d, stderr=%q", code, stderr)
	}
	if !strings.Contains(stdout, "claude-md") {
		t.Errorf("expected repo-local claude-md to surface despite personal-layer collision, got:\n%s", stdout)
	}
}

// A manifest entry whose source file does not exist must be flagged missing
// so the user knows to run `ainfra install`.
func TestInspect_MissingDeclaredCommand(t *testing.T) {
	dir := t.TempDir()
	body := `version: 1
commands:
  not-on-disk:
    source: ./commands/not-on-disk.md
    description: declared but never materialized
`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := runInspectIn(t, dir)
	if code != 0 {
		t.Fatalf("inspect exit code = %d, stderr=%q", code, stderr)
	}
	for _, want := range []string{"not-on-disk", "not installed", "ainfra install"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("expected %q in stdout, got:\n%s", want, stdout)
		}
	}
}
