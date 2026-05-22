package artifact

import (
	"os"
	"path/filepath"
	"testing"
)

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWriteThenVerifyRoundTrips(t *testing.T) {
	dir := t.TempDir()
	d := Descriptor{SchemaVersion: 1, ArtifactURL: "https://x", Agent: "claude-desktop",
		Sync: Sync{IntervalMinutes: 360, RunAtLogin: true}}
	files := map[string][]byte{"ainfra.lock": []byte("version: 1\n")}
	if err := Write(dir, d, files); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := Verify(dir); err != nil {
		t.Fatalf("Verify: %v", err)
	}
}

func TestVerifyDetectsTampering(t *testing.T) {
	dir := t.TempDir()
	d := Descriptor{SchemaVersion: 1, ArtifactURL: "https://x", Agent: "claude-desktop"}
	must(t, Write(dir, d, map[string][]byte{"ainfra.lock": []byte("a")}))
	must(t, os.WriteFile(filepath.Join(dir, "ainfra.lock"), []byte("tampered"), 0o644))
	if Verify(dir) == nil {
		t.Error("Verify must reject a tampered artifact")
	}
}

func TestReadDescriptor(t *testing.T) {
	dir := t.TempDir()
	in := Descriptor{SchemaVersion: 1, ArtifactURL: "https://x", Agent: "claude-desktop"}
	must(t, Write(dir, in, map[string][]byte{"ainfra.lock": []byte("a")}))
	got, err := ReadDescriptor(dir)
	if err != nil || got.ArtifactURL != "https://x" {
		t.Fatalf("ReadDescriptor: %v / %+v", err, got)
	}
}
