package fetch_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/MHilhorst/ainfra/internal/provider/fetch"
)

func TestCache_PutAndGet(t *testing.T) {
	dir := t.TempDir()
	c := &fetch.Cache{Root: dir}

	r := fetch.Resolved{Scheme: "github", Address: "acme/skills@abc"}
	b := fetch.Bundle{
		"SKILL.md":         []byte("# skill"),
		"sub/instructions": []byte("hi"),
	}
	if err := c.Put(r, b); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, ok := c.Get(r)
	if !ok {
		t.Fatal("Get: cache miss after Put")
	}
	if string(got["SKILL.md"]) != "# skill" {
		t.Errorf("SKILL.md content mismatch: %q", got["SKILL.md"])
	}
	if string(got["sub/instructions"]) != "hi" {
		t.Errorf("sub/instructions content mismatch: %q", got["sub/instructions"])
	}
}

func TestCache_DeleteCausesMiss(t *testing.T) {
	dir := t.TempDir()
	c := &fetch.Cache{Root: dir}
	r := fetch.Resolved{Scheme: "npm", Address: "lodash@4.17.21"}
	if err := c.Put(r, fetch.Bundle{"index.js": []byte("ok")}); err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(dir, c.Key(r))
	if err := os.RemoveAll(entry); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Get(r); ok {
		t.Fatal("expected miss after delete")
	}
}

func TestCache_KeyIsStable(t *testing.T) {
	c := &fetch.Cache{Root: "/tmp"}
	r1 := fetch.Resolved{Scheme: "github", Address: "a"}
	r2 := fetch.Resolved{Scheme: "github", Address: "a"}
	r3 := fetch.Resolved{Scheme: "github", Address: "b"}
	if c.Key(r1) != c.Key(r2) {
		t.Error("identical resolved should produce identical keys")
	}
	if c.Key(r1) == c.Key(r3) {
		t.Error("different addresses should produce different keys")
	}
}

func TestCache_GetCorruptManifestMisses(t *testing.T) {
	dir := t.TempDir()
	c := &fetch.Cache{Root: dir}
	r := fetch.Resolved{Scheme: "github", Address: "x"}
	if err := c.Put(r, fetch.Bundle{"a": []byte("a")}); err != nil {
		t.Fatal(err)
	}
	// Truncate the file so the manifest sha no longer matches.
	if err := os.WriteFile(filepath.Join(dir, c.Key(r), "contents", "a"), []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Get(r); ok {
		t.Fatal("expected miss when content sha mismatches")
	}
}
