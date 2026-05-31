package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newPluginRepo writes a minimal repo with a plugin: block, a skill, a
// marketplace.json self-entry, and an ainfra.lock baseline at 1.0.0. It also
// clears PATH so `claude plugin validate` is deterministically skipped.
func newPluginRepo(t *testing.T) string {
	t.Helper()
	t.Setenv("PATH", "")
	dir := t.TempDir()
	must := func(rel, body string) {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	must("ainfra.yaml", `version: 1
agent: claude-code
plugin:
  name: tvt-config
  description: "Team config"
  marketplace: trein-vertraging
  content: [ skills/ ]
`)
	must("skills/demo/SKILL.md", "---\ndescription: demo\n---\nbody\n")
	must(".claude-plugin/marketplace.json", `{
  "name": "trein-vertraging",
  "plugins": [
    { "name": "tvt-config", "source": "./", "description": "old" }
  ]
}`)
	must("ainfra.lock", `version: 1
plugin:
  name: tvt-config
  version: 1.0.0
  contentHash: deadbeef
`)
	return dir
}

func TestPlugin_ReleaseDriftGuard(t *testing.T) {
	dir := newPluginRepo(t)
	var errOut bytes.Buffer
	code := run([]string{"--chdir", dir, "plugin", "release"}, &bytes.Buffer{}, &errOut)
	if code == 0 {
		t.Fatal("expected non-zero exit on drift without bump")
	}
	if !strings.Contains(errOut.String(), "changed since v1.0.0") {
		t.Errorf("want drift message, got %q", errOut.String())
	}
}

func TestPlugin_ReleasePatch(t *testing.T) {
	dir := newPluginRepo(t)
	mkBefore, err := os.ReadFile(filepath.Join(dir, ".claude-plugin", "marketplace.json"))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	code := run([]string{"--chdir", dir, "plugin", "release", "--patch"}, &out, &bytes.Buffer{})
	if code != 0 {
		t.Fatalf("release --patch failed: code=%d out=%s", code, out.String())
	}

	pj, err := os.ReadFile(filepath.Join(dir, ".claude-plugin", "plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(pj, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["version"] != "1.0.1" {
		t.Errorf("plugin.json version = %v want 1.0.1", doc["version"])
	}

	lock, _ := os.ReadFile(filepath.Join(dir, "ainfra.lock"))
	if !strings.Contains(string(lock), "version: 1.0.1") {
		t.Errorf("lock not updated: %s", lock)
	}
	if strings.Contains(string(lock), "deadbeef") {
		t.Errorf("lock still has stale hash: %s", lock)
	}

	mkAfter, err := os.ReadFile(filepath.Join(dir, ".claude-plugin", "marketplace.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(mkBefore, mkAfter) {
		t.Errorf("marketplace.json must be left untouched.\nbefore:\n%s\nafter:\n%s", mkBefore, mkAfter)
	}
}
