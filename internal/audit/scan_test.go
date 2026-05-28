package audit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScanSkills_DetectsDirectoryWithSkillMd(t *testing.T) {
	root := t.TempDir()
	mkfile(t, filepath.Join(root, "skills", "foo", "SKILL.md"), "# foo")
	mkdir(t, filepath.Join(root, "skills", "bar"))

	rows := scanSkills(LayerGlobal, root)
	if len(rows) != 2 {
		t.Fatalf("expected 2 skill rows, got %d (%+v)", len(rows), rows)
	}
	var foo, bar Row
	for _, r := range rows {
		if r.ID == "foo" {
			foo = r
		}
		if r.ID == "bar" {
			bar = r
		}
	}
	if foo.Detail != "SKILL.md" {
		t.Errorf("expected foo Detail to surface SKILL.md, got %q", foo.Detail)
	}
	if bar.Detail != "" {
		t.Errorf("expected bar Detail empty (no SKILL.md), got %q", bar.Detail)
	}
}

func TestScanPlugins_EmitsOneRowPerDir(t *testing.T) {
	root := t.TempDir()
	mkdir(t, filepath.Join(root, "plugins", "alpha"))
	mkdir(t, filepath.Join(root, "plugins", "beta"))
	mkfile(t, filepath.Join(root, "plugins", "loose.md"), "ignored")

	rows := scanPlugins(LayerProject, root)
	if len(rows) != 2 {
		t.Fatalf("expected 2 plugin rows (alpha, beta), got %d (%+v)", len(rows), rows)
	}
}

func TestScanAgents_SupportsFlatMdAndAgentMdDir(t *testing.T) {
	root := t.TempDir()
	mkfile(t, filepath.Join(root, "agents", "flat.md"), "# flat")
	mkfile(t, filepath.Join(root, "agents", "nested", "agent.md"), "# nested")
	mkdir(t, filepath.Join(root, "agents", "junk")) // no agent.md → not surfaced

	rows := scanAgents(LayerGlobal, root)
	ids := map[string]bool{}
	for _, r := range rows {
		ids[r.ID] = true
	}
	if !ids["flat"] || !ids["nested"] {
		t.Errorf("expected flat and nested agents, got %+v", ids)
	}
	if ids["junk"] {
		t.Errorf("junk directory without agent.md should not be surfaced")
	}
}

func TestScanSettings_LocalJsonOnlyAtProjectLayer(t *testing.T) {
	root := t.TempDir()
	mkfile(t, filepath.Join(root, "settings.json"), `{"permissions":{"allow":["rg"]}}`)
	mkfile(t, filepath.Join(root, "settings.local.json"), `{"permissions":{"allow":["echo"]}}`)

	globalRows := scanSettings(LayerGlobal, root)
	if len(globalRows) != 1 || globalRows[0].ID != "settings.json" {
		t.Fatalf("global layer should expose only settings.json, got %+v", globalRows)
	}
	if globalRows[0].Status.Gitignored {
		t.Errorf("global settings.json should not be tagged gitignored")
	}

	projectRows := scanSettings(LayerProject, root)
	if len(projectRows) != 2 {
		t.Fatalf("project layer should expose both settings files, got %+v", projectRows)
	}
	var local Row
	for _, r := range projectRows {
		if r.ID == "settings.local.json" {
			local = r
		}
	}
	if !local.Status.Gitignored {
		t.Errorf("settings.local.json should be tagged gitignored")
	}
}

func TestScanSettings_DetailSummarizesNotableFields(t *testing.T) {
	root := t.TempDir()
	mkfile(t, filepath.Join(root, "settings.json"),
		`{"permissions":{"allow":["a","b","c"],"deny":["x"]},"hooks":{"pre":[]},"model":"claude-opus-4-7","env":{"K":"V"}}`,
	)
	rows := scanSettings(LayerGlobal, root)
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	d := rows[0].Detail
	if !strings.Contains(d, "4 permissions") {
		t.Errorf("expected permission count in Detail; got %q", d)
	}
	if !strings.Contains(d, "hooks block") {
		t.Errorf("expected 'hooks block' in Detail; got %q", d)
	}
	if !strings.Contains(d, "model override") {
		t.Errorf("expected 'model override' in Detail; got %q", d)
	}
	if !strings.Contains(d, "env var") {
		t.Errorf("expected env var mention in Detail; got %q", d)
	}
}

// helpers ---------------------------------------------------------------

func mkfile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func mkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
}
