package xdg

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigHome_Default(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	dir, err := ConfigHome()
	if err != nil {
		t.Fatalf("ConfigHome: %v", err)
	}
	// Default falls back to ~/.config/ainfra/.
	if !strings.HasSuffix(dir, filepath.Join(".config", "ainfra")) {
		t.Errorf("default ConfigHome should end with .config/ainfra, got %q", dir)
	}
}

func TestConfigHome_XDGOverride(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg-custom")
	dir, err := ConfigHome()
	if err != nil {
		t.Fatalf("ConfigHome: %v", err)
	}
	want := filepath.Join("/tmp/xdg-custom", "ainfra")
	if dir != want {
		t.Errorf("ConfigHome with XDG_CONFIG_HOME set: want %q, got %q", want, dir)
	}
}

func TestPersonalManifestPath(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg")
	got, err := PersonalManifestPath()
	if err != nil {
		t.Fatalf("PersonalManifestPath: %v", err)
	}
	want := "/tmp/xdg/ainfra/personal.yaml"
	if got != want {
		t.Errorf("PersonalManifestPath: want %q, got %q", want, got)
	}
}

func TestAppliedLedgerPath(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg")
	got, err := AppliedLedgerPath()
	if err != nil {
		t.Fatalf("AppliedLedgerPath: %v", err)
	}
	want := "/tmp/xdg/ainfra/applied.lock"
	if got != want {
		t.Errorf("AppliedLedgerPath: want %q, got %q", want, got)
	}
}
