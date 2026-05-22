package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallerNoPublishBlock(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte("version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	code := run([]string{"--chdir", dir, "installer"}, &out, &errOut)
	if code == 0 {
		t.Fatalf("installer without publish: block: expected non-zero exit, got 0")
	}
	combined := out.String() + errOut.String()
	if !strings.Contains(combined, "publish") {
		t.Errorf("installer without publish: block: expected 'publish' in error, got: %q", combined)
	}
}

func TestInstallerSuccess(t *testing.T) {
	dir := t.TempDir()
	yaml := `version: 1
publish:
  artifactURL: https://example.com/artifact
  agent: claude-desktop
  sync:
    intervalMinutes: 60
    runAtLogin: true
`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	outFile := filepath.Join(dir, "ainfra-install.command")
	var out, errOut bytes.Buffer
	code := run([]string{"--chdir", dir, "installer", "--out", outFile}, &out, &errOut)
	if code != 0 {
		t.Fatalf("installer: code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}

	// Output file must exist.
	info, err := os.Stat(outFile)
	if err != nil {
		t.Fatalf("installer: output file not written: %v", err)
	}

	// Must be executable.
	if info.Mode()&0o111 == 0 {
		t.Errorf("installer: output file is not executable, mode=%v", info.Mode())
	}

	// Must be non-empty.
	if info.Size() == 0 {
		t.Error("installer: output file is empty")
	}

	// Must contain the artifact URL.
	raw, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("installer: cannot read output file: %v", err)
	}
	if !strings.Contains(string(raw), "https://example.com/artifact") {
		t.Errorf("installer: output file missing artifact URL, got: %q", string(raw))
	}

	// Stdout must mention the output path.
	if !strings.Contains(out.String(), outFile) {
		t.Errorf("installer: expected output path in stdout, got: %q", out.String())
	}
}
