package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Install is lock-consuming, not lock-writing: only `ainfra lock`, `update`,
// and `add` may rewrite ainfra.lock / ainfra.personal.lock. Before this
// guarantee, every `ainfra install` re-resolved and rewrote both lockfiles,
// stamping a fresh generatedAt and folding environment-sensitive MCP
// introspection results into the committed lock — so every install produced
// working-tree churn that contradicted the lock-changes-are-PR-only model.
func TestInstallDoesNotRewriteLockfiles(t *testing.T) {
	dir := t.TempDir()

	manifest := `version: 1
hooks:
  on-session-start:
    event: SessionStart
    command: echo "session started"
    timeout: 3000
`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	{
		var out, errOut bytes.Buffer
		if code := run([]string{"--chdir", dir, "lock"}, &out, &errOut); code != 0 {
			t.Fatalf("lock: code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
		}
	}

	lockBefore, err := os.ReadFile(filepath.Join(dir, "ainfra.lock"))
	if err != nil {
		t.Fatal(err)
	}
	personalBefore, err := os.ReadFile(filepath.Join(dir, "ainfra.personal.lock"))
	if err != nil {
		t.Fatal(err)
	}

	// Change the manifest after locking. Install must still apply the new
	// state (front-page reconcile semantics) without refreshing the stale
	// lock — that refresh is `ainfra update`'s job, done in a PR.
	manifest += `  on-prompt:
    event: UserPromptSubmit
    command: echo "prompt"
    timeout: 2000
`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	{
		var out, errOut bytes.Buffer
		if code := run([]string{"--chdir", dir, "install", "--dry-run"}, &out, &errOut); code != 0 {
			t.Fatalf("install --dry-run: code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
		}
	}
	{
		var out, errOut bytes.Buffer
		if code := run([]string{"--chdir", dir, "install", "--yes"}, &out, &errOut); code != 0 {
			t.Fatalf("install --yes: code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
		}
	}

	lockAfter, err := os.ReadFile(filepath.Join(dir, "ainfra.lock"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(lockBefore, lockAfter) {
		t.Errorf("install rewrote ainfra.lock\nbefore:\n%s\nafter:\n%s", lockBefore, lockAfter)
	}
	personalAfter, err := os.ReadFile(filepath.Join(dir, "ainfra.personal.lock"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(personalBefore, personalAfter) {
		t.Errorf("install rewrote ainfra.personal.lock\nbefore:\n%s\nafter:\n%s", personalBefore, personalAfter)
	}

	// The manifest addition must still have been applied.
	raw, err := os.ReadFile(filepath.Join(dir, ".claude", "settings.json"))
	if err != nil {
		t.Fatalf("settings.json not written: %v", err)
	}
	if !strings.Contains(string(raw), "UserPromptSubmit") {
		t.Errorf("install did not apply the manifest change added after lock: %q", raw)
	}
}

// A re-lock that produces identical content must not churn generatedAt, so a
// no-op `ainfra update` leaves a clean working tree.
func TestLockPreservesGeneratedAtWhenUnchanged(t *testing.T) {
	dir := t.TempDir()

	manifest := `version: 1
hooks:
  on-session-start:
    event: SessionStart
    command: echo "session started"
    timeout: 3000
`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	{
		var out, errOut bytes.Buffer
		if code := run([]string{"--chdir", dir, "lock"}, &out, &errOut); code != 0 {
			t.Fatalf("lock: code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
		}
	}

	// Backdate generatedAt so a rewrite-with-fresh-timestamp is detectable
	// regardless of clock granularity.
	lockPath := filepath.Join(dir, "ainfra.lock")
	raw, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	const backdated = "2001-01-01T00:00:00Z"
	lines := strings.Split(string(raw), "\n")
	replaced := false
	for i, l := range lines {
		if strings.HasPrefix(l, "generatedAt:") {
			lines[i] = `generatedAt: "` + backdated + `"`
			replaced = true
		}
	}
	if !replaced {
		t.Fatalf("no generatedAt line in ainfra.lock: %q", raw)
	}
	if err := os.WriteFile(lockPath, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}

	{
		var out, errOut bytes.Buffer
		if code := run([]string{"--chdir", dir, "lock"}, &out, &errOut); code != 0 {
			t.Fatalf("re-lock: code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
		}
	}

	after, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(after), backdated) {
		t.Errorf("no-op re-lock churned generatedAt:\n%s", after)
	}
}
