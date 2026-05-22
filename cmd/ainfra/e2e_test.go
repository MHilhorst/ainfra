package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestE2EReconciliation exercises the full lock -> plan -> apply -> check -> plan
// cycle against a real temp directory. It uses a minimal manifest with one hook
// and one command (local source file) to keep the fixture small while covering
// the two most common channel types.
func TestE2EReconciliation(t *testing.T) {
	dir := t.TempDir()

	// Write a local source file for the command.
	cmdContent := "# greet\nSay hello to the user by name.\n"
	if err := os.WriteFile(filepath.Join(dir, "greet.md"), []byte(cmdContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// Write a minimal manifest: one hook + one command.
	manifest := `version: 1
hooks:
  on-session-start:
    event: SessionStart
    command: echo "session started"
    timeout: 3000
commands:
  greet:
    source: greet.md
    description: Greet the user by name.
`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	// Step 1: lock — must succeed and write ainfra.lock.
	{
		var out, errOut bytes.Buffer
		code := run([]string{"--chdir", dir, "lock"}, &out, &errOut)
		if code != 0 {
			t.Fatalf("lock: code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
		}
		if _, err := os.Stat(filepath.Join(dir, "ainfra.lock")); err != nil {
			t.Fatalf("lock: ainfra.lock not written: %v", err)
		}
	}

	// Step 2: plan — must succeed and show pending changes.
	{
		var out, errOut bytes.Buffer
		code := run([]string{"--chdir", dir, "plan"}, &out, &errOut)
		if code != 0 {
			t.Fatalf("plan (before apply): code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
		}
		combined := out.String() + errOut.String()
		if strings.Contains(combined, "No changes") {
			t.Errorf("plan (before apply): expected pending changes, got 'No changes': %q", combined)
		}
		// Expect at least one "to add" in the summary.
		if !strings.Contains(combined, "to add") {
			t.Errorf("plan (before apply): expected 'to add' in output, got: %q", combined)
		}
	}

	// Step 3: apply --yes — must succeed and write the artifacts.
	{
		var out, errOut bytes.Buffer
		code := run([]string{"--chdir", dir, "apply", "--yes"}, &out, &errOut)
		if code != 0 {
			t.Fatalf("apply --yes: code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
		}

		// Command file must exist under .claude/commands/.
		cmdFile := filepath.Join(dir, ".claude", "commands", "greet.md")
		if _, err := os.Stat(cmdFile); err != nil {
			t.Errorf("apply --yes: command file not written at %s: %v", cmdFile, err)
		} else {
			raw, err := os.ReadFile(cmdFile)
			if err != nil {
				t.Errorf("apply --yes: cannot read command file: %v", err)
			} else if string(raw) != cmdContent {
				t.Errorf("apply --yes: command file content = %q, want %q", string(raw), cmdContent)
			}
		}

		// Hook must be written into .claude/settings.json.
		settingsFile := filepath.Join(dir, ".claude", "settings.json")
		if _, err := os.Stat(settingsFile); err != nil {
			t.Errorf("apply --yes: settings.json not written at %s: %v", settingsFile, err)
		} else {
			raw, err := os.ReadFile(settingsFile)
			if err != nil {
				t.Errorf("apply --yes: cannot read settings.json: %v", err)
			} else if !strings.Contains(string(raw), "on-session-start") {
				t.Errorf("apply --yes: settings.json does not contain hook 'on-session-start': %q", string(raw))
			}
		}

		// Applied ledger must exist.
		ledger := filepath.Join(dir, ".ainfra", "applied.lock")
		if _, err := os.Stat(ledger); err != nil {
			t.Errorf("apply --yes: applied ledger not written at %s: %v", ledger, err)
		}
	}

	// Step 4: check — must exit 0 (no drift).
	{
		var out, errOut bytes.Buffer
		code := run([]string{"--chdir", dir, "check"}, &out, &errOut)
		if code != 0 {
			t.Fatalf("check (after apply): code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
		}
		combined := out.String() + errOut.String()
		if !strings.Contains(combined, "No drift") {
			t.Errorf("check (after apply): expected 'No drift', got: %q", combined)
		}
	}

	// Step 5: second plan — must show no changes.
	{
		var out, errOut bytes.Buffer
		code := run([]string{"--chdir", dir, "plan"}, &out, &errOut)
		if code != 0 {
			t.Fatalf("plan (after apply): code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
		}
		combined := out.String() + errOut.String()
		if !strings.Contains(combined, "No changes") {
			t.Errorf("plan (after apply): expected 'No changes', got: %q", combined)
		}
	}
}

// TestE2EToolsChannel exercises the tools channel end-to-end: apply writes
// disabledTools and permissions into settings.json, check exits 0 (no drift),
// and a second plan shows no changes. This is the regression guard for the
// resource-ID mismatch (Bug 1) and the []string disabledTools drop (Bug 2).
func TestE2EToolsChannel(t *testing.T) {
	dir := t.TempDir()

	manifest := `version: 1
tools:
  builtins:
    disabled:
      - WebSearch
  permissions:
    allow:
      - "Read(*)"
`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	// Step 1: lock.
	{
		var out, errOut bytes.Buffer
		code := run([]string{"--chdir", dir, "lock"}, &out, &errOut)
		if code != 0 {
			t.Fatalf("lock: code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
		}
	}

	// Step 2: plan — must report pending changes.
	{
		var out, errOut bytes.Buffer
		code := run([]string{"--chdir", dir, "plan"}, &out, &errOut)
		if code != 0 {
			t.Fatalf("plan (before apply): code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
		}
		combined := out.String() + errOut.String()
		if strings.Contains(combined, "No changes") {
			t.Errorf("plan (before apply): expected pending changes, got 'No changes': %q", combined)
		}
	}

	// Step 3: apply --yes — must write disabledTools and permissions.
	{
		var out, errOut bytes.Buffer
		code := run([]string{"--chdir", dir, "apply", "--yes"}, &out, &errOut)
		if code != 0 {
			t.Fatalf("apply --yes: code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
		}

		settingsFile := filepath.Join(dir, ".claude", "settings.json")
		raw, err := os.ReadFile(settingsFile)
		if err != nil {
			t.Fatalf("apply --yes: cannot read settings.json: %v", err)
		}

		var doc map[string]any
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("apply --yes: settings.json not valid JSON: %v", err)
		}

		// disabledTools must contain "WebSearch".
		dt, ok := doc["disabledTools"]
		if !ok {
			t.Errorf("apply --yes: disabledTools missing from settings.json: %s", string(raw))
		} else {
			arr, ok := dt.([]any)
			if !ok {
				t.Errorf("apply --yes: disabledTools is %T, want []any", dt)
			} else {
				found := false
				for _, v := range arr {
					if s, ok := v.(string); ok && s == "WebSearch" {
						found = true
					}
				}
				if !found {
					t.Errorf("apply --yes: disabledTools does not contain 'WebSearch': %v", arr)
				}
			}
		}

		// permissions.allow must contain "Read(*)".
		perms, ok := doc["permissions"].(map[string]any)
		if !ok {
			t.Errorf("apply --yes: permissions missing or wrong type in settings.json: %s", string(raw))
		} else {
			allow, _ := perms["allow"].([]any)
			found := false
			for _, v := range allow {
				if s, ok := v.(string); ok && s == "Read(*)" {
					found = true
				}
			}
			if !found {
				t.Errorf("apply --yes: permissions.allow does not contain 'Read(*)': %v", allow)
			}
		}
	}

	// Step 4: check — must exit 0 (no drift). This is the regression guard for Bug 1.
	{
		var out, errOut bytes.Buffer
		code := run([]string{"--chdir", dir, "check"}, &out, &errOut)
		if code != 0 {
			t.Fatalf("check (after apply): code=%d stdout=%q stderr=%q\n(non-zero exit means tools channel still reports drift — resource ID mismatch not fully fixed)",
				code, out.String(), errOut.String())
		}
		combined := out.String() + errOut.String()
		if !strings.Contains(combined, "No drift") {
			t.Errorf("check (after apply): expected 'No drift', got: %q", combined)
		}
	}

	// Step 5: second plan — must show no changes.
	{
		var out, errOut bytes.Buffer
		code := run([]string{"--chdir", dir, "plan"}, &out, &errOut)
		if code != 0 {
			t.Fatalf("plan (after apply): code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
		}
		combined := out.String() + errOut.String()
		if !strings.Contains(combined, "No changes") {
			t.Errorf("plan (after apply): expected 'No changes', got: %q", combined)
		}
	}
}

// TestE2ECodexReconciliation exercises the full lock -> plan -> apply -> check ->
// plan cycle for an `agent: codex` manifest, verifying the Codex provider set
// reconciles MCP servers into ~/.codex/config.toml and rules into AGENTS.md.
// HOME is redirected to a temp dir so the real ~/.codex is never touched.
func TestE2ECodexReconciliation(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)

	// Local source file for the rule.
	if err := os.WriteFile(filepath.Join(dir, "team.md"), []byte("Follow the team conventions.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	manifest := `version: 1
agent: codex
mcpServers:
  filesystem:
    transport: stdio
    command: npx
    args: ["-y", "@modelcontextprotocol/server-filesystem", "."]
    version: "0.6.2"
rules:
  team-conventions:
    source: team.md
`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	// Step 1: lock.
	{
		var out, errOut bytes.Buffer
		code := run([]string{"--chdir", dir, "lock"}, &out, &errOut)
		if code != 0 {
			t.Fatalf("lock: code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
		}
	}

	// Step 2: plan — must report pending changes.
	{
		var out, errOut bytes.Buffer
		code := run([]string{"--chdir", dir, "plan"}, &out, &errOut)
		if code != 0 {
			t.Fatalf("plan (before apply): code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
		}
		combined := out.String() + errOut.String()
		if !strings.Contains(combined, "to add") {
			t.Errorf("plan (before apply): expected 'to add', got: %q", combined)
		}
	}

	// Step 3: apply --yes — must write config.toml and AGENTS.md.
	{
		var out, errOut bytes.Buffer
		code := run([]string{"--chdir", dir, "apply", "--yes"}, &out, &errOut)
		if code != 0 {
			t.Fatalf("apply --yes: code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
		}

		configFile := filepath.Join(home, ".codex", "config.toml")
		raw, err := os.ReadFile(configFile)
		if err != nil {
			t.Fatalf("apply --yes: config.toml not written at %s: %v", configFile, err)
		}
		if !strings.Contains(string(raw), "[mcp_servers.filesystem]") {
			t.Errorf("apply --yes: config.toml missing [mcp_servers.filesystem]: %q", string(raw))
		}

		agentsFile := filepath.Join(dir, "AGENTS.md")
		rawA, err := os.ReadFile(agentsFile)
		if err != nil {
			t.Fatalf("apply --yes: AGENTS.md not written at %s: %v", agentsFile, err)
		}
		if !strings.Contains(string(rawA), "<!-- ainfra:rule team-conventions -->") {
			t.Errorf("apply --yes: AGENTS.md missing rule marker: %q", string(rawA))
		}
		if !strings.Contains(string(rawA), "Follow the team conventions.") {
			t.Errorf("apply --yes: AGENTS.md missing rule content: %q", string(rawA))
		}
	}

	// Step 4: check — must exit 0 (no drift).
	{
		var out, errOut bytes.Buffer
		code := run([]string{"--chdir", dir, "check"}, &out, &errOut)
		if code != 0 {
			t.Fatalf("check (after apply): code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
		}
		combined := out.String() + errOut.String()
		if !strings.Contains(combined, "No drift") {
			t.Errorf("check (after apply): expected 'No drift', got: %q", combined)
		}
	}

	// Step 5: second plan — must show no changes (idempotence).
	{
		var out, errOut bytes.Buffer
		code := run([]string{"--chdir", dir, "plan"}, &out, &errOut)
		if code != 0 {
			t.Fatalf("plan (after apply): code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
		}
		combined := out.String() + errOut.String()
		if !strings.Contains(combined, "No changes") {
			t.Errorf("plan (after apply): expected 'No changes', got: %q", combined)
		}
	}
}
