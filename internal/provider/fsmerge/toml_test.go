package fsmerge

import (
	"strings"
	"testing"
)

func TestMergeTOMLTablesCreatesFile(t *testing.T) {
	fs := newMemFS()
	err := MergeTOMLTables(fs, "/config.toml", "mcp_servers",
		map[string]any{"github": map[string]any{"command": "npx"}},
		[]string{"github"})
	if err != nil {
		t.Fatal(err)
	}
	out := string(fs.files["/config.toml"])
	if !strings.Contains(out, "[mcp_servers.github]") {
		t.Errorf("expected [mcp_servers.github] table, got:\n%s", out)
	}
	if !strings.Contains(out, `command = "npx"`) {
		t.Errorf("expected command key, got:\n%s", out)
	}
}

func TestMergeTOMLTablesPreservesForeignContent(t *testing.T) {
	fs := newMemFS()
	fs.files["/config.toml"] = []byte(
		"model = \"gpt-5\"\n\n[mcp_servers.other]\ncommand = \"other-cmd\"\n\n[mcp_servers.old]\ncommand = \"old-cmd\"\n")

	err := MergeTOMLTables(fs, "/config.toml", "mcp_servers",
		map[string]any{"github": map[string]any{"command": "npx"}},
		[]string{"old", "github"}) // owned: old (removed), github (set)
	if err != nil {
		t.Fatal(err)
	}
	out := string(fs.files["/config.toml"])
	if !strings.Contains(out, `model = "gpt-5"`) {
		t.Errorf("foreign top-level key 'model' was dropped:\n%s", out)
	}
	if !strings.Contains(out, "[mcp_servers.other]") {
		t.Errorf("foreign server 'other' was dropped:\n%s", out)
	}
	if !strings.Contains(out, "[mcp_servers.github]") {
		t.Errorf("desired server 'github' missing:\n%s", out)
	}
	if strings.Contains(out, "[mcp_servers.old]") {
		t.Errorf("owned-but-undesired server 'old' should have been removed:\n%s", out)
	}
}

func TestMergeTOMLTablesRejectsMalformed(t *testing.T) {
	fs := newMemFS()
	fs.files["/config.toml"] = []byte("this is = = not valid toml [[[")
	err := MergeTOMLTables(fs, "/config.toml", "mcp_servers",
		map[string]any{"x": map[string]any{}}, []string{"x"})
	if err == nil {
		t.Error("expected an error for malformed TOML, got nil")
	}
}
