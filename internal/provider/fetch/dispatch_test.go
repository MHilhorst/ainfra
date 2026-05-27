package fetch_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MHilhorst/ainfra/internal/provider/fetch"
)

func TestSchemeOf(t *testing.T) {
	cases := []struct{ src, want string }{
		{"github:acme/skills@v1", "github"},
		{"git+https://github.com/acme/skills?ref=v1", "github"},
		{"npm:lodash@4", "npm"},
		{"https://example.com/x", "https"},
		{"http://example.com/x", "https"},
		{"local:./skills/foo", "local"},
		{"./skills/foo", "local"},
		{"/abs/path", "local"},
	}
	for _, c := range cases {
		if got := fetch.SchemeOf(c.src); got != c.want {
			t.Errorf("SchemeOf(%q) = %q, want %q", c.src, got, c.want)
		}
	}
}

func TestMultiSchemeFetcher_DispatchLocal(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "skills/foo")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "SKILL.md"), []byte("body"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := fetch.NewMultiSchemeFetcher(root, &fetch.Cache{Root: t.TempDir()})
	b, err := m.Fetch("skills/foo", "")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if string(b["SKILL.md"]) != "body" {
		t.Errorf("SKILL.md = %q", b["SKILL.md"])
	}

	// "local:" prefix is also accepted.
	if _, err := m.Fetch("local:skills/foo", ""); err != nil {
		t.Errorf("local: prefix should work: %v", err)
	}
}

func TestMultiSchemeFetcher_DispatchUnknown(t *testing.T) {
	m := fetch.NewMultiSchemeFetcher(t.TempDir(), &fetch.Cache{Root: t.TempDir()})
	// "ftp://" is not a recognized scheme; SchemeOf falls back to "local"
	// and LocalFetcher rejects the path traversal/escape, which produces an error.
	_, err := m.Fetch("ftp://example.com", "")
	if err == nil {
		t.Fatal("expected an error for unsupported scheme")
	}
	if !strings.Contains(err.Error(), "ftp") && !strings.Contains(err.Error(), "escapes") && !strings.Contains(err.Error(), "no such file") {
		t.Logf("got error: %v", err)
	}
}
