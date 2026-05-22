package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteSettingsEnv_CreatesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".claude", "settings.local.json")

	if err := writeSettingsEnv(path, map[string]string{"FLARE_API_TOKEN": "tok"}); err != nil {
		t.Fatalf("writeSettingsEnv: %v", err)
	}

	var doc map[string]any
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	env, ok := doc["env"].(map[string]any)
	if !ok || env["FLARE_API_TOKEN"] != "tok" {
		t.Fatalf("env = %v, want FLARE_API_TOKEN=tok", doc["env"])
	}
}

func TestWriteSettingsEnv_PreservesOtherKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.local.json")
	existing := `{"permissions":{"allow":["Bash(ls)"]},"env":{"USER_SET":"keep"}}`
	if err := os.WriteFile(path, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := writeSettingsEnv(path, map[string]string{"FLARE_API_TOKEN": "tok"}); err != nil {
		t.Fatalf("writeSettingsEnv: %v", err)
	}

	var doc map[string]any
	raw, _ := os.ReadFile(path)
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	// Non-env key survives.
	if _, ok := doc["permissions"]; !ok {
		t.Error("permissions key was dropped")
	}
	env := doc["env"].(map[string]any)
	// A pre-existing, unmanaged env entry survives.
	if env["USER_SET"] != "keep" {
		t.Errorf("USER_SET = %v, want keep", env["USER_SET"])
	}
	// The managed secret is written.
	if env["FLARE_API_TOKEN"] != "tok" {
		t.Errorf("FLARE_API_TOKEN = %v, want tok", env["FLARE_API_TOKEN"])
	}
}

func TestWriteSettingsEnv_FilePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.local.json")
	if err := writeSettingsEnv(path, map[string]string{"X": "y"}); err != nil {
		t.Fatalf("writeSettingsEnv: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("file mode = %o, want 600 (it holds secrets)", perm)
	}
}
