package writer

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// helper: run AddEntry / RemoveEntry in-memory using the exported wrappers
// via tempfile, since the package-internal funcs are private but we want
// integration coverage.

func TestAddEntry_AppendsToExistingChannel(t *testing.T) {
	src := `version: 1

mcpServers:
  repo:
    transport: stdio
    command: npx
`
	got, err := addEntry([]byte(src), "mcpServers", "github",
		`transport: stdio
command: npx
version: "0.6.2"`)
	if err != nil {
		t.Fatalf("addEntry: %v", err)
	}
	want := `version: 1

mcpServers:
  repo:
    transport: stdio
    command: npx
  github:
    transport: stdio
    command: npx
    version: "0.6.2"
`
	if string(got) != want {
		t.Errorf("addEntry result mismatch.\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestAddEntry_PreservesCommentsOutsideEdit(t *testing.T) {
	src := `# Top-level comment with em-dash —
version: 1

# --- mcp servers block ---
mcpServers:
  # this one is important
  repo:
    transport: stdio
`
	got, err := addEntry([]byte(src), "mcpServers", "docs", `transport: stdio`)
	if err != nil {
		t.Fatalf("addEntry: %v", err)
	}
	for _, must := range []string{
		"# Top-level comment with em-dash —",
		"# --- mcp servers block ---",
		"# this one is important",
		"docs:",
	} {
		if !strings.Contains(string(got), must) {
			t.Errorf("addEntry stripped %q from output:\n%s", must, got)
		}
	}
}

func TestAddEntry_ChannelMissing_AppendsNewBlock(t *testing.T) {
	src := `version: 1

mcpServers:
  repo:
    transport: stdio
`
	got, err := addEntry([]byte(src), "hooks", "log",
		`event: PostToolUse
matcher: "Edit"
command: "echo edit"`)
	if err != nil {
		t.Fatalf("addEntry: %v", err)
	}
	if !strings.Contains(string(got), "hooks:") {
		t.Errorf("addEntry: expected new hooks: block, got:\n%s", got)
	}
	if !strings.Contains(string(got), "  log:") {
		t.Errorf("addEntry: expected log entry under hooks:, got:\n%s", got)
	}
	// Existing mcpServers block must be untouched.
	if !strings.Contains(string(got), "mcpServers:\n  repo:\n    transport: stdio") {
		t.Errorf("addEntry: existing block was modified:\n%s", got)
	}
}

func TestAddEntry_DuplicateErrors(t *testing.T) {
	src := `version: 1

mcpServers:
  github:
    transport: stdio
`
	_, err := addEntry([]byte(src), "mcpServers", "github", `transport: stdio`)
	if !errors.Is(err, ErrEntryExists) {
		t.Fatalf("want ErrEntryExists, got %v", err)
	}
}

func TestRemoveEntry_HappyPath(t *testing.T) {
	src := `version: 1

mcpServers:
  repo:
    transport: stdio
  github:
    transport: stdio
    version: "0.6.2"
  docs:
    transport: stdio
`
	got, err := removeEntry([]byte(src), "mcpServers", "github")
	if err != nil {
		t.Fatalf("removeEntry: %v", err)
	}
	if strings.Contains(string(got), "github:") {
		t.Errorf("removeEntry: github entry still present:\n%s", got)
	}
	for _, must := range []string{"repo:", "docs:", "  docs:", "  repo:"} {
		if !strings.Contains(string(got), must) {
			t.Errorf("removeEntry stripped %q (it should only remove github):\n%s", must, got)
		}
	}
}

func TestRemoveEntry_NotFound(t *testing.T) {
	src := `version: 1

mcpServers:
  repo:
    transport: stdio
`
	_, err := removeEntry([]byte(src), "mcpServers", "nope")
	if !errors.Is(err, ErrEntryNotFound) {
		t.Fatalf("want ErrEntryNotFound, got %v", err)
	}
}

func TestAddRemove_RoundTrip(t *testing.T) {
	src := `version: 1

mcpServers:
  repo:
    transport: stdio
`
	added, err := addEntry([]byte(src), "mcpServers", "github", `transport: stdio
command: npx
version: "0.6.2"`)
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	removed, err := removeEntry(added, "mcpServers", "github")
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	if string(removed) != src {
		t.Errorf("round-trip mismatch.\nwant:\n%s\ngot:\n%s", src, removed)
	}
}

func TestAddEntry_FilesystemRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ainfra.yaml")
	original := `version: 1

mcpServers:
  repo:
    transport: stdio
`
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := AddEntry(path, "mcpServers", "docs", `transport: stdio`); err != nil {
		t.Fatalf("AddEntry: %v", err)
	}
	out, _ := os.ReadFile(path)
	if !strings.Contains(string(out), "docs:") {
		t.Errorf("AddEntry: docs entry missing:\n%s", out)
	}
	if err := RemoveEntry(path, "mcpServers", "docs"); err != nil {
		t.Fatalf("RemoveEntry: %v", err)
	}
	out, _ = os.ReadFile(path)
	if string(out) != original {
		t.Errorf("add+remove did not restore original.\nwant:\n%s\ngot:\n%s", original, out)
	}
}

// Round-trip the repo's actual showcase manifest as a smoke test that the
// writer does not mangle real-world content (em-dashes, inline maps, varied
// styles).
func TestAddRemove_RealShowcaseManifest(t *testing.T) {
	repoRoot := findRepoRoot(t)
	src, err := os.ReadFile(filepath.Join(repoRoot, "ainfra.yaml"))
	if err != nil {
		t.Skipf("no showcase ainfra.yaml: %v", err)
	}
	added, err := addEntry(src, "mcpServers", "_test_only_entry",
		`transport: stdio
command: npx
version: "0.6.2"`)
	if err != nil {
		t.Fatalf("addEntry on showcase: %v", err)
	}
	removed, err := removeEntry(added, "mcpServers", "_test_only_entry")
	if err != nil {
		t.Fatalf("removeEntry on showcase: %v", err)
	}
	if string(removed) != string(src) {
		t.Errorf("showcase round-trip lost bytes. src=%d bytes, after add+remove=%d bytes",
			len(src), len(removed))
	}
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, _ := os.Getwd()
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Fatal("could not find repo root")
	return ""
}
