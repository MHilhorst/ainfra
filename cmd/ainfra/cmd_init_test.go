package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitWritesManifestAndGitignore(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	code := run([]string{"--chdir", dir, "init"}, &out, &bytes.Buffer{})
	if code != 0 {
		t.Fatalf("init: code=%d", code)
	}
	data, err := os.ReadFile(filepath.Join(dir, "ainfra.yaml"))
	if err != nil {
		t.Fatalf("ainfra.yaml not written: %v", err)
	}
	if !strings.Contains(string(data), "version: 1") {
		t.Errorf("manifest missing version: %q", data)
	}
	gi, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil || !strings.Contains(string(gi), "ainfra.personal.*") {
		t.Errorf(".gitignore missing personal entry: %v / %q", err, gi)
	}
}

func TestInitRefusesToOverwriteWithoutForce(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte("version: 1\n"), 0o644)
	var errOut bytes.Buffer
	code := run([]string{"--chdir", dir, "init"}, &bytes.Buffer{}, &errOut)
	if code != 1 || !strings.Contains(errOut.String(), "already exists") {
		t.Errorf("expected refusal: code=%d err=%q", code, errOut.String())
	}
}

func TestInitForceOverwrites(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte("OLD\n"), 0o644)
	code := run([]string{"--chdir", dir, "init", "--force"}, &bytes.Buffer{}, &bytes.Buffer{})
	if code != 0 {
		t.Fatalf("init --force: code=%d", code)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "ainfra.yaml"))
	if strings.Contains(string(data), "OLD") {
		t.Error("init --force did not overwrite")
	}
}

func TestInitPersonalWritesPersonalLayer(t *testing.T) {
	dir := t.TempDir()
	code := run([]string{"--chdir", dir, "init", "--personal"}, &bytes.Buffer{}, &bytes.Buffer{})
	if code != 0 {
		t.Fatalf("init --personal: code=%d", code)
	}
	if _, err := os.Stat(filepath.Join(dir, "ainfra.personal.yaml")); err != nil {
		t.Errorf("ainfra.personal.yaml not written: %v", err)
	}
}

func TestInitGitignoreIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("ainfra.personal.*\n"), 0o644)
	run([]string{"--chdir", dir, "init"}, &bytes.Buffer{}, &bytes.Buffer{})
	gi, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if strings.Count(string(gi), "ainfra.personal.*") != 1 {
		t.Errorf(".gitignore entry duplicated: %q", gi)
	}
}

func TestInitScaffoldsAgentField(t *testing.T) {
	dir := t.TempDir()
	code := run([]string{"--chdir", dir, "init"}, &bytes.Buffer{}, &bytes.Buffer{})
	if code != 0 {
		t.Fatalf("runInit exit code = %d, want 0", code)
	}
	data, err := os.ReadFile(filepath.Join(dir, "ainfra.yaml"))
	if err != nil {
		t.Fatalf("reading scaffolded manifest: %v", err)
	}
	if !strings.Contains(string(data), "agent: claude-code") {
		t.Errorf("scaffolded manifest does not declare  agent: claude-code\n%s", data)
	}
}
