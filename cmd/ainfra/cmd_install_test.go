package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestInstallYes verifies the renamed verb behaves identically to apply.
func TestInstallYes(t *testing.T) {
	dir := newDemoRepo(t)
	if code := run([]string{"--chdir", dir, "lock"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("lock failed")
	}

	var out, errOut bytes.Buffer
	code := run([]string{"--chdir", dir, "install", "--yes"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("install --yes: code=%d err=%q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "Apply complete") {
		t.Errorf("install --yes: expected 'Apply complete' in stdout, got %q", out.String())
	}
}

// TestInstallDryRunStrict_NoDrift exits 0 when nothing has drifted.
func TestInstallDryRunStrict_NoDrift(t *testing.T) {
	dir := newDemoRepo(t)
	if code := run([]string{"--chdir", dir, "lock"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("lock failed")
	}
	if code := run([]string{"--chdir", dir, "install", "--yes"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("install --yes failed")
	}

	var out bytes.Buffer
	code := run([]string{"--chdir", dir, "install", "--dry-run", "--strict"}, &out, &bytes.Buffer{})
	if code != 0 {
		t.Fatalf("install --dry-run --strict (clean): code=%d out=%q", code, out.String())
	}
}

// TestInstallDryRunStrict_WithDrift exits 1 when there's anything to do.
func TestInstallDryRunStrict_WithDrift(t *testing.T) {
	dir := newDemoRepo(t)
	if code := run([]string{"--chdir", dir, "lock"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("lock failed")
	}
	// Don't run install — there's drift between manifest and machine.

	var out bytes.Buffer
	code := run([]string{"--chdir", dir, "install", "--dry-run", "--strict"}, &out, &bytes.Buffer{})
	if code != 1 {
		t.Fatalf("install --dry-run --strict (drift): want code=1, got code=%d out=%q", code, out.String())
	}
}

// TestHelpListsInstall verifies the front-page surface.
func TestHelpListsInstall(t *testing.T) {
	var out bytes.Buffer
	run([]string{"--help"}, &out, &bytes.Buffer{})
	s := out.String()
	if !strings.Contains(s, "install") {
		t.Errorf("--help missing 'install': %q", s)
	}
	// Former alias verbs were removed — they must not appear at all.
	for _, gone := range []string{" apply ", " plan ", " check ", " validate ", " schema ", " sync ", " exec ", " history "} {
		if strings.Contains(s, gone) {
			t.Errorf("--help should not list %q: %q", gone, s)
		}
	}
}

// newDemoRepo writes a minimal ainfra.yaml with one templated MCP server to
// dir and returns it. Shared helper for install/apply tests.
func newDemoRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	yaml := `version: 1
templates:
  fs-scoped:
    params: { root: { type: string, required: true } }
    produces:
      mcpServer:
        transport: stdio
        command: npx
        args: ["-y", "@modelcontextprotocol/server-filesystem", "${params.root}"]
        version: "0.6.2"
mcpServers:
  repo:
    template: fs-scoped
    params: { root: "." }
`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}
