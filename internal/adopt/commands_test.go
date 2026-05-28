package adopt

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadCommandsMissingDir(t *testing.T) {
	dir := t.TempDir()
	out, err := readCommands(filepath.Join(dir, ".claude", "commands"), "./.claude/commands")
	if err != nil {
		t.Fatal(err)
	}
	if out != nil {
		t.Errorf("expected nil for missing dir, got %+v", out)
	}
}

func TestReadCommandsSkipsNonMd(t *testing.T) {
	dir := t.TempDir()
	cmds := filepath.Join(dir, ".claude", "commands")
	if err := os.MkdirAll(cmds, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(cmds, "foo.md"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(cmds, "ignore.txt"), []byte("x"), 0o644)
	out, err := readCommands(cmds, "./.claude/commands")
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Errorf("expected 1, got %+v", out)
	}
}
