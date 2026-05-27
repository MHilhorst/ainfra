package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MHilhorst/ainfra/internal/cli"
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

// TestApplyAliasPrintsDeprecation verifies apply still works but warns once.
func TestApplyAliasPrintsDeprecation(t *testing.T) {
	cli.ResetDeprecationFiredForTest()
	dir := newDemoRepo(t)
	if code := run([]string{"--chdir", dir, "lock"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("lock failed")
	}

	var errOut bytes.Buffer
	code := run([]string{"--chdir", dir, "apply", "--yes"}, &bytes.Buffer{}, &errOut)
	if code != 0 {
		t.Fatalf("apply --yes: code=%d err=%q", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "deprecated") || !strings.Contains(errOut.String(), "install") {
		t.Errorf("apply: expected deprecation note pointing at install, got %q", errOut.String())
	}
}

// TestApplyAliasDeprecationFiresOnce confirms the once-per-process latch.
func TestApplyAliasDeprecationFiresOnce(t *testing.T) {
	cli.ResetDeprecationFiredForTest()
	dir := newDemoRepo(t)
	if code := run([]string{"--chdir", dir, "lock"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("lock failed")
	}

	var errOut bytes.Buffer
	_ = run([]string{"--chdir", dir, "apply", "--yes"}, &bytes.Buffer{}, &errOut)
	_ = run([]string{"--chdir", dir, "apply", "--yes"}, &bytes.Buffer{}, &errOut)
	count := strings.Count(errOut.String(), "is deprecated")
	if count != 1 {
		t.Errorf("apply called twice: want 1 deprecation line, got %d (stderr=%q)", count, errOut.String())
	}
}

// TestHelpListsInstallNotApply verifies the front-page surface.
func TestHelpListsInstallNotApply(t *testing.T) {
	var out bytes.Buffer
	run([]string{"--help"}, &out, &bytes.Buffer{})
	s := out.String()
	if !strings.Contains(s, "install") {
		t.Errorf("--help missing 'install': %q", s)
	}
	// apply/plan/check/validate are now hidden — they should not appear in the overview.
	for _, hidden := range []string{"  apply ", "  plan ", "  check ", "  validate ", "  schema ", "  sync ", "  exec "} {
		if strings.Contains(s, hidden) {
			t.Errorf("--help should not list %q: %q", hidden, s)
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
