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

func TestInitWithSkillIncludesUsingAinfra(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	code := run([]string{"--chdir", dir, "init", "--with-skill"}, &out, &bytes.Buffer{})
	if code != 0 {
		t.Fatalf("init --with-skill: code=%d", code)
	}
	data, err := os.ReadFile(filepath.Join(dir, "ainfra.yaml"))
	if err != nil {
		t.Fatalf("ainfra.yaml not written: %v", err)
	}
	for _, want := range []string{"skills:", "using-ainfra:", "github:MHilhorst/ainfra/skills/using-ainfra"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("manifest missing %q\n---\n%s", want, data)
		}
	}
	if !strings.Contains(out.String(), "using-ainfra skill") {
		t.Errorf("expected stdout to mention the skill, got %q", out.String())
	}
}

func TestInitWithoutFlagOmitsSkill(t *testing.T) {
	dir := t.TempDir()
	run([]string{"--chdir", dir, "init"}, &bytes.Buffer{}, &bytes.Buffer{})
	data, _ := os.ReadFile(filepath.Join(dir, "ainfra.yaml"))
	if strings.Contains(string(data), "using-ainfra") {
		t.Errorf("bare init should not include the skill, got:\n%s", data)
	}
}

func TestInitPersonalIgnoresWithSkill(t *testing.T) {
	dir := t.TempDir()
	run([]string{"--chdir", dir, "init", "--personal", "--with-skill"}, &bytes.Buffer{}, &bytes.Buffer{})
	data, _ := os.ReadFile(filepath.Join(dir, "ainfra.personal.yaml"))
	if strings.Contains(string(data), "using-ainfra") {
		t.Errorf("personal scaffold should never include the skill, got:\n%s", data)
	}
}

func TestInitTeamScansHomeByDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	claude := filepath.Join(home, ".claude")
	os.MkdirAll(filepath.Join(claude, "commands"), 0o755)
	os.WriteFile(filepath.Join(claude, "commands", "note.md"), []byte("# note"), 0o644)
	os.WriteFile(filepath.Join(claude, "CLAUDE.md"), []byte("rules"), 0o644)

	parent := t.TempDir()
	var out, errOut bytes.Buffer
	code := run([]string{"--chdir", parent, "init", "team", "config"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("init team: code=%d err=%q", code, errOut.String())
	}
	target := filepath.Join(parent, "config")
	data, err := os.ReadFile(filepath.Join(target, "ainfra.yaml"))
	if err != nil {
		t.Fatalf("ainfra.yaml not written: %v", err)
	}
	s := string(data)
	for _, fragment := range []string{"version: 1", "commands:", "note:", "rules:", "CLAUDE.md"} {
		if !strings.Contains(s, fragment) {
			t.Errorf("missing %q in:\n%s", fragment, s)
		}
	}
	if _, err := os.Stat(filepath.Join(target, "README.md")); err != nil {
		t.Errorf("README.md not written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, ".git")); err != nil {
		t.Errorf("git init did not run: %v", err)
	}
}

func TestInitTeamEmptyWritesSkeleton(t *testing.T) {
	parent := t.TempDir()
	var out bytes.Buffer
	code := run([]string{"--chdir", parent, "init", "team", "config", "--empty"}, &out, &bytes.Buffer{})
	if code != 0 {
		t.Fatalf("init team --empty: code=%d", code)
	}
	data, err := os.ReadFile(filepath.Join(parent, "config", "ainfra.yaml"))
	if err != nil {
		t.Fatalf("ainfra.yaml not written: %v", err)
	}
	for _, want := range []string{"version: 1", "agent: claude-code", "Team ainfra manifest"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("manifest missing %q\n---\n%s", want, data)
		}
	}
}

func TestInitTeamRefusesNonEmptyDir(t *testing.T) {
	parent := t.TempDir()
	target := filepath.Join(parent, "claude-config")
	os.MkdirAll(target, 0o755)
	os.WriteFile(filepath.Join(target, "existing.txt"), []byte("x"), 0o644)

	var errOut bytes.Buffer
	code := run([]string{"--chdir", parent, "init", "team", "claude-config", "--empty"}, &bytes.Buffer{}, &errOut)
	if code != 1 || !strings.Contains(errOut.String(), "not empty") {
		t.Errorf("expected refusal: code=%d err=%q", code, errOut.String())
	}
}

func TestInitAdoptForwardsToAdopt(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".claude", "commands"), 0o755)
	os.WriteFile(filepath.Join(dir, ".claude", "commands", "deploy.md"), []byte("# deploy"), 0o644)

	code := run([]string{"--chdir", dir, "init", "--adopt"}, &bytes.Buffer{}, &bytes.Buffer{})
	if code != 0 {
		t.Fatalf("init --adopt: code=%d", code)
	}
	data, err := os.ReadFile(filepath.Join(dir, "ainfra.yaml"))
	if err != nil {
		t.Fatalf("ainfra.yaml not written: %v", err)
	}
	for _, want := range []string{"commands:", "deploy:"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("missing %q in:\n%s", want, data)
		}
	}
}

func TestInitAdoptAndPersonalAreExclusive(t *testing.T) {
	var errOut bytes.Buffer
	code := run([]string{"--chdir", t.TempDir(), "init", "--adopt", "--personal"}, &bytes.Buffer{}, &errOut)
	if code != 1 || !strings.Contains(errOut.String(), "mutually exclusive") {
		t.Errorf("expected exclusivity error: code=%d err=%q", code, errOut.String())
	}
}

func TestInitTeamMissingPath(t *testing.T) {
	var errOut bytes.Buffer
	code := run([]string{"--chdir", t.TempDir(), "init", "team"}, &bytes.Buffer{}, &errOut)
	if code != 1 || !strings.Contains(errOut.String(), "missing <path>") {
		t.Errorf("expected missing-path error: code=%d err=%q", code, errOut.String())
	}
}

func TestInitScaffoldsAgentField(t *testing.T) {
	dir := t.TempDir()
	code := run([]string{"--chdir", dir, "init"}, &bytes.Buffer{}, &bytes.Buffer{})
	if code != 0 {
		t.Fatalf("init exit code = %d, want 0", code)
	}
	data, err := os.ReadFile(filepath.Join(dir, "ainfra.yaml"))
	if err != nil {
		t.Fatalf("reading scaffolded manifest: %v", err)
	}
	if !strings.Contains(string(data), "agent: claude-code") {
		t.Errorf("scaffolded manifest does not declare  agent: claude-code\n%s", data)
	}
}
