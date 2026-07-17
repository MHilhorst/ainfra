package provider

import (
	"path/filepath"
	"testing"
)

func TestReadOfferedMissingIsEmpty(t *testing.T) {
	fs := NewMemFilesystem()
	l, corrupt, err := ReadOffered(fs, "/nope/prune-offered.json")
	if err != nil {
		t.Fatalf("err = %v, want nil (a missing ledger is a first run)", err)
	}
	if corrupt {
		t.Error("corrupt = true, want false")
	}
	if len(l.Offered) != 0 {
		t.Errorf("Offered len = %d, want 0", len(l.Offered))
	}
}

func TestReadOfferedCorruptFailsOpen(t *testing.T) {
	fs := NewMemFilesystem()
	path := "/repo/.ainfra/prune-offered.json"
	if err := fs.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	l, corrupt, err := ReadOffered(fs, path)
	if err != nil {
		t.Fatalf("err = %v, want nil; a corrupt ledger must fail open", err)
	}
	if !corrupt {
		t.Error("corrupt = false, want true so the caller can warn")
	}
	if len(l.Offered) != 0 {
		t.Error("a corrupt ledger must read as empty, so everything is re-offered and nothing is deleted")
	}
}

func TestWriteReadOfferedRoundTrip(t *testing.T) {
	fs := NewMemFilesystem()
	path := "/repo/.ainfra/prune-offered.json"
	in := &OfferedLedger{
		Version: 1,
		Offered: map[string]OfferedEntry{
			"commands:ship": {FirstOfferedAt: "2026-07-17T11:04:22Z"},
		},
	}
	if err := WriteOffered(fs, path, in); err != nil {
		t.Fatal(err)
	}
	out, corrupt, err := ReadOffered(fs, path)
	if err != nil || corrupt {
		t.Fatalf("err = %v, corrupt = %v", err, corrupt)
	}
	got, ok := out.Offered["commands:ship"]
	if !ok {
		t.Fatal("commands:ship missing after round trip")
	}
	if got.FirstOfferedAt != "2026-07-17T11:04:22Z" {
		t.Errorf("FirstOfferedAt = %q, want 2026-07-17T11:04:22Z", got.FirstOfferedAt)
	}
}

func TestOfferedKey(t *testing.T) {
	if got := OfferedKey("commands", "ship"); got != "commands:ship" {
		t.Errorf("OfferedKey = %q, want commands:ship", got)
	}
}

func TestOfferedPath(t *testing.T) {
	want := filepath.Join("/repo", ".ainfra", "prune-offered.json")
	if got := OfferedPath("/repo"); got != want {
		t.Errorf("OfferedPath = %q, want %q", got, want)
	}
}
