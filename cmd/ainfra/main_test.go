package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunVersion(t *testing.T) {
	var out bytes.Buffer
	code := run([]string{"version"}, &out, &bytes.Buffer{})
	if code != 0 || !strings.Contains(out.String(), "ainfra ") {
		t.Errorf("version: code=%d out=%q", code, out.String())
	}
}

func TestRunNoArgsShowsOverview(t *testing.T) {
	var out bytes.Buffer
	code := run(nil, &out, &bytes.Buffer{})
	if code != 0 || !strings.Contains(out.String(), "Commands:") {
		t.Errorf("overview: code=%d out=%q", code, out.String())
	}
}

func TestRunUnknownCommand(t *testing.T) {
	var errOut bytes.Buffer
	code := run([]string{"bogus"}, &bytes.Buffer{}, &errOut)
	if code != 2 || !strings.Contains(errOut.String(), "unknown command") {
		t.Errorf("unknown: code=%d err=%q", code, errOut.String())
	}
}

func TestRunLockOnMinimalManifest(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte("version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	code := run([]string{"--chdir", dir, "lock"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("lock: code=%d err=%q", code, errOut.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "ainfra.lock")); err != nil {
		t.Errorf("ainfra.lock not written: %v", err)
	}
	if !strings.Contains(out.String(), "Next:") {
		t.Errorf("lock output missing Next hint: %q", out.String())
	}
}
