package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestList_EmptyLockfile(t *testing.T) {
	dir := newDemoRepo(t)
	if code := run([]string{"--chdir", dir, "lock"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("lock failed")
	}
	var out bytes.Buffer
	code := run([]string{"--chdir", dir, "list"}, &out, &bytes.Buffer{})
	if code != 0 {
		t.Fatalf("list: code=%d out=%q", code, out.String())
	}
	if !strings.Contains(out.String(), "repo") || !strings.Contains(out.String(), "mcpServers") {
		t.Errorf("list: expected mcpServers row for 'repo', got %q", out.String())
	}
}

func TestList_ChannelFilter(t *testing.T) {
	dir := newDemoRepo(t)
	if code := run([]string{"--chdir", dir, "lock"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("lock failed")
	}
	var out bytes.Buffer
	code := run([]string{"--chdir", dir, "list", "--channel", "hooks"}, &out, &bytes.Buffer{})
	if code != 0 {
		t.Fatalf("list --channel hooks: code=%d", code)
	}
	if !strings.Contains(out.String(), "No entries.") {
		t.Errorf("list --channel hooks on a no-hooks manifest: expected 'No entries.', got %q", out.String())
	}
}

func TestList_JSON(t *testing.T) {
	dir := newDemoRepo(t)
	if code := run([]string{"--chdir", dir, "lock"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("lock failed")
	}
	var out bytes.Buffer
	code := run([]string{"--chdir", dir, "list", "--json"}, &out, &bytes.Buffer{})
	if code != 0 {
		t.Fatalf("list --json: code=%d", code)
	}
	dec := json.NewDecoder(strings.NewReader(out.String()))
	var rows []listEntry
	for dec.More() {
		var e listEntry
		if err := dec.Decode(&e); err != nil {
			t.Fatalf("list --json: decode: %v", err)
		}
		rows = append(rows, e)
	}
	if len(rows) == 0 {
		t.Errorf("list --json: expected at least one row, got 0")
	}
	for _, r := range rows {
		if r.Channel == "" || r.ID == "" {
			t.Errorf("list --json: row missing channel or id: %+v", r)
		}
	}
}

func TestList_JSONIncludesToolsetHash(t *testing.T) {
	dir := newDemoRepo(t)
	if code := run([]string{"--chdir", dir, "lock"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("lock failed")
	}
	var out bytes.Buffer
	if code := run([]string{"--chdir", dir, "list", "--json"}, &out, &bytes.Buffer{}); code != 0 {
		t.Fatalf("list --json: code=%d", code)
	}
	dec := json.NewDecoder(strings.NewReader(out.String()))
	sawMCP := false
	for dec.More() {
		var e listEntry
		if err := dec.Decode(&e); err != nil {
			t.Fatal(err)
		}
		if e.Channel == "mcpServers" {
			sawMCP = true
			// TestMain disables introspection so the toolset is empty in
			// this run; we just confirm the field is part of the JSON
			// surface.
			if e.ToolsetHash != "" {
				t.Errorf("expected empty toolsetHash with introspection disabled, got %q", e.ToolsetHash)
			}
		}
	}
	if !sawMCP {
		t.Fatalf("no mcpServers row in --json output: %q", out.String())
	}
	if !strings.Contains(out.String(), "toolsetHash") {
		t.Errorf("expected toolsetHash field in JSON output: %q", out.String())
	}
}

func TestList_HumanShowsUnverifiedForMCP(t *testing.T) {
	dir := newDemoRepo(t)
	if code := run([]string{"--chdir", dir, "lock"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("lock failed")
	}
	var out bytes.Buffer
	if code := run([]string{"--chdir", dir, "list"}, &out, &bytes.Buffer{}); code != 0 {
		t.Fatalf("list: code=%d", code)
	}
	if !strings.Contains(out.String(), "unverified") {
		t.Errorf("expected 'unverified' in list output, got: %q", out.String())
	}
}

func TestList_NoLockfile(t *testing.T) {
	dir := t.TempDir()
	var errOut bytes.Buffer
	code := run([]string{"--chdir", dir, "list"}, &bytes.Buffer{}, &errOut)
	if code != 1 {
		t.Errorf("list with no lockfile: want code=1, got %d", code)
	}
}
