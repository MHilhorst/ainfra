package lockfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteThenReadRoundTrips(t *testing.T) {
	dir := t.TempDir()
	lock := &Lock{
		Version:     1,
		GeneratedAt: "2026-05-21T00:00:00Z",
		Entries: Entries{
			MCPServers: map[string]Entry{
				"analytics-db": {Layer: "repo", ContentHash: "sha256:abc",
					Resolved: map[string]any{"tunnelPort": 13306}},
			},
			BackgroundServices: map[string]Entry{
				"analytics-db-tunnel": {Layer: "repo", ContentHash: "sha256:def"},
			},
		},
	}
	path := filepath.Join(dir, "ainfra.lock")
	if err := Write(path, lock); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.Entries.MCPServers["analytics-db"].ContentHash != "sha256:abc" {
		t.Errorf("round-trip lost mcpServer data: %+v", got)
	}
	if got.Entries.BackgroundServices["analytics-db-tunnel"].ContentHash != "sha256:def" {
		t.Errorf("round-trip lost backgroundService data: %+v", got)
	}
}

func TestReadMissingFileReturnsEmptyLock(t *testing.T) {
	got, err := Read(filepath.Join(t.TempDir(), "absent.lock"))
	if err != nil {
		t.Fatalf("Read of missing file should not error: %v", err)
	}
	if got.Version != 1 ||
		len(got.Entries.MCPServers) != 0 ||
		len(got.Entries.BackgroundServices) != 0 ||
		len(got.Entries.CLITools) != 0 {
		t.Errorf("want empty v1 lock with all maps non-nil, got %+v", got)
	}
}

func TestReadSparseFileInitialisesMaps(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ainfra.lock")
	// A lockfile with no entries section at all.
	if err := writeFile(t, path, "version: 1\n"); err != nil {
		t.Fatal(err)
	}
	got, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	// All six channel maps must be non-nil so a caller can write into them.
	got.Entries.MCPServers["a"] = Entry{}
	got.Entries.BackgroundServices["b"] = Entry{}
	got.Entries.Hooks["c"] = Entry{}
	got.Entries.Commands["d"] = Entry{}
	got.Entries.ScheduledJobs["e"] = Entry{}
	got.Entries.CLITools["f"] = Entry{}
}

func TestWriteThenReadRoundTripsScheduledJobs(t *testing.T) {
	dir := t.TempDir()
	lock := &Lock{Version: 1, Entries: Entries{
		ScheduledJobs: map[string]Entry{
			"nightly": {Layer: "repo", RunsOn: []string{"hub"}, ContentHash: "sha256:xyz"},
		},
	}}
	path := filepath.Join(dir, "ainfra.lock")
	if err := Write(path, lock); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	e := got.Entries.ScheduledJobs["nightly"]
	if e.ContentHash != "sha256:xyz" || len(e.RunsOn) != 1 || e.RunsOn[0] != "hub" {
		t.Errorf("round-trip lost scheduled job data: %+v", e)
	}
}

func writeFile(t *testing.T, path, body string) error {
	t.Helper()
	return os.WriteFile(path, []byte(body), 0o644)
}
