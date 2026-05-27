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
	code := run([]string{"--chdir", dir, "add", "mcp", "newone", "--no-install"}, &bytes.Buffer{}, &bytes.Buffer{})
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
	if code := run([]string{"--chdir", dir, "add", "mcp", "newone", "--no-install"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("first add failed")
	}
	var errOut bytes.Buffer
	code := run([]string{"--chdir", dir, "add", "mcp", "newone", "--no-install"}, &bytes.Buffer{}, &errOut)
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
	code := run([]string{"--chdir", dir, "add", "--personal", "mcp", "local-fs", "--no-install"}, &bytes.Buffer{}, &bytes.Buffer{})
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
