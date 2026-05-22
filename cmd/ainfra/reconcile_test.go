package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProvidersForDir_DefaultsToClaudeCode(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte("version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	providers, err := providersForDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(providers) != 10 {
		t.Fatalf("providersForDir returned %d providers, want 10 (claude-code default)", len(providers))
	}
}

func TestBuildEnv_Fields(t *testing.T) {
	dir := t.TempDir()
	env := buildEnv(dir)

	if env.Root != dir {
		t.Errorf("Root = %q, want %q", env.Root, dir)
	}
	if env.FS == nil {
		t.Error("FS is nil, want non-nil")
	}
	if env.Runner == nil {
		t.Error("Runner is nil, want non-nil")
	}
	if env.Fetch == nil {
		t.Error("Fetch is nil, want non-nil")
	}
}
