package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeSecretFixture writes the minimal manifest used by the refresh tests:
// one envFile secret resolved from the TEAM_ENV_BLOB env var plus one command.
func writeSecretFixture(t *testing.T, dir string) {
	t.Helper()
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
}

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

	writeSecretFixture(t, dir)
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

	writeSecretFixture(t, dir)
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
// environment only. Secrets reach it through the launcher shim, which re-execs
// claude via `ainfra exec` — installing must write the shim and put its dir on
// PATH, and must never write credential values into shell config.
func TestInstallWritesLauncherShimAndPathLine(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("TEAM_ENV_BLOB", "EXCALIDRAW_API_KEY=sk-abc\n")

	writeSecretFixture(t, dir)
	if code := run([]string{"--chdir", dir, "lock"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("lock failed")
	}
	if code := run([]string{"--chdir", dir, "install", "--yes"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("install failed")
	}

	shimPath := filepath.Join(home, ".config", "ainfra", "bin", "claude")
	raw, err := os.ReadFile(shimPath)
	if err != nil {
		t.Fatalf("launcher shim not written: %v", err)
	}
	shim := string(raw)
	if !strings.Contains(shim, "--chdir") || !strings.Contains(shim, `exec -- claude "$@"`) {
		t.Errorf("shim does not re-exec through ainfra exec, got:\n%s", shim)
	}
	// The ainfra binary must be referenced absolutely — GUI-spawned processes
	// have a minimal PATH where a bare `ainfra` is not found.
	if strings.Contains(shim, "\nexec ainfra ") {
		t.Errorf("shim references ainfra by bare name, want an absolute path:\n%s", shim)
	}
	if !strings.Contains(shim, dir) {
		t.Errorf("shim does not bake in the manifest dir %q, got:\n%s", dir, shim)
	}
	if strings.Contains(shim, "sk-abc") {
		t.Errorf("shim contains a credential value:\n%s", shim)
	}
	info, err := os.Stat(shimPath)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm&0o111 == 0 {
		t.Errorf("shim mode = %o, want executable", perm)
	}

	zshenv, err := os.ReadFile(filepath.Join(home, ".zshenv"))
	if err != nil {
		t.Fatalf(".zshenv not written: %v", err)
	}
	if !strings.Contains(string(zshenv), ".config/ainfra/bin") {
		t.Errorf(".zshenv does not put the shim dir on PATH, got:\n%s", zshenv)
	}
	if strings.Contains(string(zshenv), "sk-abc") {
		t.Errorf(".zshenv contains a credential value:\n%s", zshenv)
	}

	// Second install must not duplicate the PATH line.
	if code := run([]string{"--chdir", dir, "install", "--yes"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("second install failed")
	}
	zshenv2, _ := os.ReadFile(filepath.Join(home, ".zshenv"))
	if got := strings.Count(string(zshenv2), "Added by ainfra"); got != 1 {
		t.Errorf(".zshenv ainfra block count = %d, want 1 (idempotent)", got)
	}
}

// Machines upgraded from ainfra <= 0.2.11 carry the legacy delivery: an
// env.sh of credential exports sourced from ~/.zshenv. Install must remove
// both while preserving unrelated ~/.zshenv content.
func TestInstallCleansUpLegacyShellEnv(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("TEAM_ENV_BLOB", "EXCALIDRAW_API_KEY=sk-abc\n")

	// Seed the legacy state exactly as ainfra 0.2.11 wrote it.
	envSh := filepath.Join(home, ".config", "ainfra", "env.sh")
	if err := os.MkdirAll(filepath.Dir(envSh), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(envSh, []byte("export EXCALIDRAW_API_KEY='sk-abc'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	legacyZshenv := "export EDITOR=vim\n" +
		"\n# Added by ainfra — exports managed secrets into the shell environment.\n" +
		"[ -f \"$HOME/.config/ainfra/env.sh\" ] && . \"$HOME/.config/ainfra/env.sh\"\n"
	if err := os.WriteFile(filepath.Join(home, ".zshenv"), []byte(legacyZshenv), 0o644); err != nil {
		t.Fatal(err)
	}

	writeSecretFixture(t, dir)
	if code := run([]string{"--chdir", dir, "lock"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("lock failed")
	}
	if code := run([]string{"--chdir", dir, "install", "--yes"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("install failed")
	}

	if _, err := os.Stat(envSh); !os.IsNotExist(err) {
		t.Errorf("legacy env.sh still present (stat err = %v)", err)
	}
	zshenv, err := os.ReadFile(filepath.Join(home, ".zshenv"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(zshenv)
	if strings.Contains(content, "env.sh") {
		t.Errorf(".zshenv still sources legacy env.sh:\n%s", content)
	}
	if strings.Contains(content, "exports managed secrets") {
		t.Errorf(".zshenv still carries the legacy comment:\n%s", content)
	}
	if !strings.Contains(content, "export EDITOR=vim") {
		t.Errorf(".zshenv lost unrelated user content:\n%s", content)
	}
	if !strings.Contains(content, ".config/ainfra/bin") {
		t.Errorf(".zshenv missing the shim PATH line after cleanup:\n%s", content)
	}
}

// A manifest with no secrets must not create the shim or touch the user's
// ~/.zshenv.
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

	if _, err := os.Stat(filepath.Join(home, ".config", "ainfra", "bin", "claude")); !os.IsNotExist(err) {
		t.Errorf("shim written for a secretless manifest (stat err = %v)", err)
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
