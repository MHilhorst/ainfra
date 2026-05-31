package plugin

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/MHilhorst/ainfra/internal/manifest"
)

func writeFile(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestContentHash_StableAndSensitive(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "skills/a/SKILL.md", "alpha")
	writeFile(t, root, ".mcp.json", "{}")
	writeFile(t, root, "ignored.txt", "noise")

	h1, err := ContentHash(root, []string{"skills/", ".mcp.json"})
	if err != nil {
		t.Fatal(err)
	}
	h2, err := ContentHash(root, []string{"skills/", ".mcp.json"})
	if err != nil {
		t.Fatal(err)
	}
	if h1 != h2 {
		t.Error("hash not stable across calls")
	}
	writeFile(t, root, "ignored.txt", "different noise")
	h3, err := ContentHash(root, []string{"skills/", ".mcp.json"})
	if err != nil {
		t.Fatal(err)
	}
	if h1 != h3 {
		t.Error("hash changed due to unrelated file")
	}
	writeFile(t, root, "skills/a/SKILL.md", "beta")
	h4, err := ContentHash(root, []string{"skills/", ".mcp.json"})
	if err != nil {
		t.Fatal(err)
	}
	if h1 == h4 {
		t.Error("hash did not change when tracked content changed")
	}
}

func TestContentHash_OrderIndependent(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "skills/a/SKILL.md", "alpha")
	writeFile(t, root, "commands/x.md", "cmd")
	writeFile(t, root, ".mcp.json", "{}")

	a, err := ContentHash(root, []string{"skills/", "commands/", ".mcp.json"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := ContentHash(root, []string{".mcp.json", "commands/", "skills/"})
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Errorf("hash depends on path order: %s != %s", a, b)
	}
}

func TestContentHash_RenameSensitive(t *testing.T) {
	root1 := t.TempDir()
	writeFile(t, root1, "skills/a/SKILL.md", "same body")
	h1, err := ContentHash(root1, []string{"skills/"})
	if err != nil {
		t.Fatal(err)
	}

	root2 := t.TempDir()
	writeFile(t, root2, "skills/b/SKILL.md", "same body")
	h2, err := ContentHash(root2, []string{"skills/"})
	if err != nil {
		t.Fatal(err)
	}
	if h1 == h2 {
		t.Error("hash ignored the file path; a rename should change the hash")
	}
}

func TestContentHash_MissingPathIgnored(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, ".mcp.json", "{}")
	if _, err := ContentHash(root, []string{"skills/", ".mcp.json"}); err != nil {
		t.Errorf("missing dir should be ignored, got %v", err)
	}
}

func TestReleaseHash_ContentAndMetadataSensitive(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "skills/a/SKILL.md", "x")
	base := manifest.PluginBuild{
		Name: "p", Description: "one", Marketplace: "m",
		Content: []string{"skills/"},
	}
	h1, err := ReleaseHash(root, base)
	if err != nil {
		t.Fatal(err)
	}

	// Metadata-only change must change the hash.
	meta := base
	meta.Description = "two"
	h2, err := ReleaseHash(root, meta)
	if err != nil {
		t.Fatal(err)
	}
	if h1 == h2 {
		t.Error("metadata change must change the release hash")
	}

	// Version is NOT part of the drift hash (it's the bumped value).
	// Content change must change the hash.
	writeFile(t, root, "skills/a/SKILL.md", "y")
	h3, err := ReleaseHash(root, base)
	if err != nil {
		t.Fatal(err)
	}
	if h1 == h3 {
		t.Error("content change must change the release hash")
	}
}
