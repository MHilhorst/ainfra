package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAudit_AE1 — fresh home with accumulated config, no ainfra: every
// detected item is unmanaged, footer suggests `ainfra adopt --scope=user`.
func TestAudit_AE1(t *testing.T) {
	home := t.TempDir()
	mkClaudeTree(t, home, map[string]string{
		".claude/skills/foo/SKILL.md":                  "# foo",
		".claude/skills/bar/SKILL.md":                  "# bar",
		".claude/plugins/myplugin/plugin.json":         "{}",
		".claude/commands/greet.md":                    "# greet",
		".claude/settings.json":                        `{"permissions":{"allow":["rg","ls"]}}`,
		".claude/agents/reviewer.md":                   "# reviewer",
	})
	dir := t.TempDir() // no .claude, no ainfra.yaml → Project section omitted

	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")

	out, _ := runAudit(t, dir)
	mustContain(t, out, "GLOBAL (~/.claude)")
	mustContain(t, out, "[unmanaged]")
	mustNotContain(t, out, "[managed]")
	mustContain(t, out, "ainfra adopt --scope=user")
	// Project section is omitted via the FooterNote, not rendered as section header.
}

// TestAudit_AE2 — a skill present in the lockfile sourced from a team
// manifest renders as [managed] with `from: …`. No separate "Team" section
// is rendered.
func TestAudit_AE2(t *testing.T) {
	home := t.TempDir()
	mkClaudeTree(t, home, map[string]string{
		".claude/skills/teamskill/SKILL.md": "# team-managed skill",
	})

	dir := t.TempDir()
	// Repo manifest declares an extends source so reconcile can surface it.
	writeFile(t, filepath.Join(dir, "ainfra.yaml"), "version: 1\nextends:\n  - source: github:org/team-config@1.2.0\n")
	writeFile(t, filepath.Join(dir, "ainfra.personal.lock"),
		"version: 1\nentries:\n  skills:\n    teamskill:\n      layer: team\n      contentHash: sha256:abc\n",
	)

	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")

	out, _ := runAudit(t, dir)
	mustContain(t, out, "teamskill")
	mustContain(t, out, "[managed]")
	mustContain(t, out, "from: github:org/team-config@1.2.0")
	// No separate Team layer section header.
	mustNotContain(t, out, "TEAM (")
}

// TestAudit_AE3 — run outside any repo: Global section only, Project
// section omitted with a one-line note.
func TestAudit_AE3(t *testing.T) {
	home := t.TempDir()
	mkClaudeTree(t, home, map[string]string{
		".claude/skills/lonely/SKILL.md": "# lonely",
	})
	dir := t.TempDir()

	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")

	// JSON mode is the easiest place to assert the omission note shape.
	out, _ := runAudit(t, dir, "--json")
	notes := decodeNotes(t, out)
	var omitted bool
	for _, n := range notes {
		if n.Layer == "project" && strings.Contains(n.Message, "omitted") {
			omitted = true
		}
	}
	if !omitted {
		t.Fatalf("expected a project-layer omission note; notes=%+v", notes)
	}
}

// TestAudit_AE4 — same id present in Global and Project: Global row tagged
// shadowed-by, Project row tagged normally.
func TestAudit_AE4(t *testing.T) {
	home := t.TempDir()
	mkClaudeTree(t, home, map[string]string{
		".claude/skills/shared/SKILL.md": "# shared (global)",
	})
	dir := t.TempDir()
	mkClaudeTree(t, dir, map[string]string{
		".claude/skills/shared/SKILL.md": "# shared (project)",
	})

	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")

	out, _ := runAudit(t, dir, "--json")
	rows := decodeRows(t, out)

	var globalShadowed, projectActive bool
	for _, r := range rows {
		if r.Channel == "skills" && r.ID == "shared" {
			if r.Layer == "global" && r.Status.Shadowed && r.ShadowedBy == "project" {
				globalShadowed = true
			}
			if r.Layer == "project" && !r.Status.Shadowed {
				projectActive = true
			}
		}
	}
	if !globalShadowed {
		t.Errorf("expected Global skills/shared to be shadowed-by project; rows=%+v", rows)
	}
	if !projectActive {
		t.Errorf("expected Project skills/shared to be active (non-shadowed); rows=%+v", rows)
	}
}

// TestAudit_AE5 — fully managed and current: footer renders the positive
// health line and no next-command suggestion.
func TestAudit_AE5(t *testing.T) {
	home := t.TempDir()
	mkClaudeTree(t, home, map[string]string{
		".claude/skills/clean/SKILL.md": "# clean",
	})
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "ainfra.yaml"), "version: 1\n")
	writeFile(t, filepath.Join(dir, "ainfra.personal.lock"),
		"version: 1\nentries:\n  skills:\n    clean:\n      layer: personal\n      contentHash: sha256:abc\n",
	)

	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")

	out, _ := runAudit(t, dir, "--json")
	footer := decodeFooter(t, out)
	if !footer.Healthy {
		t.Errorf("expected Healthy=true; footer=%+v", footer)
	}
	if footer.Suggested != "" {
		t.Errorf("expected no Suggested when healthy; got %q", footer.Suggested)
	}

	textOut, _ := runAudit(t, dir)
	mustContain(t, textOut, "all detected config is managed")
}

// TestAudit_AE6 — Project layer with both settings.json and
// settings.local.json: two distinct settings rows, .local.json is gitignored.
func TestAudit_AE6(t *testing.T) {
	home := t.TempDir()
	dir := t.TempDir()
	mkClaudeTree(t, dir, map[string]string{
		".claude/settings.json":       `{"permissions":{"allow":["rg"]}}`,
		".claude/settings.local.json": `{"permissions":{"allow":["echo","ls","cat"]}}`,
	})

	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")

	out, _ := runAudit(t, dir, "--json")
	rows := decodeRows(t, out)
	var sawSettings, sawLocal, localGitignored bool
	for _, r := range rows {
		if r.Layer != "project" || r.Channel != "settings" {
			continue
		}
		if r.ID == "settings.json" {
			sawSettings = true
		}
		if r.ID == "settings.local.json" {
			sawLocal = true
			if r.Status.Gitignored {
				localGitignored = true
			}
		}
	}
	if !sawSettings || !sawLocal {
		t.Errorf("expected both settings rows; rows=%+v", rows)
	}
	if !localGitignored {
		t.Errorf("expected settings.local.json row tagged gitignored")
	}
}

// TestAudit_FreshMachine — no ~/.claude, no project: exit 0, footer reports
// no-config-detected. Guards R2 (works without manifest, without claude).
func TestAudit_FreshMachine(t *testing.T) {
	home := t.TempDir() // no .claude inside
	dir := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")

	out, code := runAudit(t, dir, "--json")
	if code != 0 {
		t.Fatalf("audit on fresh machine: expected exit 0, got %d (%s)", code, out)
	}
	footer := decodeFooter(t, out)
	if !footer.NoConfigDetected {
		t.Errorf("expected NoConfigDetected=true on fresh machine; got %+v", footer)
	}
}

// TestAudit_UnknownFlag — bad flag exits non-zero.
func TestAudit_UnknownFlag(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	var out, errOut bytes.Buffer
	code := run([]string{"--chdir", dir, "audit", "--no-such-flag"}, &out, &errOut)
	if code == 0 {
		t.Fatalf("audit with unknown flag: expected non-zero, got 0; stderr=%q", errOut.String())
	}
}

// --- helpers -------------------------------------------------------------

// runAudit dispatches the audit command and returns combined stdout + the
// exit code. We attach extra flags after the base form.
func runAudit(t *testing.T, dir string, extra ...string) (string, int) {
	t.Helper()
	args := append([]string{"--chdir", dir, "audit"}, extra...)
	var out, errOut bytes.Buffer
	code := run(args, &out, &errOut)
	if code != 0 {
		t.Logf("audit stderr: %s", errOut.String())
	}
	return out.String(), code
}

// mkClaudeTree writes each {relPath: content} pair under root, creating
// parent directories as needed.
func mkClaudeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func mustContain(t *testing.T, s, sub string) {
	t.Helper()
	if !strings.Contains(s, sub) {
		t.Errorf("output missing %q; got:\n%s", sub, s)
	}
}

func mustNotContain(t *testing.T, s, sub string) {
	t.Helper()
	if strings.Contains(s, sub) {
		t.Errorf("output unexpectedly contains %q; got:\n%s", sub, s)
	}
}

// JSON helpers — decode the streamed output into our shapes.

type auditJSONRow struct {
	Layer      string `json:"layer"`
	Channel    string `json:"channel"`
	ID         string `json:"id"`
	Version    string `json:"version,omitempty"`
	Status     auditJSONStatus
	Source     string `json:"source,omitempty"`
	ShadowedBy string `json:"shadowedBy,omitempty"`
	Detail     string `json:"detail,omitempty"`
}

type auditJSONStatus struct {
	Managed    bool `json:"managed,omitempty"`
	Unmanaged  bool `json:"unmanaged,omitempty"`
	Shadowed   bool `json:"shadowed,omitempty"`
	Stale      bool `json:"stale,omitempty"`
	Drift      bool `json:"drift,omitempty"`
	Gitignored bool `json:"gitignored,omitempty"`
}

type auditJSONNote struct {
	Layer   string `json:"layer,omitempty"`
	Message string `json:"message"`
}

type auditJSONFooter struct {
	Adoptable        int    `json:"adoptable"`
	Stale            int    `json:"stale"`
	Drift            int    `json:"drift"`
	Suggested        string `json:"suggested,omitempty"`
	Healthy          bool   `json:"healthy"`
	NoConfigDetected bool   `json:"noConfigDetected,omitempty"`
}

func decodeRows(t *testing.T, out string) []auditJSONRow {
	t.Helper()
	var rows []auditJSONRow
	dec := json.NewDecoder(strings.NewReader(out))
	for dec.More() {
		var raw map[string]json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if _, isFooter := raw["footer"]; isFooter {
			continue
		}
		if _, isNote := raw["note"]; isNote {
			continue
		}
		var r auditJSONRow
		merged := mergeJSON(raw)
		if err := json.Unmarshal(merged, &r); err != nil {
			t.Fatalf("decode row: %v", err)
		}
		// Re-parse status from the inner field.
		if statusRaw, ok := raw["status"]; ok {
			if err := json.Unmarshal(statusRaw, &r.Status); err != nil {
				t.Fatalf("decode status: %v", err)
			}
		}
		rows = append(rows, r)
	}
	return rows
}

func decodeNotes(t *testing.T, out string) []auditJSONNote {
	t.Helper()
	var notes []auditJSONNote
	dec := json.NewDecoder(strings.NewReader(out))
	for dec.More() {
		var raw map[string]json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if noteRaw, ok := raw["note"]; ok {
			var n auditJSONNote
			if err := json.Unmarshal(noteRaw, &n); err != nil {
				t.Fatalf("decode note: %v", err)
			}
			notes = append(notes, n)
		}
	}
	return notes
}

func decodeFooter(t *testing.T, out string) auditJSONFooter {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(out))
	var footer auditJSONFooter
	for dec.More() {
		var raw map[string]json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if footerRaw, ok := raw["footer"]; ok {
			if err := json.Unmarshal(footerRaw, &footer); err != nil {
				t.Fatalf("decode footer: %v", err)
			}
		}
	}
	return footer
}

// mergeJSON re-serializes a map[string]json.RawMessage as a single JSON
// object, used to unmarshal into a typed struct while preserving the
// freedom to extract nested fields separately.
func mergeJSON(raw map[string]json.RawMessage) []byte {
	out, err := json.Marshal(raw)
	if err != nil {
		return []byte("{}")
	}
	return out
}
