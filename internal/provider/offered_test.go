package provider

import (
	"strings"
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

func TestOfferedPathIsOutsideTheRepo(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/xdg")

	// Regression: a ledger inside the repo would be committed (ainfra never
	// git-ignores .ainfra/), so a teammate would inherit a list of entries they
	// were never shown and their first --prune would delete on first sight.
	got, err := OfferedPathRepo("/repo", "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(got, "/repo") {
		t.Errorf("OfferedPathRepo = %q; the ledger must never live inside the repo", got)
	}
	if !strings.HasPrefix(got, "/xdg") {
		t.Errorf("OfferedPathRepo = %q, want it under XDG_CONFIG_HOME", got)
	}
}

// A different agent gets a different ledger, mirroring the applied ledger.
// Otherwise an --agent codex run, whose provider set has no Pruner, rewrites
// the Claude Code ledger to empty and prune can never converge.
func TestOfferedPathIsAgentScoped(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/xdg")

	claude, _ := OfferedPathRepo("/repo", "claude-code")
	defaulted, _ := OfferedPathRepo("/repo", "")
	codex, _ := OfferedPathRepo("/repo", "codex")

	if claude != defaulted {
		t.Errorf("claude-code = %q, default = %q; want the same file", claude, defaulted)
	}
	if codex == claude {
		t.Errorf("codex and claude-code share %q; a codex run would wipe the Claude ledger", codex)
	}

	u1, _ := OfferedPathUser("claude-code")
	u2, _ := OfferedPathUser("codex")
	if u1 == u2 {
		t.Errorf("user-scope ledgers collide across agents: %q", u1)
	}
}
