package provider

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppendAndReadHistoryRoundTrip(t *testing.T) {
	root := t.TempDir()
	got, err := ReadHistory(root)
	if err != nil {
		t.Fatalf("ReadHistory on missing: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("missing-file history must be empty, got %d events", len(got))
	}
	events := []HistoryEvent{
		{Command: "apply", Channel: "mcpServers", ID: "github", Kind: "create", Actor: "alice@example.com"},
		{Command: "apply", Channel: "skills", ID: "reviewer", Kind: "update"},
	}
	if err := AppendHistory(root, events); err != nil {
		t.Fatalf("AppendHistory: %v", err)
	}
	back, err := ReadHistory(root)
	if err != nil {
		t.Fatalf("ReadHistory: %v", err)
	}
	if len(back) != 2 {
		t.Fatalf("event count: got %d, want 2", len(back))
	}
	if back[0].Channel != "mcpServers" || back[0].ID != "github" || back[0].Kind != "create" {
		t.Errorf("first event lost fields: %+v", back[0])
	}
	if back[0].TS == "" {
		t.Error("first event missing auto-stamped TS")
	}
}

func TestAppendHistoryEmptyIsNoop(t *testing.T) {
	root := t.TempDir()
	if err := AppendHistory(root, nil); err != nil {
		t.Fatalf("AppendHistory(nil): %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".ainfra", HistoryFile)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("empty append created the file: %v", err)
	}
}

func TestAppendHistoryAppendsNotOverwrites(t *testing.T) {
	root := t.TempDir()
	if err := AppendHistory(root, []HistoryEvent{{Command: "apply", Channel: "a", ID: "1", Kind: "create"}}); err != nil {
		t.Fatal(err)
	}
	if err := AppendHistory(root, []HistoryEvent{{Command: "apply", Channel: "b", ID: "2", Kind: "update"}}); err != nil {
		t.Fatal(err)
	}
	all, err := ReadHistory(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 || all[0].ID != "1" || all[1].ID != "2" {
		t.Errorf("append order lost: %+v", all)
	}
}

func TestEventsFromResultsSkipsNoop(t *testing.T) {
	results := []ApplyResult{{
		Channel: "mcpServers",
		Applied: []Change{
			{Kind: ChangeCreate, ID: "a"},
			{Kind: ChangeNoop, ID: "b"},
			{Kind: ChangeUpdate, ID: "c"},
		},
	}}
	events := EventsFromResults(results, HistoryEvent{Command: "apply", Actor: "x"})
	if len(events) != 2 {
		t.Fatalf("event count: got %d, want 2", len(events))
	}
	if events[0].ID != "a" || events[0].Kind != "create" {
		t.Errorf("first event: %+v", events[0])
	}
	if events[1].ID != "c" || events[1].Kind != "update" {
		t.Errorf("second event: %+v", events[1])
	}
	for _, e := range events {
		if e.Channel != "mcpServers" || e.Actor != "x" {
			t.Errorf("base fields not copied: %+v", e)
		}
	}
}

func TestEventsFromResultsRecordsFailedAndSkipped(t *testing.T) {
	results := []ApplyResult{{
		Channel: "hooks",
		Failed:  []ChangeFailure{{Change: Change{Kind: ChangeCreate, ID: "f"}, Err: errors.New("boom")}},
		Skipped: []ChangeSkip{{Change: Change{Kind: ChangeUpdate, ID: "s"}, Reason: "blocked"}},
	}}
	events := EventsFromResults(results, HistoryEvent{Command: "apply"})
	if len(events) != 2 {
		t.Fatalf("event count: got %d, want 2", len(events))
	}
	kinds := []string{events[0].Kind, events[1].Kind}
	if !contains(kinds, "create-failed") || !contains(kinds, "update-skipped") {
		t.Errorf("kinds: %v", kinds)
	}
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

func TestReadHistoryIgnoresBlankLines(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".ainfra")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := strings.Join([]string{
		`{"ts":"2026-05-26T10:00:00Z","command":"apply","channel":"a","id":"1","kind":"create"}`,
		``,
		`   `,
		`{"ts":"2026-05-26T10:00:01Z","command":"apply","channel":"a","id":"2","kind":"update"}`,
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(dir, HistoryFile), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	events, err := ReadHistory(root)
	if err != nil {
		t.Fatalf("ReadHistory: %v", err)
	}
	if len(events) != 2 {
		t.Errorf("event count: got %d, want 2", len(events))
	}
}
