package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// captureExec swaps execFn for a recorder and returns a pointer to the
// captured call. The recorder never replaces the process.
type execCall struct {
	bin  string
	argv []string
	env  []string
}

func captureExec(t *testing.T) *execCall {
	t.Helper()
	call := &execCall{}
	orig := execFn
	execFn = func(bin string, argv []string, env []string) error {
		call.bin = bin
		call.argv = argv
		call.env = env
		return nil
	}
	t.Cleanup(func() { execFn = orig })
	return call
}

func envValue(env []string, key string) (string, bool) {
	for _, kv := range env {
		if strings.HasPrefix(kv, key+"=") {
			return kv[len(key)+1:], true
		}
	}
	return "", false
}

// exec must resolve the manifest's secrets into the child's environment, and
// a freshly-resolved value must win over a stale exported one — that is what
// makes rotation reach the next launch without re-running install.
func TestExecInjectsResolvedSecretsOverStaleEnv(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("TEAM_ENV_BLOB", "EXCALIDRAW_API_KEY=fresh-key\n")
	t.Setenv("EXCALIDRAW_API_KEY", "stale-key")

	writeSecretFixture(t, dir)
	if code := run([]string{"--chdir", dir, "lock"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("lock failed")
	}

	call := captureExec(t)
	var out, errOut bytes.Buffer
	if code := run([]string{"--chdir", dir, "exec", "--", "sh", "-c", "true"}, &out, &errOut); code != 0 {
		t.Fatalf("exec: code=%d out=%q err=%q", code, out.String(), errOut.String())
	}
	if got, ok := envValue(call.env, "EXCALIDRAW_API_KEY"); !ok || got != "fresh-key" {
		t.Errorf("child EXCALIDRAW_API_KEY = %q (present=%v), want fresh-key", got, ok)
	}
	if len(call.argv) != 3 || call.argv[0] != "sh" || call.argv[2] != "true" {
		t.Errorf("child argv = %v, want [sh -c true]", call.argv)
	}
	if call.bin == "" || !strings.HasSuffix(call.bin, "/sh") {
		t.Errorf("resolved binary = %q, want a path to sh", call.bin)
	}
}

// A missing lockfile (deleted repo, wrong --chdir) must not block the launch:
// exec warns and runs the command without injection.
func TestExecFailsOpenWithoutLockfile(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)

	call := captureExec(t)
	var out, errOut bytes.Buffer
	if code := run([]string{"--chdir", dir, "exec", "--", "sh", "-c", "true"}, &out, &errOut); code != 0 {
		t.Fatalf("exec: code=%d err=%q", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "without secret injection") {
		t.Errorf("expected fail-open warning on stderr, got: %q", errOut.String())
	}
	if call.bin == "" {
		t.Error("command was not executed")
	}
}

// An unresolvable secret must degrade to a warning, not block the launch.
func TestExecFailsOpenOnUnresolvableSecret(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("TEAM_ENV_BLOB", "X=1\n")

	writeSecretFixture(t, dir)
	if code := run([]string{"--chdir", dir, "lock"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("lock failed")
	}
	// The blob's backing env var disappears after lock (e.g. vault locked).
	os.Unsetenv("TEAM_ENV_BLOB")

	call := captureExec(t)
	var out, errOut bytes.Buffer
	if code := run([]string{"--chdir", dir, "exec", "--", "sh", "-c", "true"}, &out, &errOut); code != 0 {
		t.Fatalf("exec: code=%d err=%q", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "launching without it") {
		t.Errorf("expected per-secret warning, got: %q", errOut.String())
	}
	if call.bin == "" {
		t.Error("command was not executed")
	}
}

func TestExecNoCommandIsUsageError(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"exec"}, &out, &errOut); code != 2 {
		t.Fatalf("exec with no command: code=%d, want 2", code)
	}
}

// The claude shim re-enters `ainfra exec -- claude`, so PATH resolution must
// skip the shim dir or the shim would exec itself forever.
func TestLookPathSkipsShimDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	shimDir := filepath.Join(home, ".config", "ainfra", "bin")
	realDir := filepath.Join(home, "realbin")
	for _, d := range []string{shimDir, realDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []string{filepath.Join(shimDir, "toolx"), filepath.Join(realDir, "toolx")} {
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", shimDir+string(os.PathListSeparator)+realDir)

	got, err := lookPathSkippingShims("toolx")
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(realDir, "toolx") {
		t.Errorf("resolved %q, want the real binary %q", got, filepath.Join(realDir, "toolx"))
	}
}

func TestMergeEnvResolvedWins(t *testing.T) {
	base := []string{"A=1", "B=2", "WEIRD"}
	out := mergeEnv(base, map[string]string{"B": "fresh", "C": "3"})
	if got, _ := envValue(out, "B"); got != "fresh" {
		t.Errorf("B = %q, want fresh", got)
	}
	if got, _ := envValue(out, "A"); got != "1" {
		t.Errorf("A = %q, want 1", got)
	}
	if got, _ := envValue(out, "C"); got != "3" {
		t.Errorf("C = %q, want 3", got)
	}
	found := false
	for _, kv := range out {
		if kv == "WEIRD" {
			found = true
		}
	}
	if !found {
		t.Error("entry without '=' was dropped")
	}
}
