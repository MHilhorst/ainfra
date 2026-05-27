package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MHilhorst/ainfra/internal/check"
	"github.com/MHilhorst/ainfra/internal/mcpclient"
	"github.com/MHilhorst/ainfra/internal/resolve"
)

// withCheckRunner swaps check.IntrospectRunner for a test and restores it.
func withCheckRunner(t *testing.T, r mcpclient.Runner) {
	t.Helper()
	prev := check.IntrospectRunner
	check.IntrospectRunner = r
	t.Cleanup(func() { check.IntrospectRunner = prev })
}

// withLockRunner swaps resolve.IntrospectRunner for a test and restores it.
func withLockRunner(t *testing.T, r mcpclient.Runner) {
	t.Helper()
	prev := resolve.IntrospectRunner
	resolve.IntrospectRunner = r
	t.Cleanup(func() { resolve.IntrospectRunner = prev })
}

func okRunner(toolName, description string) *mcpclient.FakeRunner {
	body, _ := json.Marshal(map[string]any{"tools": []map[string]any{
		{"name": toolName, "description": description, "inputSchema": map[string]any{"type": "object"}},
	}})
	return &mcpclient.FakeRunner{
		Responses: map[string]json.RawMessage{
			"initialize": json.RawMessage(`{"protocolVersion":"2024-11-05","capabilities":{}}`),
			"tools/list": body,
		},
	}
}

func TestCheckToolsetDriftDetected(t *testing.T) {
	dir := t.TempDir()
	yaml := `version: 1
mcpServers:
  fs:
    command: fake-mcp
    transport: stdio
    version: "1.0.0"
`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	// Lock with one toolset; check sees a different live toolset.
	withLockRunner(t, okRunner("alpha", "old"))
	if code := run([]string{"--chdir", dir, "lock"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("lock failed")
	}
	if code := run([]string{"--chdir", dir, "apply", "--yes"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("apply failed")
	}

	withCheckRunner(t, okRunner("alpha", "new"))
	var out, errOut bytes.Buffer
	code := run([]string{"--chdir", dir, "check"}, &out, &errOut)
	if code == 0 {
		t.Fatalf("expected non-zero exit on toolset drift; out=%q err=%q", out.String(), errOut.String())
	}
	combined := out.String() + errOut.String()
	if !strings.Contains(combined, "Toolset drift") {
		t.Errorf("expected 'Toolset drift' in output; got: %q", combined)
	}
	if !strings.Contains(combined, "alpha") {
		t.Errorf("expected 'alpha' in output; got: %q", combined)
	}
	if !strings.Contains(combined, "description changed") {
		t.Errorf("expected 'description changed' in output; got: %q", combined)
	}
}

func TestCheckToolsetDriftClean(t *testing.T) {
	dir := t.TempDir()
	yaml := `version: 1
mcpServers:
  fs:
    command: fake-mcp
    transport: stdio
    version: "1.0.0"
`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	withLockRunner(t, okRunner("alpha", "a"))
	if code := run([]string{"--chdir", dir, "lock"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("lock failed")
	}
	if code := run([]string{"--chdir", dir, "apply", "--yes"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("apply failed")
	}

	withCheckRunner(t, okRunner("alpha", "a"))
	var out, errOut bytes.Buffer
	code := run([]string{"--chdir", dir, "check"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("expected clean check; code=%d out=%q err=%q", code, out.String(), errOut.String())
	}
	combined := out.String() + errOut.String()
	if !strings.Contains(combined, "No drift") {
		t.Errorf("expected 'No drift'; got: %q", combined)
	}
}


func TestCheckNoDrift(t *testing.T) {
	dir := t.TempDir()

	srcContent := "# Hello command\n"
	if err := os.WriteFile(filepath.Join(dir, "hello.md"), []byte(srcContent), 0o644); err != nil {
		t.Fatal(err)
	}
	yaml := "version: 1\ncommands:\n  hello:\n    source: hello.md\n"
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	if code := run([]string{"--chdir", dir, "lock"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("lock failed")
	}
	if code := run([]string{"--chdir", dir, "apply", "--yes"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("apply failed")
	}

	var out, errOut bytes.Buffer
	code := run([]string{"--chdir", dir, "check"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("check after apply: code=%d out=%q err=%q", code, out.String(), errOut.String())
	}
	combined := out.String() + errOut.String()
	if !strings.Contains(combined, "No drift") {
		t.Errorf("check no drift: expected 'No drift', got: %q", combined)
	}
}

func TestCheckDriftExitsNonZero(t *testing.T) {
	dir := t.TempDir()

	srcContent := "# Hello command\n"
	if err := os.WriteFile(filepath.Join(dir, "hello.md"), []byte(srcContent), 0o644); err != nil {
		t.Fatal(err)
	}
	yaml := "version: 1\ncommands:\n  hello:\n    source: hello.md\n"
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	if code := run([]string{"--chdir", dir, "lock"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("lock failed")
	}
	if code := run([]string{"--chdir", dir, "apply", "--yes"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("apply failed")
	}

	// Delete the applied artifact to introduce drift.
	dest := filepath.Join(dir, ".claude", "commands", "hello.md")
	if err := os.Remove(dest); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	code := run([]string{"--chdir", dir, "check"}, &out, &errOut)
	if code == 0 {
		t.Fatal("check with drift: expected non-zero exit, got 0")
	}
	combined := out.String() + errOut.String()
	if !strings.Contains(combined, "hello") {
		t.Errorf("check with drift: expected 'hello' in output, got: %q", combined)
	}
}

func TestCheckNoLockFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte("version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	code := run([]string{"--chdir", dir, "check"}, &out, &errOut)
	if code == 0 {
		t.Fatal("check without lock: expected non-zero exit, got 0")
	}
	combined := out.String() + errOut.String()
	if !strings.Contains(combined, "ainfra lock") {
		t.Errorf("check without lock: expected 'ainfra lock' hint, got: %q", combined)
	}
}
