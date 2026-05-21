package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
