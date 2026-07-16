package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A secret value can rotate in its backend (1Password, env) without any
// manifest drift. A no-op install must still re-materialize secrets, or the
// rotated value never reaches the machine.
func TestInstallNoDriftStillRefreshesSecrets(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("TEAM_ENV_BLOB", "EXCALIDRAW_API_KEY=old-key\n")

	if err := os.WriteFile(filepath.Join(dir, "hello.md"), []byte("# Hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	yaml := `version: 1
secrets:
  team-env:
    mode: direct
    scope: shared
    ref: "env://TEAM_ENV_BLOB"
    envFile: true
commands:
  hello:
    source: hello.md
`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"--chdir", dir, "lock"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("lock failed")
	}
	if code := run([]string{"--chdir", dir, "install", "--yes"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("first apply failed")
	}
	if got := settingsEnvValue(t, home, "EXCALIDRAW_API_KEY"); got != "old-key" {
		t.Fatalf("after first install: EXCALIDRAW_API_KEY = %q, want old-key", got)
	}

	// Rotate the secret in the backend; nothing else changes.
	t.Setenv("TEAM_ENV_BLOB", "EXCALIDRAW_API_KEY=new-key\n")

	var out, errOut bytes.Buffer
	code := run([]string{"--chdir", dir, "install", "--yes"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("second apply: code=%d out=%q err=%q", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "Nothing to do") {
		t.Errorf("second apply: expected 'Nothing to do', got: %q", out.String())
	}
	if got := settingsEnvValue(t, home, "EXCALIDRAW_API_KEY"); got != "new-key" {
		t.Errorf("no-drift install did not refresh rotated secret: EXCALIDRAW_API_KEY = %q, want new-key", got)
	}
}

// A dry run must never resolve or write secrets — it stays usable in CI
// where no vault is signed in.
func TestInstallNoDriftDryRunDoesNotTouchSecrets(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("TEAM_ENV_BLOB", "EXCALIDRAW_API_KEY=old-key\n")

	if err := os.WriteFile(filepath.Join(dir, "hello.md"), []byte("# Hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	yaml := `version: 1
secrets:
  team-env:
    mode: direct
    scope: shared
    ref: "env://TEAM_ENV_BLOB"
    envFile: true
commands:
  hello:
    source: hello.md
`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"--chdir", dir, "lock"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("lock failed")
	}
	if code := run([]string{"--chdir", dir, "install", "--yes"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("first apply failed")
	}

	t.Setenv("TEAM_ENV_BLOB", "EXCALIDRAW_API_KEY=new-key\n")

	var out, errOut bytes.Buffer
	if code := run([]string{"--chdir", dir, "install", "--dry-run"}, &out, &errOut); code != 0 {
		t.Fatalf("dry run: code=%d out=%q err=%q", code, out.String(), errOut.String())
	}
	if got := settingsEnvValue(t, home, "EXCALIDRAW_API_KEY"); got != "old-key" {
		t.Errorf("dry run refreshed secrets: EXCALIDRAW_API_KEY = %q, want old-key untouched", got)
	}
}

// Claude Code expands ${VAR} in HTTP MCP server headers from the real process
// environment only — the settings env block is invisible to it. Secrets must
// therefore also land in a shell-sourced env file.
func TestInstallWritesShellEnvFileAndWiresZshenv(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("TEAM_ENV_BLOB", "EXCALIDRAW_API_KEY=sk-abc\nOTHER_TOKEN=it's quoted\n")

	if err := os.WriteFile(filepath.Join(dir, "hello.md"), []byte("# Hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	yaml := `version: 1
secrets:
  team-env:
    mode: direct
    scope: shared
    ref: "env://TEAM_ENV_BLOB"
    envFile: true
commands:
  hello:
    source: hello.md
`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"--chdir", dir, "lock"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("lock failed")
	}
	if code := run([]string{"--chdir", dir, "install", "--yes"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("install failed")
	}

	shellPath := filepath.Join(home, ".config", "ainfra", "env.sh")
	raw, err := os.ReadFile(shellPath)
	if err != nil {
		t.Fatalf("shell env file not written: %v", err)
	}
	content := string(raw)
	if !strings.Contains(content, "export EXCALIDRAW_API_KEY='sk-abc'") {
		t.Errorf("env.sh missing export, got:\n%s", content)
	}
	// A value with a single quote must be escaped so the file still parses.
	if !strings.Contains(content, `export OTHER_TOKEN='it'\''s quoted'`) {
		t.Errorf("env.sh single-quote escaping wrong, got:\n%s", content)
	}
	info, err := os.Stat(shellPath)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("env.sh mode = %o, want 600 (it holds credentials)", perm)
	}

	zshenv, err := os.ReadFile(filepath.Join(home, ".zshenv"))
	if err != nil {
		t.Fatalf(".zshenv not written: %v", err)
	}
	if !strings.Contains(string(zshenv), ".config/ainfra/env.sh") {
		t.Errorf(".zshenv does not source env.sh, got:\n%s", zshenv)
	}

	// Second install must not duplicate the source line.
	if code := run([]string{"--chdir", dir, "install", "--yes"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("second install failed")
	}
	zshenv2, _ := os.ReadFile(filepath.Join(home, ".zshenv"))
	if got := strings.Count(string(zshenv2), "Added by ainfra"); got != 1 {
		t.Errorf(".zshenv ainfra block count = %d, want 1 (idempotent)", got)
	}
}

// A manifest with no secrets must not create the shell env file or touch
// the user's ~/.zshenv.
func TestInstallNoSecretsDoesNotTouchZshenv(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)

	if err := os.WriteFile(filepath.Join(dir, "hello.md"), []byte("# Hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	yaml := "version: 1\ncommands:\n  hello:\n    source: hello.md\n"
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"--chdir", dir, "lock"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("lock failed")
	}
	if code := run([]string{"--chdir", dir, "install", "--yes"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("install failed")
	}

	if _, err := os.Stat(filepath.Join(home, ".config", "ainfra", "env.sh")); !os.IsNotExist(err) {
		t.Errorf("env.sh written for a secretless manifest (stat err = %v)", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".zshenv")); !os.IsNotExist(err) {
		t.Errorf(".zshenv touched for a secretless manifest (stat err = %v)", err)
	}
}

func settingsEnvValue(t *testing.T, home, key string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(home, ".claude", "settings.local.json"))
	if err != nil {
		t.Fatalf("read settings.local.json: %v", err)
	}
	var doc struct {
		Env map[string]string `json:"env"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse settings.local.json: %v", err)
	}
	return doc.Env[key]
}
