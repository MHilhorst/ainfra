package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MHilhorst/ainfra/internal/artifact"
)

func TestPublishNoPublishBlock(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte("version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Write a minimal lock file so the missing-lock check doesn't fire first.
	if err := os.WriteFile(filepath.Join(dir, "ainfra.lock"), []byte(`{"version":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	code := run([]string{"--chdir", dir, "publish"}, &out, &errOut)
	if code == 0 {
		t.Fatalf("publish without publish: block: expected non-zero exit, got 0")
	}
	combined := out.String() + errOut.String()
	if !strings.Contains(combined, "publish") {
		t.Errorf("publish without publish: block: expected 'publish' in error, got: %q", combined)
	}
}

func TestPublishNoLockFile(t *testing.T) {
	dir := t.TempDir()
	yaml := `version: 1
publish:
  artifactURL: https://example.com/artifact
  agent: claude
  sync:
    intervalMinutes: 60
    runAtLogin: true
`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	code := run([]string{"--chdir", dir, "publish"}, &out, &errOut)
	if code == 0 {
		t.Fatalf("publish without ainfra.lock: expected non-zero exit, got 0")
	}
	combined := out.String() + errOut.String()
	if !strings.Contains(combined, "ainfra lock") {
		t.Errorf("publish without ainfra.lock: expected 'ainfra lock' hint, got: %q", combined)
	}
}

func TestPublishSuccess(t *testing.T) {
	dir := t.TempDir()
	yaml := `version: 1
publish:
  artifactURL: https://example.com/artifact
  agent: claude
  sync:
    intervalMinutes: 60
    runAtLogin: true
`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	lockContent := `{"version":1,"entries":{}}`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.lock"), []byte(lockContent), 0o644); err != nil {
		t.Fatal(err)
	}

	outDir := filepath.Join(dir, "dist")
	var out, errOut bytes.Buffer
	code := run([]string{"--chdir", dir, "publish", "--out", outDir}, &out, &errOut)
	if code != 0 {
		t.Fatalf("publish: code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}

	// Check expected files exist.
	for _, name := range []string{"ainfra.lock", artifact.DescriptorName, artifact.ManifestName} {
		if _, err := os.Stat(filepath.Join(outDir, name)); err != nil {
			t.Errorf("publish: expected %s to exist: %v", name, err)
		}
	}

	// Verify artifact integrity.
	if err := artifact.Verify(outDir); err != nil {
		t.Errorf("publish: artifact.Verify failed: %v", err)
	}

	// Check stdout contains helpful output.
	if !strings.Contains(out.String(), outDir) {
		t.Errorf("publish: expected output dir in stdout, got: %q", out.String())
	}
}
