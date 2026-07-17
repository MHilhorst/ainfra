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

// newSHAVersionedPluginRepo is newPluginRepo with `versioning: sha` — the plugin
// declares no version and lets Claude Code use the commit SHA.
func newSHAVersionedPluginRepo(t *testing.T) string {
	t.Helper()
	dir := newPluginRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(`version: 1
agent: claude-code
plugin:
  name: tvt-config
  description: "Team config"
  marketplace: trein-vertraging
  versioning: sha
  content: [ skills/ ]
`), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestPlugin_BuildSHAVersionedOmitsVersion(t *testing.T) {
	dir := newSHAVersionedPluginRepo(t)
	var out, errOut bytes.Buffer
	if code := run([]string{"--chdir", dir, "plugin", "build"}, &out, &errOut); code != 0 {
		t.Fatalf("build exited %d: %s", code, errOut.String())
	}
	raw, err := os.ReadFile(filepath.Join(dir, ".claude-plugin", "plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if _, present := doc["version"]; present {
		t.Errorf("versioning: sha must omit the version key so Claude Code uses the\n"+
			"commit SHA; got: %s", raw)
	}
}

// TestPlugin_ReleaseRejectedWhenSHAVersioned: releasing is meaningless for a
// SHA-versioned plugin — there is no version to bump and every commit already
// ships. Failing loudly beats silently writing a version key that would pin
// every user and stop updates dead.
func TestPlugin_ReleaseRejectedWhenSHAVersioned(t *testing.T) {
	dir := newSHAVersionedPluginRepo(t)
	var errOut bytes.Buffer
	code := run([]string{"--chdir", dir, "plugin", "release", "--patch"}, &bytes.Buffer{}, &errOut)
	if code == 0 {
		t.Fatal("expected non-zero exit releasing a SHA-versioned plugin")
	}
	if !strings.Contains(errOut.String(), "versioning: sha") {
		t.Errorf("error should name the versioning mode, got %q", errOut.String())
	}
}

// TestPlugin_BuildRejectsUnknownVersioning: `plugin build` loads the manifest
// directly, so it must validate the plugin block itself. Without this, a typo'd
// `versioning:` silently renders a pinned version — the author believes every
// commit ships while their users are frozen at whatever version last shipped.
func TestPlugin_BuildRejectsUnknownVersioning(t *testing.T) {
	dir := newPluginRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(`version: 1
agent: claude-code
plugin:
  name: tvt-config
  description: "Team config"
  marketplace: trein-vertraging
  versioning: shaa
  content: [ skills/ ]
`), 0o644); err != nil {
		t.Fatal(err)
	}
	var errOut bytes.Buffer
	code := run([]string{"--chdir", dir, "plugin", "build"}, &bytes.Buffer{}, &errOut)
	if code == 0 {
		t.Fatal("expected non-zero exit for an unknown versioning mode")
	}
	if !strings.Contains(errOut.String(), "versioning") {
		t.Errorf("error should name the offending field, got %q", errOut.String())
	}
}
