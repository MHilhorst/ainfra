package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateAcceptsAValidManifest(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte("version: 1\n"), 0o644)
	var out bytes.Buffer
	code := run([]string{"--chdir", dir, "validate"}, &out, &bytes.Buffer{})
	if code != 0 || !strings.Contains(out.String(), "valid") {
		t.Errorf("validate valid: code=%d out=%q", code, out.String())
	}
}

func TestValidateReportsADiagnosticBlock(t *testing.T) {
	dir := t.TempDir()
	bad := "version: 1\nmcpServers:\n  s:\n    command: npx\n"
	os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(bad), 0o644)
	var errOut bytes.Buffer
	code := run([]string{"--chdir", dir, "validate"}, &bytes.Buffer{}, &errOut)
	if code != 1 {
		t.Fatalf("validate invalid: code=%d, want 1", code)
	}
	s := errOut.String()
	for _, want := range []string{"Error:", "pin an exact version", "ainfra.yaml, mcpServers.s"} {
		if !strings.Contains(s, want) {
			t.Errorf("diagnostic missing %q\n---\n%s", want, s)
		}
	}
}

func TestValidateReportsMissingManifest(t *testing.T) {
	dir := t.TempDir()
	var errOut bytes.Buffer
	code := run([]string{"--chdir", dir, "validate"}, &bytes.Buffer{}, &errOut)
	if code != 1 || !strings.Contains(errOut.String(), "Error:") {
		t.Errorf("missing manifest: code=%d err=%q", code, errOut.String())
	}
}
