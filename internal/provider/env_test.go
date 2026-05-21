package provider

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOSFilesystemRoundTrip(t *testing.T) {
	fs := OSFilesystem{}
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "f.txt")
	if err := fs.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := fs.WriteFile(path, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := fs.ReadFile(path)
	if err != nil || string(got) != "hi" {
		t.Fatalf("read = %q, %v", got, err)
	}
	if _, err := fs.Stat(path); err != nil {
		t.Fatalf("stat: %v", err)
	}
	if err := fs.Remove(path); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := fs.Stat(path); !os.IsNotExist(err) {
		t.Errorf("stat after remove = %v, want not-exist", err)
	}
}
