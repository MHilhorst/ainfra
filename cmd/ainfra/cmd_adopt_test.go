package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAdoptEmptyRepo(t *testing.T) {
	dir := t.TempDir()
	var out, errOut bytes.Buffer
	code := run([]string{"--chdir", dir, "init", "--adopt"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("adopt: code=%d err=%q", code, errOut.String())
	}
	data, err := os.ReadFile(filepath.Join(dir, "ainfra.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "version: 1") {
		t.Errorf("missing version: %s", data)
	}
	if !strings.Contains(out.String(), "Next:") {
		t.Errorf("missing Next hint: %s", out.String())
	}
}

func TestAdoptMCPFromFixture(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(`{
		"mcpServers": {
			"alpha": {"type":"http","url":"https://a.example.com"},
			"beta":  {"type":"http","url":"https://b.example.com"}
		}
	}`), 0o644)
	var out, errOut bytes.Buffer
	code := run([]string{"--chdir", dir, "init", "--adopt"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("adopt: code=%d err=%q", code, errOut.String())
	}
	data, _ := os.ReadFile(filepath.Join(dir, "ainfra.yaml"))
	for _, want := range []string{"mcpServers:", "alpha:", "beta:", "https://a.example.com", "https://b.example.com"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("missing %q\n%s", want, data)
		}
	}
}

func TestAdoptStripsCredentialAndWarns(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(`{
		"mcpServers": {
			"github": {
				"type": "http",
				"url": "https://api.github.com",
				"headers": {"Authorization": "Bearer ghp_abcdefghijklmnopqrst"}
			}
		}
	}`), 0o644)
	var out, errOut bytes.Buffer
	code := run([]string{"--chdir", dir, "init", "--adopt"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("adopt: code=%d err=%q", code, errOut.String())
	}
	data, _ := os.ReadFile(filepath.Join(dir, "ainfra.yaml"))
	if strings.Contains(string(data), "ghp_abcdef") {
		t.Errorf("literal credential leaked into manifest:\n%s", data)
	}
	if !strings.Contains(string(data), "secrets:") {
		t.Errorf("expected synthesized secret block:\n%s", data)
	}
	if !strings.Contains(errOut.String(), "stripped literal credential") {
		t.Errorf("missing strip warning: %s", errOut.String())
	}
}

func TestAdoptRefusesToOverwriteWithoutFlag(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte("version: 1\n"), 0o644)
	var errOut bytes.Buffer
	code := run([]string{"--chdir", dir, "init", "--adopt"}, &bytes.Buffer{}, &errOut)
	if code != 1 {
		t.Fatalf("expected exit 1, got %d", code)
	}
	if !strings.Contains(errOut.String(), "exists") {
		t.Errorf("missing refusal: %s", errOut.String())
	}
}

func TestAdoptForceOverwrites(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte("OLD\n"), 0o644)
	os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(`{"mcpServers":{"x":{"type":"http","url":"https://x"}}}`), 0o644)
	code := run([]string{"--chdir", dir, "init", "--adopt", "--force"}, &bytes.Buffer{}, &bytes.Buffer{})
	if code != 0 {
		t.Fatalf("force: code=%d", code)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "ainfra.yaml"))
	if strings.Contains(string(data), "OLD") {
		t.Errorf("not overwritten:\n%s", data)
	}
	if !strings.Contains(string(data), "x:") {
		t.Errorf("scanned content missing:\n%s", data)
	}
}

func TestAdoptMergeAddsNewKeys(t *testing.T) {
	dir := t.TempDir()
	existing := "version: 1\nmcpServers:\n  existing:\n    transport: http\n    url: https://existing.example.com\n"
	os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(existing), 0o644)
	os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(`{
		"mcpServers": {
			"existing": {"type":"http","url":"https://different.example.com"},
			"newone":   {"type":"http","url":"https://new.example.com"}
		}
	}`), 0o644)
	var errOut bytes.Buffer
	code := run([]string{"--chdir", dir, "init", "--adopt", "--merge"}, &bytes.Buffer{}, &errOut)
	if code != 0 {
		t.Fatalf("merge: code=%d err=%q", code, errOut.String())
	}
	data, _ := os.ReadFile(filepath.Join(dir, "ainfra.yaml"))
	s := string(data)
	if !strings.Contains(s, "https://existing.example.com") {
		t.Errorf("existing key was overwritten:\n%s", s)
	}
	if strings.Contains(s, "https://different.example.com") {
		t.Errorf("scanned overwrote existing key:\n%s", s)
	}
	if !strings.Contains(s, "newone:") {
		t.Errorf("new key not added:\n%s", s)
	}
	if !strings.Contains(errOut.String(), "adding mcpServers.newone") {
		t.Errorf("missing add warning: %s", errOut.String())
	}
}

func TestAdoptCommandsAndRules(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".claude", "commands"), 0o755)
	os.WriteFile(filepath.Join(dir, ".claude", "commands", "deploy.md"), []byte("# deploy"), 0o644)
	os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("rules"), 0o644)
	code := run([]string{"--chdir", dir, "init", "--adopt"}, &bytes.Buffer{}, &bytes.Buffer{})
	if code != 0 {
		t.Fatalf("adopt: code=%d", code)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "ainfra.yaml"))
	for _, want := range []string{"commands:", "deploy:", "rules:", "CLAUDE.md"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("missing %q in:\n%s", want, data)
		}
	}
}


func TestAdoptOutputValidates(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(`{
		"mcpServers": {"alpha": {"type":"http","url":"https://x"}}
	}`), 0o644)
	os.MkdirAll(filepath.Join(dir, ".claude", "commands"), 0o755)
	os.WriteFile(filepath.Join(dir, ".claude", "commands", "foo.md"), []byte("# foo"), 0o644)
	if code := run([]string{"--chdir", dir, "init", "--adopt"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatalf("adopt: code=%d", code)
	}
	if code := run([]string{"--chdir", dir, "lock"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatalf("lock: code=%d", code)
	}
	var validateErr bytes.Buffer
	if code := run([]string{"--chdir", dir, "install", "--dry-run"}, &bytes.Buffer{}, &validateErr); code != 0 {
		data, _ := os.ReadFile(filepath.Join(dir, "ainfra.yaml"))
		t.Fatalf("install --dry-run failed: code=%d err=%q\n--- ainfra.yaml ---\n%s", code, validateErr.String(), data)
	}
}
