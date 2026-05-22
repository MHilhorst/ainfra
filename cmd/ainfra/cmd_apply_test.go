package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyYesWritesFile(t *testing.T) {
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
	code := run([]string{"--chdir", dir, "apply", "--yes"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("apply --yes: code=%d out=%q err=%q", code, out.String(), errOut.String())
	}

	// Expect the command file to be written.
	dest := filepath.Join(dir, ".claude", "commands", "hello.md")
	if _, err := os.Stat(dest); err != nil {
		t.Errorf("apply --yes: expected %s to be written, got: %v", dest, err)
	}

	// Applied ledger should exist.
	ledger := filepath.Join(dir, ".ainfra", "applied.lock")
	if _, err := os.Stat(ledger); err != nil {
		t.Errorf("apply --yes: expected applied ledger at %s, got: %v", ledger, err)
	}
}

func TestApplySecondRunNothingToDo(t *testing.T) {
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
	if code := run([]string{"--chdir", dir, "apply", "--yes"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatal("first apply failed")
	}

	var out, errOut bytes.Buffer
	code := run([]string{"--chdir", dir, "apply", "--yes"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("second apply: code=%d out=%q err=%q", code, out.String(), errOut.String())
	}
	combined := out.String() + errOut.String()
	if !strings.Contains(combined, "Nothing to do") {
		t.Errorf("second apply: expected 'Nothing to do', got: %q", combined)
	}
}

func TestApplyDryRun(t *testing.T) {
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
	code := run([]string{"--chdir", dir, "apply", "--dry-run"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("apply --dry-run: code=%d out=%q err=%q", code, out.String(), errOut.String())
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
		t.Errorf("apply --dry-run: expected 'Dry run' in output, got: %q", out.String())
	}
}

func TestApplyNoInstall(t *testing.T) {
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
	code := run([]string{"--chdir", dir, "apply", "--yes", "--no-install"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("apply --yes --no-install: code=%d out=%q err=%q", code, out.String(), errOut.String())
	}

	// The file-writing channels still reconcile.
	dest := filepath.Join(dir, ".claude", "commands", "hello.md")
	if _, err := os.Stat(dest); err != nil {
		t.Errorf("apply --no-install: expected %s to be written, got: %v", dest, err)
	}
}

func TestApplyWithoutNoInstallFailsOnAbsentTool(t *testing.T) {
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
	code := run([]string{"--chdir", dir, "apply", "--yes"}, &out, &errOut)
	if code == 0 {
		t.Fatalf("apply --yes (no --no-install): expected non-zero exit for an absent tool, got 0; out=%q err=%q",
			out.String(), errOut.String())
	}
}

func TestApplyNoLockFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte("version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	code := run([]string{"--chdir", dir, "apply", "--yes"}, &out, &errOut)
	if code == 0 {
		t.Fatal("apply without lock: expected non-zero exit, got 0")
	}
	combined := out.String() + errOut.String()
	if !strings.Contains(combined, "ainfra lock") {
		t.Errorf("apply without lock: expected 'ainfra lock' hint, got: %q", combined)
	}
}

func TestApplyPrintsSummary(t *testing.T) {
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
	code := run([]string{"--chdir", dir, "apply", "--yes"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("apply --yes: code=%d out=%q err=%q", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "applied 1, skipped 0, failed 0") {
		t.Errorf("expected an apply summary line, got: %q", out.String())
	}
}

func TestApplyFailureListsFailedResource(t *testing.T) {
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
	code := run([]string{"--chdir", dir, "apply", "--yes"}, &out, &errOut)
	if code == 0 {
		t.Fatalf("apply of an absent tool: expected non-zero exit, got 0; out=%q", out.String())
	}
	combined := out.String() + errOut.String()
	if !strings.Contains(combined, "failed 1") {
		t.Errorf("expected 'failed 1' in the summary, got: %q", combined)
	}
	if !strings.Contains(combined, "ainfra-absent-tool-xyz") {
		t.Errorf("expected the failed resource id in the output, got: %q", combined)
	}
}
