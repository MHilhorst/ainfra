package provider

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/MHilhorst/ainfra/internal/provider/fetch"
)

func TestOSFilesystemRoundTrip(t *testing.T) {
	fs := OSFilesystem{}
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "f.txt")
	if err := fs.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := fs.WriteFile(path, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := fs.ReadFile(path)
	if err != nil || string(got) != "hi" {
		t.Fatalf("read = %q, %v", got, err)
	}
	if _, err := fs.Stat(path); err != nil {
		t.Fatalf("stat: %v", err)
	}
	if err := fs.Remove(path); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := fs.Stat(path); !os.IsNotExist(err) {
		t.Errorf("stat after remove = %v, want not-exist", err)
	}
}

func TestEnvFetchField(t *testing.T) {
	// Confirm the Fetch field exists and accepts a fetch.Fetcher.
	bundle := fetch.Bundle{"hello.txt": []byte("world")}
	env := Env{
		Fetch: fetch.FakeFetcher{Bundles: map[string]fetch.Bundle{"src": bundle}},
	}
	got, err := env.Fetch.Fetch("src", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got["hello.txt"]) != "world" {
		t.Errorf("hello.txt = %q, want %q", got["hello.txt"], "world")
	}
}

func TestEnvZeroValueUsable(t *testing.T) {
	// A zero Env must not panic; Fetch is nil but that is valid until a provider uses it.
	var env Env
	if env.Fetch != nil {
		t.Errorf("zero Env.Fetch should be nil, got %v", env.Fetch)
	}
}
