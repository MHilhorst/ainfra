package lockfile

import (
	"path/filepath"
	"testing"
)

func TestPluginBaseline_RoundTrips(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ainfra.lock")

	l := &Lock{
		Version: 1,
		Plugin:  &PluginBaseline{Name: "tvt-config", Version: "2.11.0", ContentHash: "abc123"},
	}
	if err := Write(path, l); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.Plugin == nil {
		t.Fatal("plugin baseline lost on round-trip")
	}
	if got.Plugin.Version != "2.11.0" || got.Plugin.ContentHash != "abc123" {
		t.Errorf("got %+v", got.Plugin)
	}
}
