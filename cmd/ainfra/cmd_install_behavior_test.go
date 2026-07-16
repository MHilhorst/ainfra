package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallYesWritesFile(t *testing.T) {
	dir := t.TempDir()

	// Write a command source file.
	srcContent := "# Hello command\n"
	if err := os.WriteFile(filepath.Join(dir, "hello.md"), []byte(srcContent), 0o644); err != nil {
		t.Fatal(err)
	}
	yaml := "version: 1\ncommands:\n  hello:\n    source: hello.md\n"
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	// Lock first.
	if code := run([]string{"--chdir", dir, "lock"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("lock failed")
	}

	var out, errOut bytes.Buffer
	code := run([]string{"--chdir", dir, "install", "--yes"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("install --yes: code=%d out=%q err=%q", code, out.String(), errOut.String())
	}

	// Expect the command file to be written.
	dest := filepath.Join(dir, ".claude", "commands", "hello.md")
	if _, err := os.Stat(dest); err != nil {
		t.Errorf("install --yes: expected %s to be written, got: %v", dest, err)
	}

	// Applied ledger should exist.
	ledger := filepath.Join(dir, ".ainfra", "applied.lock")
	if _, err := os.Stat(ledger); err != nil {
		t.Errorf("install --yes: expected applied ledger at %s, got: %v", ledger, err)
	}
}

func TestInstallSecondRunNothingToDo(t *testing.T) {
	dir := t.TempDir()

	srcContent := "# Hello command\n"
	if err := os.WriteFile(filepath.Join(dir, "hello.md"), []byte(srcContent), 0o644); err != nil {
		t.Fatal(err)
	}
	yaml := "version: 1\ncommands:\n  hello:\n    source: hello.md\n"
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	if code := run([]string{"--chdir", dir, "lock"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("lock failed")
	}
	if code := run([]string{"--chdir", dir, "install", "--yes"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("first apply failed")
	}

	var out, errOut bytes.Buffer
	code := run([]string{"--chdir", dir, "install", "--yes"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("second apply: code=%d out=%q err=%q", code, out.String(), errOut.String())
	}
	combined := out.String() + errOut.String()
	if !strings.Contains(combined, "Nothing to do") {
		t.Errorf("second apply: expected 'Nothing to do', got: %q", combined)
	}
}

func TestInstallDryRun(t *testing.T) {
	dir := t.TempDir()

	srcContent := "# Hello command\n"
	if err := os.WriteFile(filepath.Join(dir, "hello.md"), []byte(srcContent), 0o644); err != nil {
		t.Fatal(err)
	}
	yaml := "version: 1\ncommands:\n  hello:\n    source: hello.md\n"
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	if code := run([]string{"--chdir", dir, "lock"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("lock failed")
	}

	var out, errOut bytes.Buffer
	code := run([]string{"--chdir", dir, "install", "--dry-run"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("install --dry-run: code=%d out=%q err=%q", code, out.String(), errOut.String())
	}

	// The command file must NOT be written.
	dest := filepath.Join(dir, ".claude", "commands", "hello.md")
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Errorf("apply --dry-run wrote %s; want no write (stat err = %v)", dest, err)
	}
	// The applied ledger must NOT be written.
	ledger := filepath.Join(dir, ".ainfra", "applied.lock")
	if _, err := os.Stat(ledger); !os.IsNotExist(err) {
		t.Errorf("apply --dry-run wrote the applied ledger; want no write (stat err = %v)", err)
	}
	// Output names it a dry run.
	if !strings.Contains(out.String(), "Dry run") {
		t.Errorf("install --dry-run: expected 'Dry run' in output, got: %q", out.String())
	}
}

func TestInstallAgentOverrideKeepsLedgersSeparate(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)

	if err := os.WriteFile(filepath.Join(dir, "claude.md"), []byte("Claude team rules.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "codex.md"), []byte("Codex team rules.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	yaml := `version: 1
rules:
  claude-team:
    target: CLAUDE.md
    source: claude.md
    agents: [claude-code]
  codex-team:
    target: AGENTS.md
    source: codex.md
    agents: [codex]
`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"--chdir", dir, "lock"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("lock failed")
	}
	if code := run([]string{"--chdir", dir, "install", "--yes"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("claude install failed")
	}

	var codexOut, codexErr bytes.Buffer
	code := run([]string{"--chdir", dir, "install", "--agent", "codex", "--dry-run"}, &codexOut, &codexErr)
	if code != 0 {
		t.Fatalf("codex dry-run: code=%d out=%q err=%q", code, codexOut.String(), codexErr.String())
	}
	combined := codexOut.String() + codexErr.String()
	if strings.Contains(combined, "claude-team") {
		t.Fatalf("codex dry-run planned against Claude-owned rule: %q", combined)
	}
	if !strings.Contains(combined, "codex-team") {
		t.Fatalf("codex dry-run should plan the Codex rule: %q", combined)
	}

	if code := run([]string{"--chdir", dir, "install", "--agent", "codex", "--yes"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("codex install failed")
	}
	if _, err := os.Stat(filepath.Join(dir, ".ainfra", "applied.lock")); err != nil {
		t.Fatalf("claude applied ledger missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".ainfra", "applied.codex.lock")); err != nil {
		t.Fatalf("codex applied ledger missing: %v", err)
	}

	var claudeOut, claudeErr bytes.Buffer
	code = run([]string{"--chdir", dir, "install", "--dry-run"}, &claudeOut, &claudeErr)
	if code != 0 {
		t.Fatalf("claude dry-run: code=%d out=%q err=%q", code, claudeOut.String(), claudeErr.String())
	}
	if combined := claudeOut.String() + claudeErr.String(); strings.Contains(combined, "codex-team") {
		t.Fatalf("claude dry-run planned against Codex-owned rule: %q", combined)
	}
}

func TestInstallNoInstall(t *testing.T) {
	dir := t.TempDir()

	srcContent := "# Hello command\n"
	if err := os.WriteFile(filepath.Join(dir, "hello.md"), []byte(srcContent), 0o644); err != nil {
		t.Fatal(err)
	}
	// A CLI tool whose binary is absent and whose only install method is
	// unrecognised. Without --no-install the cliTools channel (applied before
	// commands) fails the declare-and-check probe and aborts the apply.
	yaml := "version: 1\n" +
		"cliTools:\n" +
		"  ainfra-absent-tool-xyz:\n" +
		"    install:\n" +
		"      manual: {}\n" +
		"commands:\n" +
		"  hello:\n" +
		"    source: hello.md\n"
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	if code := run([]string{"--chdir", dir, "lock"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("lock failed")
	}

	var out, errOut bytes.Buffer
	code := run([]string{"--chdir", dir, "install", "--yes", "--no-install"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("apply --yes --no-install: code=%d out=%q err=%q", code, out.String(), errOut.String())
	}

	// The file-writing channels still reconcile.
	dest := filepath.Join(dir, ".claude", "commands", "hello.md")
	if _, err := os.Stat(dest); err != nil {
		t.Errorf("install --no-install: expected %s to be written, got: %v", dest, err)
	}
}

func TestInstallWithoutNoInstallFailsOnAbsentTool(t *testing.T) {
	dir := t.TempDir()

	yaml := "version: 1\n" +
		"cliTools:\n" +
		"  ainfra-absent-tool-xyz:\n" +
		"    install:\n" +
		"      manual: {}\n"
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	if code := run([]string{"--chdir", dir, "lock"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("lock failed")
	}

	var out, errOut bytes.Buffer
	code := run([]string{"--chdir", dir, "install", "--yes"}, &out, &errOut)
	if code == 0 {
		t.Fatalf("apply --yes (no --no-install): expected non-zero exit for an absent tool, got 0; out=%q err=%q",
			out.String(), errOut.String())
	}
}

func TestInstallNoLockFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte("version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	code := run([]string{"--chdir", dir, "install", "--yes"}, &out, &errOut)
	if code == 0 {
		t.Fatal("install without lock: expected non-zero exit, got 0")
	}
	combined := out.String() + errOut.String()
	if !strings.Contains(combined, "ainfra lock") {
		t.Errorf("install without lock: expected 'ainfra lock' hint, got: %q", combined)
	}
}

func TestInstallPrintsSummary(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "hello.md"), []byte("# hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	yaml := "version: 1\ncommands:\n  hello:\n    source: hello.md\n"
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"--chdir", dir, "lock"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("lock failed")
	}

	var out, errOut bytes.Buffer
	code := run([]string{"--chdir", dir, "install", "--yes"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("install --yes: code=%d out=%q err=%q", code, out.String(), errOut.String())
	}
	// 1 command + the auto-emitted SessionStart staleness hook.
	if !strings.Contains(out.String(), "Applied 2 changes") {
		t.Errorf("expected an apply summary line, got: %q", out.String())
	}
}

func TestInstallFailureListsFailedResource(t *testing.T) {
	dir := t.TempDir()

	// A CLI tool whose binary is absent and whose only install method is
	// unrecognised — its cliTools entry fails the declare-and-check probe.
	yaml := "version: 1\n" +
		"cliTools:\n" +
		"  ainfra-absent-tool-xyz:\n" +
		"    install:\n" +
		"      manual: {}\n"
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"--chdir", dir, "lock"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("lock failed")
	}

	var out, errOut bytes.Buffer
	code := run([]string{"--chdir", dir, "install", "--yes"}, &out, &errOut)
	if code == 0 {
		t.Fatalf("install of an absent tool: expected non-zero exit, got 0; out=%q", out.String())
	}
	combined := out.String() + errOut.String()
	if !strings.Contains(combined, "failed 1") {
		t.Errorf("expected 'failed 1' in the summary, got: %q", combined)
	}
	if !strings.Contains(combined, "ainfra-absent-tool-xyz") {
		t.Errorf("expected the failed resource id in the output, got: %q", combined)
	}
}
