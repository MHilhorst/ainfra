package lockfile

import (
	"path/filepath"
	"testing"
)

func TestWriteThenReadRoundTrips(t *testing.T) {
	dir := t.TempDir()
	lock := &Lock{
		Version:     1,
		GeneratedAt: "2026-05-21T00:00:00Z",
		Entries: Entries{MCPServers: map[string]Entry{
			"analytics-db": {Layer: "repo", ContentHash: "sha256:abc",
				Resolved: map[string]any{"tunnelPort": 13306}},
		}},
	}
	path := filepath.Join(dir, "ai-stack.lock")
	if err := Write(path, lock); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.Entries.MCPServers["analytics-db"].ContentHash != "sha256:abc" {
		t.Errorf("round-trip lost data: %+v", got)
	}
}

func TestReadMissingFileReturnsEmptyLock(t *testing.T) {
	got, err := Read(filepath.Join(t.TempDir(), "absent.lock"))
	if err != nil {
		t.Fatalf("Read of missing file should not error: %v", err)
	}
	if got.Version != 1 || len(got.Entries.MCPServers) != 0 {
		t.Errorf("want empty v1 lock, got %+v", got)
	}
}
