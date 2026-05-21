package main

import (
	"bytes"
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
