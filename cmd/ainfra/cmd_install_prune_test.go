package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newPruneRepo builds a repo with an empty manifest and an isolated HOME, then
// locks it.
//
// Isolating HOME is not optional. --prune forces the user-scope orchestrator
// into existence, and the user-scope providers read $HOME/.claude/. Without
// this a test run on a developer's machine would observe their real slash
// commands and, once an offered ledger existed, delete them. TestMain isolates
// XDG_CONFIG_HOME for the same class of reason; HOME matters more here because
// the consequence is data loss rather than a leaked manifest.
func newPruneRepo(t *testing.T, userCommands ...string) (dir, home string) {
	t.Helper()
	dir = t.TempDir()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte("version: 1\nagent: claude-code\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmds := filepath.Join(home, ".claude", "commands")
	if err := os.MkdirAll(cmds, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, id := range userCommands {
		if err := os.WriteFile(filepath.Join(cmds, id+".md"), []byte("# "+id+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if code := run([]string{"--chdir", dir, "lock"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("lock failed")
	}
	return dir, home
}

func commandExists(home, id string) bool {
	_, err := os.Stat(filepath.Join(home, ".claude", "commands", id+".md"))
	return err == nil
}

// declarePersonal writes a personal manifest declaring ids, sourcing each from
// a file outside the pruned tree.
func declarePersonal(t *testing.T, home string, ids ...string) {
	t.Helper()
	src := t.TempDir()
	var b strings.Builder
	b.WriteString("version: 1\nagent: claude-code\ncommands:\n")
	for _, id := range ids {
		p := filepath.Join(src, id+".md")
		if err := os.WriteFile(p, []byte("# "+id+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		b.WriteString("  " + id + ":\n    source: " + p + "\n")
	}
	dir := filepath.Join(home, ".config", "ainfra")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "personal.yaml"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestInstallPruneFirstRunDeletesNothing is the acceptance test for the whole
// feature, using the real numbers from the machine that motivated it: eight
// undeclared slash commands, every one of them real work.
func TestInstallPruneFirstRunDeletesNothing(t *testing.T) {
	ids := []string{"dbaccess", "document", "monitor", "review-wip", "ship", "spin", "start", "stop"}
	dir, home := newPruneRepo(t, ids...)

	var out bytes.Buffer
	if code := run([]string{"--chdir", dir, "install", "--prune", "--yes"}, &out, &bytes.Buffer{}); code != 0 {
		t.Fatalf("code = %d, want 0\n%s", code, out.String())
	}

	for _, id := range ids {
		if !commandExists(home, id) {
			t.Errorf("%s was deleted on the first --prune; prune must never delete on first sight", id)
		}
	}
	if !strings.Contains(out.String(), "Not declared in ainfra") {
		t.Errorf("first run must report undeclared entries; got:\n%s", out.String())
	}
	for _, id := range ids {
		if !strings.Contains(out.String(), id) {
			t.Errorf("%s not named in the report; the user cannot declare what they were not shown", id)
		}
	}
}

// After the user declares what they want, a second run removes only the rest.
func TestInstallPruneSecondRunRemovesOnlyUndeclared(t *testing.T) {
	dir, home := newPruneRepo(t, "ship", "cruft")

	if code := run([]string{"--chdir", dir, "install", "--prune", "--yes"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("first run failed")
	}
	declarePersonal(t, home, "ship")
	if code := run([]string{"--chdir", dir, "lock"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("re-lock failed")
	}

	var out bytes.Buffer
	if code := run([]string{"--chdir", dir, "install", "--prune", "--yes"}, &out, &bytes.Buffer{}); code != 0 {
		t.Fatalf("second run: code=%d\n%s", code, out.String())
	}

	if !commandExists(home, "ship") {
		t.Error("ship was pruned despite being declared")
	}
	if commandExists(home, "cruft") {
		t.Error("cruft survived a second --prune despite staying undeclared")
	}
}

// A backup is written for anything removed, so a mistaken prune is recoverable.
func TestInstallPruneBacksUpWhatItRemoves(t *testing.T) {
	dir, home := newPruneRepo(t, "cruft")

	if code := run([]string{"--chdir", dir, "install", "--prune", "--yes"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("first run failed")
	}
	if code := run([]string{"--chdir", dir, "install", "--prune", "--yes"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("second run failed")
	}
	if commandExists(home, "cruft") {
		t.Fatal("cruft not removed; the rest of this test is meaningless")
	}

	matches, err := filepath.Glob(filepath.Join(home, ".ainfra", "pruned-*", "commands", "cruft.md"))
	if err != nil || len(matches) == 0 {
		t.Fatalf("no backup for the removed command: %v", err)
	}
	data, err := os.ReadFile(matches[0])
	if err != nil || len(data) == 0 {
		t.Errorf("backup is empty (%q, %v); a backup that loses the content is worse than none", data, err)
	}
}

// A preview must not arm a deletion: otherwise --dry-run --prune followed by a
// real --prune would delete on what the user experienced as the first run.
func TestInstallPruneDryRunDoesNotArm(t *testing.T) {
	dir, home := newPruneRepo(t, "cruft")

	if code := run([]string{"--chdir", dir, "install", "--prune", "--dry-run"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("dry run failed")
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "ainfra", "prune-offered.json")); err == nil {
		t.Error("--dry-run wrote the offered ledger; a preview must not arm a deletion")
	}

	if code := run([]string{"--chdir", dir, "install", "--prune", "--yes"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("real run failed")
	}
	if !commandExists(home, "cruft") {
		t.Error("cruft deleted on the first real run because --dry-run armed it")
	}
}

// The offer must be recorded even when there is nothing else to apply,
// otherwise it is re-offered forever and prune can never remove anything.
func TestInstallPruneRecordsOfferWithEmptyPlan(t *testing.T) {
	dir, home := newPruneRepo(t, "cruft")

	if code := run([]string{"--chdir", dir, "install", "--prune", "--yes"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("first run failed")
	}

	raw, err := os.ReadFile(filepath.Join(home, ".config", "ainfra", "prune-offered.json"))
	if err != nil {
		t.Fatalf("offered ledger not written: %v", err)
	}
	var l struct {
		Offered map[string]any `json:"offered"`
	}
	if err := json.Unmarshal(raw, &l); err != nil {
		t.Fatal(err)
	}
	if _, ok := l.Offered["commands:cruft"]; !ok {
		t.Errorf("commands:cruft not recorded: %s", raw)
	}
}

// The headline safety property: a normal install never prunes.
func TestInstallWithoutPruneLeavesUndeclaredAlone(t *testing.T) {
	dir, home := newPruneRepo(t, "ship")

	// Run twice: even a machine that has seen many installs must keep it.
	for i := 0; i < 2; i++ {
		if code := run([]string{"--chdir", dir, "install", "--yes"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
			t.Fatal("install failed")
		}
	}
	if !commandExists(home, "ship") {
		t.Error("ship was removed without --prune")
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "ainfra", "prune-offered.json")); err == nil {
		t.Error("a non-prune install wrote the offered ledger")
	}
}

// --prune with --from has no manifest to declare against, so it must fail
// rather than silently no-op and let the user believe they were shown their
// undeclared config.
func TestInstallPruneWithFromIsRejected(t *testing.T) {
	dir, _ := newPruneRepo(t)

	var errOut bytes.Buffer
	code := run([]string{"--chdir", dir, "install", "--prune", "--from", "/nonexistent"}, &bytes.Buffer{}, &errOut)
	if code == 0 {
		t.Error("--prune --from exited 0; it must be rejected")
	}
	if !strings.Contains(errOut.String(), "not supported with --from") {
		t.Errorf("error should explain the rejection, got %q", errOut.String())
	}
}
