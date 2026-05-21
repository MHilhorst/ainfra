package fetch_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MHilhorst/ainfra/internal/provider/fetch"
)

func TestLocalFetcher_LocalDirectory(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "b.txt"), []byte("world"), 0o644); err != nil {
		t.Fatal(err)
	}

	f := fetch.LocalFetcher{Root: dir}
	bundle, err := f.Fetch(dir, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := string(bundle["a.txt"]); got != "hello" {
		t.Errorf("a.txt = %q, want %q", got, "hello")
	}
	if got := string(bundle[filepath.Join("sub", "b.txt")]); got != "world" {
		t.Errorf("sub/b.txt = %q, want %q", got, "world")
	}
}

func TestLocalFetcher_RelativeSource(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "myskill")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "hello.md"), []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}

	f := fetch.LocalFetcher{Root: root}
	bundle, err := f.Fetch("myskill", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := string(bundle["hello.md"]); got != "content" {
		t.Errorf("hello.md = %q, want %q", got, "content")
	}
}

func TestLocalFetcher_RemoteSchemeGit(t *testing.T) {
	f := fetch.LocalFetcher{Root: "/irrelevant"}
	_, err := f.Fetch("git+https://github.com/example/repo", "v1.0.0")
	if err == nil {
		t.Fatal("expected error for git+ scheme, got nil")
	}
	if !strings.Contains(err.Error(), "git+https://github.com/example/repo") {
		t.Errorf("error message does not name the source: %v", err)
	}
	if !strings.Contains(err.Error(), "not supported") {
		t.Errorf("error message does not mention unsupported: %v", err)
	}
}

func TestLocalFetcher_RemoteSchemeNpm(t *testing.T) {
	f := fetch.LocalFetcher{Root: "/irrelevant"}
	_, err := f.Fetch("npm:my-package", "1.2.3")
	if err == nil {
		t.Fatal("expected error for npm: scheme, got nil")
	}
	if !strings.Contains(err.Error(), "npm:my-package") {
		t.Errorf("error message does not name the source: %v", err)
	}
	if !strings.Contains(err.Error(), "not supported") {
		t.Errorf("error message does not mention unsupported: %v", err)
	}
}

func TestFakeFetcher_ReturnsCannedBundle(t *testing.T) {
	bundle := fetch.Bundle{"file.txt": []byte("canned")}
	f := fetch.FakeFetcher{Bundles: map[string]fetch.Bundle{"mysource": bundle}}

	got, err := f.Fetch("mysource", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got["file.txt"]) != "canned" {
		t.Errorf("file.txt = %q, want %q", got["file.txt"], "canned")
	}
}

func TestFakeFetcher_ReturnsErr(t *testing.T) {
	injected := errors.New("injected error")
	f := fetch.FakeFetcher{Err: injected}

	_, err := f.Fetch("any", "")
	if !errors.Is(err, injected) {
		t.Errorf("got %v, want injected error", err)
	}
}

func TestFakeFetcher_MissingSource(t *testing.T) {
	f := fetch.FakeFetcher{Bundles: map[string]fetch.Bundle{}}
	_, err := f.Fetch("missing", "")
	if err == nil {
		t.Fatal("expected error for missing source, got nil")
	}
}
