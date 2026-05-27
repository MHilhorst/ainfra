package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestUpdate_BareReResolves(t *testing.T) {
	dir := newDemoRepo(t)
	if code := run([]string{"--chdir", dir, "lock"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("lock failed")
	}
	if code := run([]string{"--chdir", dir, "install", "--yes"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("install failed")
	}
	var out bytes.Buffer
	code := run([]string{"--chdir", dir, "update"}, &out, &bytes.Buffer{})
	if code != 0 {
		t.Fatalf("update bare: want code=0, got %d out=%q", code, out.String())
	}
	if !strings.Contains(out.String(), "Re-resolved lockfile") {
		t.Errorf("update: want 'Re-resolved lockfile' in output, got %q", out.String())
	}
}

func TestUpdate_PerEntry(t *testing.T) {
	dir := newDemoRepo(t)
	if code := run([]string{"--chdir", dir, "lock"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("lock failed")
	}
	code := run([]string{"--chdir", dir, "update", "mcp", "repo"}, &bytes.Buffer{}, &bytes.Buffer{})
	if code != 0 {
		t.Errorf("update mcp repo: want code=0, got %d", code)
	}
}

func TestUpdate_UnknownChannel(t *testing.T) {
	dir := newDemoRepo(t)
	if code := run([]string{"--chdir", dir, "lock"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("lock failed")
	}
	var errOut bytes.Buffer
	code := run([]string{"--chdir", dir, "update", "mcps", "github"}, &bytes.Buffer{}, &errOut)
	if code != 1 {
		t.Errorf("update unknown channel: want code=1, got %d", code)
	}
}

func TestUpdate_NoInstall(t *testing.T) {
	dir := newDemoRepo(t)
	if code := run([]string{"--chdir", dir, "lock"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("lock failed")
	}
	code := run([]string{"--chdir", dir, "update", "--no-install"}, &bytes.Buffer{}, &bytes.Buffer{})
	if code != 0 {
		t.Errorf("update --no-install: want code=0, got %d", code)
	}
}
