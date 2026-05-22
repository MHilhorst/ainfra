package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLockWarnsOnUnautomatableCLITool(t *testing.T) {
	dir := t.TempDir()
	// `jq` installs via brew (automatable); `legacy-tool` declares only an
	// unrecognised method, so apply can only probe for it on PATH.
	yaml := "version: 1\n" +
		"cliTools:\n" +
		"  jq:\n" +
		"    install:\n" +
		"      brew:\n" +
		"        formula: jq\n" +
		"  legacy-tool:\n" +
		"    install:\n" +
		"      manual:\n" +
		"        url: https://example.com/legacy\n"
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	code := run([]string{"--chdir", dir, "lock"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("lock: code=%d out=%q err=%q", code, out.String(), errOut.String())
	}
	combined := out.String() + errOut.String()
	if !strings.Contains(combined, "legacy-tool") {
		t.Errorf("expected a warning naming 'legacy-tool', got: %q", combined)
	}
	if strings.Contains(combined, `"jq"`) {
		t.Errorf("jq installs via brew and must not be warned about, got: %q", combined)
	}
}

func TestLockWarnsOnCLIToolWithNoInstallBlock(t *testing.T) {
	dir := t.TempDir()
	yaml := "version: 1\n" +
		"cliTools:\n" +
		"  bare-tool: {}\n"
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	code := run([]string{"--chdir", dir, "lock"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("lock: code=%d out=%q err=%q", code, out.String(), errOut.String())
	}
	combined := out.String() + errOut.String()
	if !strings.Contains(combined, "bare-tool") {
		t.Errorf("expected a warning naming 'bare-tool', got: %q", combined)
	}
}
