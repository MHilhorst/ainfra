package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MHilhorst/ainfra/internal/artifact"
	"github.com/MHilhorst/ainfra/internal/provider"
)

// buildTestArtifact creates a minimal valid artifact directory for claude-desktop.
// It writes ainfra.lock, rendered.json, and the descriptor, then computes the
// MANIFEST.sha256 via artifact.Write.
func buildTestArtifact(t *testing.T, resources map[string][]provider.Resource) string {
	t.Helper()
	dir := t.TempDir()

	lockBytes := []byte("version: 1\n")

	renderedBytes, err := json.MarshalIndent(resources, "", "  ")
	if err != nil {
		t.Fatalf("buildTestArtifact: marshal rendered: %v", err)
	}

	desc := artifact.Descriptor{
		SchemaVersion: 1,
		ArtifactURL:   "https://example.com/artifact",
		Agent:         "claude-desktop",
		Sync: artifact.Sync{
			IntervalMinutes: 60,
			RunAtLogin:      true,
		},
	}
	files := map[string][]byte{
		"ainfra.lock":   lockBytes,
		"rendered.json": renderedBytes,
	}
	if err := artifact.Write(dir, desc, files); err != nil {
		t.Fatalf("buildTestArtifact: artifact.Write: %v", err)
	}
	return dir
}

func TestApplyFromLocalArtifact(t *testing.T) {
	resources := map[string][]provider.Resource{
		"mcpServers": {
			{
				ID:      "demo",
				Channel: "mcpServers",
				Payload: map[string]any{
					"command": "npx",
					"args":    []any{"-y", "demo"},
				},
			},
		},
	}
	artifactDir := buildTestArtifact(t, resources)

	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	var out, errOut bytes.Buffer
	code := run([]string{"apply", "--from", artifactDir, "--yes"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("apply --from: code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}

	configPath := filepath.Join(tmpHome, "Library", "Application Support", "Claude", "claude_desktop_config.json")
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("apply --from: claude_desktop_config.json not written at %s: %v", configPath, err)
	}
	content := string(raw)
	if !strings.Contains(content, "demo") {
		t.Errorf("apply --from: expected 'demo' in config, got: %q", content)
	}
	if !strings.Contains(content, "npx") {
		t.Errorf("apply --from: expected 'npx' in config, got: %q", content)
	}
}

func TestApplyFromRejectsTamperedArtifact(t *testing.T) {
	resources := map[string][]provider.Resource{
		"mcpServers": {
			{
				ID:      "demo",
				Channel: "mcpServers",
				Payload: map[string]any{"command": "npx", "args": []any{"-y", "demo"}},
			},
		},
	}
	artifactDir := buildTestArtifact(t, resources)

	// Tamper with ainfra.lock to break the hash.
	if err := os.WriteFile(filepath.Join(artifactDir, "ainfra.lock"), []byte("tampered: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	var out, errOut bytes.Buffer
	code := run([]string{"apply", "--from", artifactDir, "--yes"}, &out, &errOut)
	if code == 0 {
		t.Fatal("apply --from tampered artifact: expected non-zero exit, got 0")
	}

	configPath := filepath.Join(tmpHome, "Library", "Application Support", "Claude", "claude_desktop_config.json")
	if _, err := os.Stat(configPath); err == nil {
		t.Error("apply --from tampered artifact: claude_desktop_config.json should NOT have been written")
	}
}
