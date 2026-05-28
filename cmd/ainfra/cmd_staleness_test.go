package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStalenessHook_AutoEmitted: a clean repo install writes the SessionStart
// hook into .claude/settings.json without it being declared in ainfra.yaml.
func TestStalenessHook_AutoEmitted(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte("version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"--chdir", dir, "lock"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("lock failed")
	}
	if code := run([]string{"--chdir", dir, "install", "--yes"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("install failed")
	}

	raw, err := os.ReadFile(filepath.Join(dir, ".claude", "settings.json"))
	if err != nil {
		t.Fatalf("settings.json: %v", err)
	}
	var settings map[string]any
	if err := json.Unmarshal(raw, &settings); err != nil {
		t.Fatalf("parsing settings.json: %v", err)
	}
	hooks, _ := settings["hooks"].(map[string]any)
	if _, ok := hooks["SessionStart"]; !ok {
		t.Fatalf("expected SessionStart hook in settings.json, got: %s", raw)
	}
	if !strings.Contains(string(raw), "ainfra _staleness-check") {
		t.Errorf("expected staleness command in settings.json, got: %s", raw)
	}
}

// TestStalenessHook_OptOut: stalenessWarning: false suppresses the hook.
func TestStalenessHook_OptOut(t *testing.T) {
	dir := t.TempDir()
	yaml := "version: 1\nstalenessWarning: false\n"
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"--chdir", dir, "lock"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("lock failed")
	}
	var out bytes.Buffer
	if code := run([]string{"--chdir", dir, "install", "--yes"}, &out, &bytes.Buffer{}); code != 0 {
		t.Fatalf("install failed: %s", out.String())
	}

	if _, err := os.Stat(filepath.Join(dir, ".claude", "settings.json")); err == nil {
		raw, _ := os.ReadFile(filepath.Join(dir, ".claude", "settings.json"))
		if strings.Contains(string(raw), "_staleness-check") {
			t.Errorf("opted-out repo still got the staleness hook: %s", raw)
		}
	}
}

// TestStalenessHook_HiddenFromList: `ainfra list` never shows the synthetic
// hook entry.
func TestStalenessHook_HiddenFromList(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte("version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"--chdir", dir, "lock"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("lock failed")
	}
	if code := run([]string{"--chdir", dir, "install", "--yes"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("install failed")
	}
	var out bytes.Buffer
	if code := run([]string{"--chdir", dir, "list"}, &out, &bytes.Buffer{}); code != 0 {
		t.Fatalf("list failed: %s", out.String())
	}
	if strings.Contains(out.String(), "__ainfra_staleness") {
		t.Errorf("list should hide synthetic staleness hook, got: %s", out.String())
	}
}

// TestStalenessCheck_Clean: the hook command stays silent immediately after
// an install (manifest hash matches the applied ledger).
func TestStalenessCheck_Clean(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte("version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"--chdir", dir, "lock"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("lock failed")
	}
	if code := run([]string{"--chdir", dir, "install", "--yes"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("install failed")
	}
	var out, errOut bytes.Buffer
	code := run([]string{"--chdir", dir, "_staleness-check"}, &out, &errOut)
	if code != 0 {
		t.Errorf("_staleness-check clean: code=%d err=%q", code, errOut.String())
	}
	if errOut.Len() != 0 {
		t.Errorf("clean repo should be silent, got: %q", errOut.String())
	}
}

// TestStalenessCheck_Stale: editing the manifest after install causes the
// hook to warn on stderr but still exit 0.
func TestStalenessCheck_Stale(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte("version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"--chdir", dir, "lock"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("lock failed")
	}
	if code := run([]string{"--chdir", dir, "install", "--yes"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("install failed")
	}

	// Mutate the manifest so its hash drifts from the applied ledger. The
	// hash is computed from parsed YAML, so adding a comment alone wouldn't
	// trip drift — change a field.
	mutated := "version: 1\nvars:\n  drift: \"v2\"\n"
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(mutated), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	code := run([]string{"--chdir", dir, "_staleness-check"}, &out, &errOut)
	if code != 0 {
		t.Errorf("_staleness-check should always exit 0, got %d", code)
	}
	if !strings.Contains(errOut.String(), "ainfra install") {
		t.Errorf("expected stale warning naming install, got: %q", errOut.String())
	}
}

// TestStalenessCheck_NoManifest: in a directory without ainfra.yaml the hook
// is a silent no-op.
func TestStalenessCheck_NoManifest(t *testing.T) {
	dir := t.TempDir()
	var out, errOut bytes.Buffer
	code := run([]string{"--chdir", dir, "_staleness-check"}, &out, &errOut)
	if code != 0 || out.Len() != 0 || errOut.Len() != 0 {
		t.Errorf("no-manifest case: code=%d out=%q err=%q", code, out.String(), errOut.String())
	}
}
