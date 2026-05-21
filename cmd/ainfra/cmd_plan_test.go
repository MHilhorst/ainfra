package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlanNoLockFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte("version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	code := run([]string{"--chdir", dir, "plan"}, &out, &errOut)
	if code == 0 {
		t.Fatalf("plan without lock: expected non-zero exit, got 0")
	}
	combined := out.String() + errOut.String()
	if !strings.Contains(combined, "ainfra lock") {
		t.Errorf("plan without lock: expected 'ainfra lock' hint, got: %q", combined)
	}
}

func TestPlanWithLockFile(t *testing.T) {
	dir := t.TempDir()
	yaml := "version: 1\nhooks:\n  on-start:\n    event: SessionStart\n    command: echo hello\n"
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	// Run lock first so ainfra.lock exists.
	var lockOut, lockErr bytes.Buffer
	if code := run([]string{"--chdir", dir, "lock"}, &lockOut, &lockErr); code != 0 {
		t.Fatalf("lock: code=%d err=%q", code, lockErr.String())
	}
	var out, errOut bytes.Buffer
	code := run([]string{"--chdir", dir, "plan"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("plan: code=%d out=%q err=%q", code, out.String(), errOut.String())
	}
	combined := out.String() + errOut.String()
	// Should show either changes or the no-changes message.
	if !strings.Contains(combined, "to add") && !strings.Contains(combined, "No changes") {
		t.Errorf("plan: expected plan output, got: %q", combined)
	}
}
