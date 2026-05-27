package adopt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScanEmptyRepo(t *testing.T) {
	dir := t.TempDir()
	m, ws, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if m.Version != 1 {
		t.Errorf("version=%d want 1", m.Version)
	}
	if m.MCPServers != nil || m.Hooks != nil || m.Commands != nil || m.Rules != nil || m.Secrets != nil {
		t.Errorf("expected empty channels, got %+v", m)
	}
	for _, w := range ws {
		if strings.Contains(w.Message, "stripped literal") {
			t.Errorf("unexpected strip warning on empty repo: %q", w.Message)
		}
	}
}

func TestScanMCPOnly(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".mcp.json"), `{
		"mcpServers": {
			"alpha": {"command": "node", "args": ["server.js"]},
			"beta":  {"type": "http", "url": "https://mcp.example.com/sse"}
		}
	}`)
	m, _, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m.MCPServers["alpha"]; !ok {
		t.Errorf("missing alpha: %+v", m.MCPServers)
	}
	if beta, ok := m.MCPServers["beta"]; !ok || beta.Transport != "http" || beta.URL == "" {
		t.Errorf("beta wrong: %+v", beta)
	}
}

func TestScanStripsLiteralCredential(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".mcp.json"), `{
		"mcpServers": {
			"github": {
				"command": "node",
				"args": ["server.js"],
				"env": {"GITHUB_TOKEN": "ghp_abcdefghijklmnopqrst"}
			}
		}
	}`)
	m, ws, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	srv := m.MCPServers["github"]
	if got := srv.Env["GITHUB_TOKEN"]; got != "${secrets.github-token}" {
		t.Errorf("env not stripped: %q", got)
	}
	if _, ok := m.Secrets["github-token"]; !ok {
		t.Errorf("secret not added: %+v", m.Secrets)
	}
	found := false
	for _, w := range ws {
		if strings.Contains(w.Message, "stripped literal credential") {
			found = true
		}
	}
	if !found {
		t.Errorf("no strip warning: %+v", ws)
	}
}

func TestScanPreservesTemplate(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".mcp.json"), `{
		"mcpServers": {
			"github": {
				"command": "node",
				"args": ["server.js"],
				"env": {"GITHUB_TOKEN": "${env:GH}"}
			}
		}
	}`)
	m, ws, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	srv := m.MCPServers["github"]
	if got := srv.Env["GITHUB_TOKEN"]; got != "${env:GH}" {
		t.Errorf("template not preserved: %q", got)
	}
	if len(m.Secrets) != 0 {
		t.Errorf("no secret should have been added: %+v", m.Secrets)
	}
	for _, w := range ws {
		if strings.Contains(w.Message, "stripped literal") {
			t.Errorf("unexpected strip warning: %q", w.Message)
		}
	}
}

func TestScanGenericPasswordKey(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".mcp.json"), `{
		"mcpServers": {
			"db": {
				"command": "node",
				"args": ["s.js"],
				"env": {"DB_PASSWORD": "hunter2"}
			}
		}
	}`)
	m, ws, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	srv := m.MCPServers["db"]
	if !strings.HasPrefix(srv.Env["DB_PASSWORD"], "${secrets.") {
		t.Errorf("password not stripped: %q", srv.Env["DB_PASSWORD"])
	}
	if len(m.Secrets) != 1 {
		t.Errorf("expected one secret: %+v", m.Secrets)
	}
	if len(ws) == 0 {
		t.Errorf("expected warning")
	}
}

func TestScanHooksFromSettings(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".claude", "settings.json"), `{
		"hooks": {
			"PostToolUse": [{
				"matcher": "Edit|Write",
				"hooks": [{"type": "command", "command": "gofmt -w .", "timeout": 5}]
			}]
		}
	}`)
	m, _, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Hooks) != 1 {
		t.Fatalf("expected 1 hook: %+v", m.Hooks)
	}
	for _, h := range m.Hooks {
		if h.Event != "PostToolUse" || h.Matcher != "Edit|Write" || h.Command != "gofmt -w ." || h.Timeout != 5000 {
			t.Errorf("hook wrong: %+v", h)
		}
	}
}

func TestScanCommands(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".claude", "commands", "foo.md"), "# foo\n")
	writeFile(t, filepath.Join(dir, ".claude", "commands", "bar.md"), "# bar\n")
	m, _, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Commands) != 2 {
		t.Fatalf("expected 2 commands, got %+v", m.Commands)
	}
	if got := m.Commands["foo"].Source; got != "./.claude/commands/foo.md" {
		t.Errorf("foo source: %q", got)
	}
}

func TestScanRulesFromClaudeMd(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "CLAUDE.md"), "# rules\n")
	m, _, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if r, ok := m.Rules["claude-md"]; !ok || r.Source != "./CLAUDE.md" {
		t.Errorf("CLAUDE.md rule missing: %+v", m.Rules)
	}
}

func TestScanNoClaudeDir(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".mcp.json"), `{"mcpServers": {"x": {"command":"node","args":["a"]}}}`)
	writeFile(t, filepath.Join(dir, "CLAUDE.md"), "# rules\n")
	m, _, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if m.Agent == "claude-code" {
		t.Errorf("should not set claude-code without .claude/ dir, got %q", m.Agent)
	}
	if len(m.MCPServers) != 1 || len(m.Rules) != 1 {
		t.Errorf("expected MCP + Rules only: %+v", m)
	}
}

func TestScanSimpleFixture(t *testing.T) {
	m, _, err := Scan("testdata/simple")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m.MCPServers["docs"]; !ok {
		t.Errorf("missing docs MCP server: %+v", m.MCPServers)
	}
	if _, ok := m.Commands["deploy"]; !ok {
		t.Errorf("missing deploy command: %+v", m.Commands)
	}
	if _, ok := m.Rules["claude-md"]; !ok {
		t.Errorf("missing CLAUDE.md rule: %+v", m.Rules)
	}
	if m.Agent != "claude-code" {
		t.Errorf("agent: got %q want claude-code", m.Agent)
	}
}

func TestScanCodexDetected(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".codex", "config.toml"), "")
	writeFile(t, filepath.Join(dir, ".mcp.json"), `{"mcpServers": {}}`)
	m, _, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if m.Agent != "codex" {
		t.Errorf("expected codex, got %q", m.Agent)
	}
}
