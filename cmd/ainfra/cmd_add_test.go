package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAdd_NoManifest(t *testing.T) {
	dir := t.TempDir()
	var errOut bytes.Buffer
	code := run([]string{"--chdir", dir, "add", "mcp", "github"}, &bytes.Buffer{}, &errOut)
	if code != 1 {
		t.Fatalf("add with no ainfra.yaml: want code=1, got %d", code)
	}
	if !strings.Contains(errOut.String(), "ainfra init") {
		t.Errorf("add no manifest: want hint about ainfra init, got %q", errOut.String())
	}
}

func TestAdd_UnknownChannel(t *testing.T) {
	dir := newDemoRepo(t)
	var errOut bytes.Buffer
	code := run([]string{"--chdir", dir, "add", "mcps", "github"}, &bytes.Buffer{}, &errOut)
	if code != 1 {
		t.Fatalf("unknown channel: want code=1, got %d", code)
	}
	if !strings.Contains(errOut.String(), "unknown channel") {
		t.Errorf("unknown channel: want error mentioning unknown channel, got %q", errOut.String())
	}
}

func TestAdd_AppendsAndLocks(t *testing.T) {
	dir := newDemoRepo(t)
	code := run([]string{"--chdir", dir, "add", "--no-install", "mcp", "newone"}, &bytes.Buffer{}, &bytes.Buffer{})
	if code != 0 {
		t.Fatalf("add --no-install: want code=0, got %d", code)
	}
	yaml, _ := os.ReadFile(filepath.Join(dir, "ainfra.yaml"))
	if !strings.Contains(string(yaml), "newone:") {
		t.Errorf("add did not write newone: %s", yaml)
	}
	if _, err := os.Stat(filepath.Join(dir, "ainfra.lock")); err != nil {
		t.Errorf("add did not write ainfra.lock: %v", err)
	}
}

func TestAdd_IdempotentErrors(t *testing.T) {
	dir := newDemoRepo(t)
	if code := run([]string{"--chdir", dir, "add", "--no-install", "mcp", "newone"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("first add failed")
	}
	var errOut bytes.Buffer
	code := run([]string{"--chdir", dir, "add", "--no-install", "mcp", "newone"}, &bytes.Buffer{}, &errOut)
	if code != 1 {
		t.Errorf("second add: want code=1, got %d", code)
	}
	if !strings.Contains(errOut.String(), "already exists") {
		t.Errorf("second add: want 'already exists', got %q", errOut.String())
	}
}

func TestAdd_PersonalTargetsPersonalFile(t *testing.T) {
	dir := newDemoRepo(t)
	personalYAML := `version: 1
mcpServers: {}
`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.personal.yaml"), []byte(personalYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	code := run([]string{"--chdir", dir, "add", "--personal", "--no-install", "mcp", "local-fs"}, &bytes.Buffer{}, &bytes.Buffer{})
	if code != 0 {
		t.Fatalf("add --personal: want code=0, got %d", code)
	}
	committed, _ := os.ReadFile(filepath.Join(dir, "ainfra.yaml"))
	if strings.Contains(string(committed), "local-fs") {
		t.Errorf("--personal entry leaked into ainfra.yaml: %s", committed)
	}
	personal, _ := os.ReadFile(filepath.Join(dir, "ainfra.personal.yaml"))
	if !strings.Contains(string(personal), "local-fs") {
		t.Errorf("--personal entry missing from ainfra.personal.yaml: %s", personal)
	}
}

// TestAddRejectsFlagAfterPositionals is the regression test for a silent
// wrong-file write.
//
// `ainfra add command ship <src> --global` used to drop --global (Go stops
// parsing flags at the first positional), write the entry into the team's
// committed ainfra.yaml, and exit 0 — leaving the user believing they had
// declared a personal command globally. `install --prune` in another repo
// would then remove it. It must fail loudly instead.
func TestAddRejectsFlagAfterPositionals(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte("version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "ship.md")
	if err := os.WriteFile(src, []byte("# ship\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var errOut bytes.Buffer
	code := run([]string{"--chdir", dir, "add", "command", "ship", src, "--global"}, &bytes.Buffer{}, &errOut)
	if code == 0 {
		t.Error("exited 0; a dropped --global must not silently write ainfra.yaml")
	}
	if !strings.Contains(errOut.String(), "--global") {
		t.Errorf("error should name the ignored flag, got %q", errOut.String())
	}

	raw, err := os.ReadFile(filepath.Join(dir, "ainfra.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "ship") {
		t.Errorf("the entry was written to the team manifest anyway:\n%s", raw)
	}
}
