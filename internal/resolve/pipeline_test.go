package resolve

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLockPipelineOnMultiDBExample(t *testing.T) {
	dir := t.TempDir()
	manifestYAML := `version: 1
cliTools:
  ssh: { versionConstraint: ">=8.0" }
templates:
  tun:
    params: { host: { type: string, required: true } }
    resolved: { tunnelPort: { kind: allocated-port } }
    produces:
      mcpServer:
        command: npx
        version: "1.0.0"
        env: { P: "${resolved.tunnelPort}" }
        requires: [ { service: "${instance.id}-tunnel" } ]
      backgroundService:
        id: "${instance.id}-tunnel"
        kind: ssh-tunnel
        requires: [ { cliTool: ssh } ]
mcpServers:
  db-a: { template: tun, params: { host: a.example } }
  db-b: { template: tun, params: { host: b.example } }
`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(manifestYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RunLock(dir); err != nil {
		t.Fatalf("RunLock: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "ainfra.lock"))
	if err != nil {
		t.Fatalf("lock not written: %v", err)
	}
	out := string(data)
	for _, want := range []string{"db-a", "db-b", "tunnelPort: 13306", "tunnelPort: 13307", "contentHash:"} {
		if !strings.Contains(out, want) {
			t.Errorf("lock missing %q\n---\n%s", want, out)
		}
	}
}

func TestLockPipelineResolvesHooksAndCommands(t *testing.T) {
	dir := t.TempDir()
	manifestYAML := `version: 1
cliTools:
  node: { versionConstraint: ">=20" }
hooks:
  guard-branch:
    event: PreToolUse
    matcher: "Edit|Write"
    command: node .ainfra/run/guard.js
    requires: [ { cliTool: node } ]
commands:
  ship:
    source: ./commands/ship.md
    description: Fast-path merge to the default branch.
`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(manifestYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RunLock(dir); err != nil {
		t.Fatalf("RunLock: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "ainfra.lock"))
	if err != nil {
		t.Fatalf("lock not written: %v", err)
	}
	out := string(data)
	for _, want := range []string{"hooks:", "guard-branch", "commands:", "ship", "contentHash:"} {
		if !strings.Contains(out, want) {
			t.Errorf("lock missing %q\n---\n%s", want, out)
		}
	}
}

func TestLockPipelineAcceptsCleanHookAndCommandGraph(t *testing.T) {
	dir := t.TempDir()
	// A hook and a command both depending on the same cliTool is not a cycle;
	// the graph must accept it without a false positive.
	manifestYAML := `version: 1
cliTools:
  gh: { versionConstraint: ">=2" }
hooks:
  a: { event: Stop, command: "echo a", requires: [ { cliTool: gh } ] }
commands:
  b: { source: ./b.md, requires: [ { cliTool: gh } ] }
`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(manifestYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RunLock(dir); err != nil {
		t.Fatalf("clean hook+command manifest must not error: %v", err)
	}
}

func TestLockPipelineHashesHeadersForDrift(t *testing.T) {
	run := func(token string) string {
		dir := t.TempDir()
		manifestYAML := `version: 1
templates:
  api:
    params: { tok: { type: string, required: true } }
    produces:
      mcpServer:
        transport: http
        url: https://mcp.example.com
        headers: { Authorization: "${params.tok}" }
mcpServers:
  svc: { template: api, params: { tok: "` + token + `" } }
`
		if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(manifestYAML), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := RunLock(dir); err != nil {
			t.Fatalf("RunLock: %v", err)
		}
		data, err := os.ReadFile(filepath.Join(dir, "ainfra.lock"))
		if err != nil {
			t.Fatalf("lock not written: %v", err)
		}
		return string(data)
	}
	hashLine := func(lock string) string {
		for _, line := range strings.Split(lock, "\n") {
			if strings.Contains(line, "contentHash:") {
				return strings.TrimSpace(line)
			}
		}
		return ""
	}
	a, b := run("token-one"), run("token-two")
	if hashLine(a) == "" {
		t.Fatal("no contentHash in lock")
	}
	if hashLine(a) == hashLine(b) {
		t.Errorf("a header change did not affect contentHash: %q", hashLine(a))
	}
}

func TestLockPipelineHashesArgsForDrift(t *testing.T) {
	run := func(arg string) string {
		dir := t.TempDir()
		manifestYAML := `version: 1
templates:
  api:
    params: { host: { type: string, required: true } }
    produces:
      mcpServer:
        command: npx
        version: "1.0.0"
        args: ["-y", "` + arg + `"]
mcpServers:
  svc: { template: api, params: { host: x } }
`
		if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(manifestYAML), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := RunLock(dir); err != nil {
			t.Fatalf("RunLock: %v", err)
		}
		data, err := os.ReadFile(filepath.Join(dir, "ainfra.lock"))
		if err != nil {
			t.Fatalf("lock not written: %v", err)
		}
		return string(data)
	}
	hashLine := func(lock string) string {
		for _, line := range strings.Split(lock, "\n") {
			if strings.Contains(line, "contentHash:") {
				return strings.TrimSpace(line)
			}
		}
		return ""
	}
	a, b := run("pkg-one"), run("pkg-two")
	if hashLine(a) == "" {
		t.Fatal("no contentHash in lock")
	}
	if hashLine(a) == hashLine(b) {
		t.Errorf("an args change did not affect contentHash: %q", hashLine(a))
	}
}
