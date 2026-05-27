package adopt

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MHilhorst/ainfra/internal/manifest"
)

func TestEmitEmptyManifest(t *testing.T) {
	out, err := Emit(manifest.Manifest{Version: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(out, []byte("\n")) {
		t.Errorf("missing trailing newline: %q", out)
	}
	if !strings.Contains(string(out), "version: 1") {
		t.Errorf("missing version: %s", out)
	}
}

func TestEmitKeyOrder(t *testing.T) {
	m := manifest.Manifest{
		Version: 1,
		Agent:   "claude-code",
		Secrets: map[string]manifest.Secret{"x": {Mode: "direct", Scope: "personal", Ref: "z"}},
		MCPServers: map[string]manifest.MCPServer{
			"s": {Transport: "http", URL: "https://example.com"},
		},
		Hooks: map[string]manifest.Hook{
			"h": {Event: "PostToolUse", Command: "echo"},
		},
		Commands: map[string]manifest.Command{"c": {Source: "./x.md"}},
		Rules:    map[string]manifest.Rule{"r": {Source: "./CLAUDE.md", Target: "CLAUDE.md"}},
	}
	out, err := Emit(m)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	wantOrder := []string{"version:", "agent:", "secrets:", "mcpServers:", "hooks:", "commands:", "rules:"}
	lastIdx := -1
	for _, key := range wantOrder {
		idx := strings.Index(s, key)
		if idx < 0 {
			t.Errorf("missing key %q in:\n%s", key, s)
			continue
		}
		if idx <= lastIdx {
			t.Errorf("key %q out of order in:\n%s", key, s)
		}
		lastIdx = idx
	}
}

func TestEmitRoundTrip(t *testing.T) {
	dir := t.TempDir()
	original := manifest.Manifest{
		Version: 1,
		Agent:   "claude-code",
		MCPServers: map[string]manifest.MCPServer{
			"foo": {Transport: "http", URL: "https://example.com/sse"},
		},
		Commands: map[string]manifest.Command{"bar": {Source: "./.claude/commands/bar.md"}},
	}
	out1, err := Emit(original)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "ainfra.yaml")
	if err := os.WriteFile(path, out1, 0o644); err != nil {
		t.Fatal(err)
	}
	layers, err := manifest.LoadLayers(dir)
	if err != nil {
		t.Fatalf("LoadLayers: %v\n---\n%s", err, out1)
	}
	repo := layers[manifest.LayerRepo]
	if repo == nil {
		t.Fatal("no repo layer")
	}
	out2, err := Emit(*repo)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out1, out2) {
		t.Errorf("round-trip mismatch:\n--- first ---\n%s\n--- second ---\n%s", out1, out2)
	}
}
