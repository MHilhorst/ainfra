package adopt

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadRulesBothFiles(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("x"), 0o644)
	out := readRules(RepoLayout(dir).Rules)
	if len(out) != 2 {
		t.Fatalf("expected 2, got %+v", out)
	}
}

func TestReadRulesNone(t *testing.T) {
	if out := readRules(RepoLayout(t.TempDir()).Rules); out != nil {
		t.Errorf("expected nil, got %+v", out)
	}
}
