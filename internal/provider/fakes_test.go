package provider

import (
	"os"
	"testing"
)

func TestMemFilesystem(t *testing.T) {
	fs := NewMemFilesystem()
	if _, err := fs.Stat("/x"); !os.IsNotExist(err) {
		t.Errorf("absent stat = %v, want not-exist", err)
	}
	if err := fs.MkdirAll("/a/b", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := fs.WriteFile("/a/b/f", []byte("v"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := fs.ReadFile("/a/b/f")
	if err != nil || string(got) != "v" {
		t.Fatalf("read = %q %v", got, err)
	}
	if err := fs.Remove("/a/b/f"); err != nil {
		t.Fatal(err)
	}
	if _, err := fs.ReadFile("/a/b/f"); !os.IsNotExist(err) {
		t.Errorf("read after remove = %v, want not-exist", err)
	}
}

func TestFakeRunner(t *testing.T) {
	r := NewFakeRunner()
	r.Script["brew --version"] = FakeResult{Output: []byte("Homebrew 4.0")}
	out, err := r.Run("brew", "--version")
	if err != nil || string(out) != "Homebrew 4.0" {
		t.Fatalf("run = %q %v", out, err)
	}
	if len(r.Calls) != 1 || r.Calls[0] != "brew --version" {
		t.Errorf("calls = %v", r.Calls)
	}
	if _, err := r.Run("unknown"); err == nil {
		t.Error("an unscripted command must error")
	}
}
