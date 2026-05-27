package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemove_NoEntry(t *testing.T) {
	dir := newDemoRepo(t)
	var errOut bytes.Buffer
	code := run([]string{"--chdir", dir, "remove", "mcp", "nope"}, &bytes.Buffer{}, &errOut)
	if code != 1 {
		t.Fatalf("remove nonexistent: want code=1, got %d", code)
	}
	if !strings.Contains(errOut.String(), "not found") {
		t.Errorf("remove nonexistent: want 'not found', got %q", errOut.String())
	}
}

func TestRemove_AfterAdd_RoundTrips(t *testing.T) {
	dir := newDemoRepo(t)
	original, _ := os.ReadFile(filepath.Join(dir, "ainfra.yaml"))

	if code := run([]string{"--chdir", dir, "add", "--no-install", "mcp", "tmp"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("add failed")
	}
	if code := run([]string{"--chdir", dir, "remove", "--no-install", "mcp", "tmp"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("remove failed")
	}
	after, _ := os.ReadFile(filepath.Join(dir, "ainfra.yaml"))
	if string(after) != string(original) {
		t.Errorf("add then remove did not restore original.\nwant:\n%s\ngot:\n%s", original, after)
	}
}

func TestRemove_WithInstall_DropsFromMcpJSON(t *testing.T) {
	dir := newDemoRepo(t)
	// Install the baseline so .mcp.json has the 'repo' server.
	if code := run([]string{"--chdir", dir, "lock"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("lock failed")
	}
	if code := run([]string{"--chdir", dir, "install", "--yes"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("install failed")
	}
	if code := run([]string{"--chdir", dir, "remove", "mcp", "repo"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("remove failed")
	}
	mcpJSON, err := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	if err == nil && strings.Contains(string(mcpJSON), `"repo"`) {
		t.Errorf("remove with install did not drop repo from .mcp.json: %s", mcpJSON)
	}
}
