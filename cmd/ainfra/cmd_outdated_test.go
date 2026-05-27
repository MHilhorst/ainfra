package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestOutdated_UpToDate(t *testing.T) {
	dir := newDemoRepo(t)
	if code := run([]string{"--chdir", dir, "lock"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("lock failed")
	}
	var out bytes.Buffer
	code := run([]string{"--chdir", dir, "outdated"}, &out, &bytes.Buffer{})
	if code != 0 {
		t.Fatalf("outdated (no stale entries): want code=0, got %d", code)
	}
	if !strings.Contains(out.String(), "Up to date.") {
		t.Errorf("outdated: expected 'Up to date.', got %q", out.String())
	}
}

func TestOutdated_StrictNoStale(t *testing.T) {
	dir := newDemoRepo(t)
	if code := run([]string{"--chdir", dir, "lock"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("lock failed")
	}
	code := run([]string{"--chdir", dir, "outdated", "--strict"}, &bytes.Buffer{}, &bytes.Buffer{})
	if code != 0 {
		t.Errorf("outdated --strict (no stale): want code=0, got %d", code)
	}
}

func TestOutdated_NoLockfile(t *testing.T) {
	dir := t.TempDir()
	code := run([]string{"--chdir", dir, "outdated"}, &bytes.Buffer{}, &bytes.Buffer{})
	if code != 1 {
		t.Errorf("outdated with no lockfile: want code=1, got %d", code)
	}
}
