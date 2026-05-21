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

func TestMemFilesystemReadDir(t *testing.T) {
	fs := NewMemFilesystem()

	// Missing directory returns not-exist error.
	_, err := fs.ReadDir("/repo/.claude/commands")
	if !os.IsNotExist(err) {
		t.Fatalf("ReadDir missing dir = %v, want not-exist", err)
	}

	// Empty but recorded directory returns empty slice, no error.
	if err := fs.MkdirAll("/repo/.claude/commands", 0o755); err != nil {
		t.Fatal(err)
	}
	names, err := fs.ReadDir("/repo/.claude/commands")
	if err != nil {
		t.Fatalf("ReadDir empty dir = %v", err)
	}
	if len(names) != 0 {
		t.Errorf("ReadDir empty dir = %v, want []", names)
	}

	// Files in the directory are returned as base names.
	if err := fs.WriteFile("/repo/.claude/commands/foo.md", []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := fs.WriteFile("/repo/.claude/commands/bar.md", []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A file in a deeper path should not appear.
	if err := fs.WriteFile("/repo/.claude/commands/sub/baz.md", []byte("z"), 0o644); err != nil {
		t.Fatal(err)
	}

	names, err = fs.ReadDir("/repo/.claude/commands")
	if err != nil {
		t.Fatalf("ReadDir = %v", err)
	}
	want := []string{"bar.md", "foo.md"}
	if len(names) != len(want) {
		t.Fatalf("ReadDir names = %v, want %v", names, want)
	}
	for i, n := range names {
		if n != want[i] {
			t.Errorf("names[%d] = %q, want %q", i, n, want[i])
		}
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
