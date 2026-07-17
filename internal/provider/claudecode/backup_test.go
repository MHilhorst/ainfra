package claudecode_test

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/MHilhorst/ainfra/internal/provider"
	"github.com/MHilhorst/ainfra/internal/provider/claudecode"
)

// TestPrunerImplementations pins the pruneable set to the type system. A
// channel is pruneable only if its provider implements Pruner, so quietly
// adding Backup to an excluded provider must fail here rather than silently
// widen what --prune can delete.
func TestPrunerImplementations(t *testing.T) {
	cases := []struct {
		p    provider.Provider
		want bool
	}{
		{claudecode.MCP{}, true},
		{claudecode.Skills{}, true},
		{claudecode.Commands{}, true},
		// hooks: Observe is ledger-sourced, so observed == prior and the
		// untracked set is always empty. A prune would be a silent no-op.
		{claudecode.Hooks{}, false},
		// rules: Observe spans the repo and $HOME; deferred.
		{claudecode.Rules{}, false},
		// tools would uninstall brew packages; services would tear down the
		// prod-DB tunnels; plugins and marketplaces are installed artifacts.
		{claudecode.Tools{}, false},
		{claudecode.Services{}, false},
		{claudecode.Plugins{}, false},
		{claudecode.Marketplaces{}, false},
	}
	for _, c := range cases {
		_, ok := c.p.(provider.Pruner)
		if ok != c.want {
			t.Errorf("%s implements Pruner = %v, want %v", c.p.Channel(), ok, c.want)
		}
	}
}

func TestSkillsBackupRoundTrip(t *testing.T) {
	mem := provider.NewMemFilesystem()
	env := provider.Env{FS: mem, Root: "/repo"}

	// Mirror what fsmerge.WriteOwnedFile does on a real filesystem: it
	// MkdirAlls each file's parent, so nested dirs are recorded and visible to
	// ReadDir. Without that the fake FS hides "refs" and the test would pass a
	// recursive copy that never recursed.
	skill := filepath.Join("/repo", ".claude", "skills", "stray")
	if err := mem.MkdirAll(skill, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := mem.WriteFile(filepath.Join(skill, "SKILL.md"), []byte("hand written"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := mem.MkdirAll(filepath.Join(skill, "refs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := mem.WriteFile(filepath.Join(skill, "refs", "x.md"), []byte("nested"), 0o644); err != nil {
		t.Fatal(err)
	}

	dir := "/repo/.ainfra/pruned-x"
	if err := (claudecode.Skills{}).Backup(env, provider.Resource{ID: "stray", Channel: "skills"}, dir); err != nil {
		t.Fatal(err)
	}

	got, err := mem.ReadFile(filepath.Join(dir, "skills", "stray", "SKILL.md"))
	if err != nil || string(got) != "hand written" {
		t.Errorf("SKILL.md = %q, %v; want %q", got, err, "hand written")
	}
	// A skill bundle may nest files; a flat copy would silently drop them from
	// the backup of a directory that is about to be removed.
	nested, err := mem.ReadFile(filepath.Join(dir, "skills", "stray", "refs", "x.md"))
	if err != nil || string(nested) != "nested" {
		t.Errorf("refs/x.md = %q, %v; want %q", nested, err, "nested")
	}
}

func TestCommandsBackupRoundTrip(t *testing.T) {
	mem := provider.NewMemFilesystem()
	env := provider.Env{FS: mem, Root: "/repo"}

	path := filepath.Join("/repo", ".claude", "commands", "ship.md")
	if err := mem.WriteFile(path, []byte("# ship"), 0o644); err != nil {
		t.Fatal(err)
	}

	dir := "/repo/.ainfra/pruned-x"
	if err := (claudecode.Commands{}).Backup(env, provider.Resource{ID: "ship", Channel: "commands"}, dir); err != nil {
		t.Fatal(err)
	}

	got, err := mem.ReadFile(filepath.Join(dir, "commands", "ship.md"))
	if err != nil || string(got) != "# ship" {
		t.Errorf("ship.md = %q, %v; want %q", got, err, "# ship")
	}
}

// TestMCPBackupWritesFragmentNotEmptyFile is the regression test for the trap
// that Observe does not populate Payload: a backup that serialized
// Resource.Payload would write an empty object while reporting success, losing
// exactly the data it claims to protect.
func TestMCPBackupWritesFragmentNotEmptyFile(t *testing.T) {
	mem := provider.NewMemFilesystem()
	env := provider.Env{FS: mem, Root: "/repo"}

	doc := `{"mcpServers":{"stray":{"command":"npx","args":["stray-mcp"]}}}`
	if err := mem.WriteFile(filepath.Join("/repo", ".mcp.json"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}

	dir := "/repo/.ainfra/pruned-x"
	if err := (claudecode.MCP{}).Backup(env, provider.Resource{ID: "stray", Channel: "mcpServers"}, dir); err != nil {
		t.Fatal(err)
	}

	raw, err := mem.ReadFile(filepath.Join(dir, "mcpServers", "stray.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got["command"] != "npx" {
		t.Errorf("backup = %s; want the server's real entry, not an empty object", raw)
	}
}

// A backup that cannot find its source must error, never report success. The
// orchestrator cancels the delete on a backup error, so a silent success here
// would delete an un-backed-up resource.
func TestMCPBackupMissingEntryErrors(t *testing.T) {
	mem := provider.NewMemFilesystem()
	env := provider.Env{FS: mem, Root: "/repo"}

	if err := mem.WriteFile(filepath.Join("/repo", ".mcp.json"), []byte(`{"mcpServers":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	err := (claudecode.MCP{}).Backup(env, provider.Resource{ID: "ghost", Channel: "mcpServers"}, "/repo/.ainfra/pruned-x")
	if err == nil {
		t.Error("Backup of a missing entry returned nil; it must error so the delete is cancelled")
	}
}
