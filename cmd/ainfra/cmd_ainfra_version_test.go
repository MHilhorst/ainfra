package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAinfraVersion_Mismatch_Warns: a repo pinning a different ainfra
// version surfaces a one-line stderr warning.
func TestAinfraVersion_Mismatch_Warns(t *testing.T) {
	dir := t.TempDir()
	yaml := `version: 1
ainfraVersion: "99.0.0"
mcpServers:
  fs:
    transport: stdio
    command: npx
    args: ["-y", "pkg"]
    version: "0.1.0"
`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"--chdir", dir, "lock"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("lock failed")
	}

	var errOut bytes.Buffer
	_ = run([]string{"--chdir", dir, "install", "--dry-run"}, &bytes.Buffer{}, &errOut)
	got := errOut.String()
	if !strings.Contains(got, "this repo expects ainfra 99.0.0") {
		t.Errorf("expected version-mismatch warning, got: %q", got)
	}
}

// TestAinfraVersion_NoField_Silent: a repo without the field doesn't warn.
func TestAinfraVersion_NoField_Silent(t *testing.T) {
	dir := t.TempDir()
	yaml := `version: 1
mcpServers:
  fs:
    transport: stdio
    command: npx
    args: ["-y", "pkg"]
    version: "0.1.0"
`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"--chdir", dir, "lock"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("lock failed")
	}

	var errOut bytes.Buffer
	_ = run([]string{"--chdir", dir, "install", "--dry-run"}, &bytes.Buffer{}, &errOut)
	if strings.Contains(errOut.String(), "this repo expects ainfra") {
		t.Errorf("unexpected version warning on a manifest with no ainfraVersion: %q", errOut.String())
	}
}

// TestAinfraVersion_Quiet_Suppresses: AINFRA_QUIET=1 suppresses the warning.
func TestAinfraVersion_Quiet_Suppresses(t *testing.T) {
	dir := t.TempDir()
	yaml := `version: 1
ainfraVersion: "99.0.0"
mcpServers:
  fs:
    transport: stdio
    command: npx
    args: ["-y", "pkg"]
    version: "0.1.0"
`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"--chdir", dir, "lock"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("lock failed")
	}
	t.Setenv("AINFRA_QUIET", "1")

	var errOut bytes.Buffer
	_ = run([]string{"--chdir", dir, "install", "--dry-run"}, &bytes.Buffer{}, &errOut)
	if strings.Contains(errOut.String(), "this repo expects ainfra") {
		t.Errorf("AINFRA_QUIET should suppress version warning, got: %q", errOut.String())
	}
}
