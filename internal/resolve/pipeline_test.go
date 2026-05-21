package resolve

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MHilhorst/ainfra/internal/lockfile"
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

func TestLockPipelineResolvesSkillsPluginsRules(t *testing.T) {
	dir := t.TempDir()
	manifestYAML := `version: 1
cliTools:
  node: { versionConstraint: ">=20" }
skills:
  debug:
    source: ./skills/debug
    requires: [ { cliTool: node } ]
plugins:
  tvt: { source: "npm:@acme/tvt@2.0.1", version: "2.0.1" }
rules:
  team: { target: CLAUDE.md, source: ./rules/team.md }
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
	for _, want := range []string{"skills:", "debug", "plugins:", "tvt", "rules:", "team", "cli:node"} {
		if !strings.Contains(out, want) {
			t.Errorf("lock missing %q\n---\n%s", want, out)
		}
	}
	lock, err := lockfile.Read(filepath.Join(dir, "ainfra.lock"))
	if err != nil {
		t.Fatalf("read lock: %v", err)
	}
	if got := lock.Entries.Skills["debug"].Requires; len(got) != 1 || got[0] != "cli:node" {
		t.Errorf("skill debug requires = %v, want [cli:node]", got)
	}
	if e, ok := lock.Entries.Plugins["tvt"]; !ok || e.Version != "2.0.1" {
		t.Errorf("plugin tvt = %+v, ok=%v", e, ok)
	}
	if e, ok := lock.Entries.Rules["team"]; !ok || e.ContentHash == "" {
		t.Errorf("rule team = %+v, ok=%v", e, ok)
	}
}

func TestLockPipelineResolvesTools(t *testing.T) {
	dir := t.TempDir()
	manifestYAML := `version: 1
tools:
  builtins:
    disabled: [WebFetch]
  permissions:
    allow: ["Bash(go test:*)"]
    deny: ["Bash(rm -rf:*)"]
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
	for _, want := range []string{"tools:", "repo:", "contentHash:"} {
		if !strings.Contains(out, want) {
			t.Errorf("lock missing %q\n---\n%s", want, out)
		}
	}
}

func TestLockPipelineResolvesInlineMCPServer(t *testing.T) {
	dir := t.TempDir()
	manifestYAML := `version: 1
mcpServers:
  github:
    command: npx
    args: ["-y", "@modelcontextprotocol/server-github"]
    version: "2025.4.0"
    transport: stdio
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
	for _, want := range []string{"github", "version: 2025.4.0", "contentHash:"} {
		if !strings.Contains(out, want) {
			t.Errorf("lock missing %q\n---\n%s", want, out)
		}
	}
}

func TestLockPipelineRecordsRequiresOnExistingChannels(t *testing.T) {
	dir := t.TempDir()
	manifestYAML := `version: 1
cliTools:
  node: { versionConstraint: ">=20" }
  ssh: { versionConstraint: ">=8" }
templates:
  tun:
    params: { host: { type: string, required: true } }
    resolved: { tunnelPort: { kind: allocated-port } }
    produces:
      mcpServer:
        command: npx
        version: "1.0.0"
        requires: [ { service: "${instance.id}-tunnel" } ]
      backgroundService:
        id: "${instance.id}-tunnel"
        kind: ssh-tunnel
        requires: [ { cliTool: ssh } ]
mcpServers:
  db-a: { template: tun, params: { host: a.example } }
hooks:
  guard: { event: Stop, command: "echo x", requires: [ { cliTool: node } ] }
commands:
  ship: { source: ./ship.md, requires: [ { cliTool: node } ] }
`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(manifestYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RunLock(dir); err != nil {
		t.Fatalf("RunLock: %v", err)
	}
	lock, err := lockfile.Read(filepath.Join(dir, "ainfra.lock"))
	if err != nil {
		t.Fatalf("read lock: %v", err)
	}
	cases := []struct {
		name string
		got  []string
		want string
	}{
		{"templated MCP server", lock.Entries.MCPServers["db-a"].Requires, "svc:db-a-tunnel"},
		{"background service", lock.Entries.BackgroundServices["db-a-tunnel"].Requires, "cli:ssh"},
		{"hook", lock.Entries.Hooks["guard"].Requires, "cli:node"},
		{"command", lock.Entries.Commands["ship"].Requires, "cli:node"},
	}
	for _, c := range cases {
		if len(c.got) != 1 || c.got[0] != c.want {
			t.Errorf("%s requires = %v, want [%s]", c.name, c.got, c.want)
		}
	}
}

func TestLockPipelineRecordsManifestHash(t *testing.T) {
	dir := t.TempDir()
	manifestYAML := `version: 1
rules:
  team: { target: CLAUDE.md, source: ./rules/team.md }
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
	if !strings.Contains(string(data), "manifestHash: sha256:") {
		t.Errorf("ainfra.lock missing manifestHash\n---\n%s", data)
	}
	if !strings.Contains(string(data), "team") {
		t.Errorf("ainfra.lock dropped the rules entry\n---\n%s", data)
	}
}

func TestManifestHashIgnoresPersonalLayer(t *testing.T) {
	dir := t.TempDir()
	repoYAML := `version: 1
rules:
  team: { target: CLAUDE.md, source: ./rules/team.md }
`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(repoYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RunLock(dir); err != nil {
		t.Fatalf("RunLock: %v", err)
	}
	first, err := lockfile.Read(filepath.Join(dir, "ainfra.lock"))
	if err != nil {
		t.Fatalf("read lock: %v", err)
	}

	// Adding a personal layer must not change the committed lock's hash.
	personalYAML := `version: 1
rules:
  mine: { target: CLAUDE.md, source: ./rules/mine.md }
`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.personal.yaml"), []byte(personalYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RunLock(dir); err != nil {
		t.Fatalf("RunLock with personal layer: %v", err)
	}
	second, err := lockfile.Read(filepath.Join(dir, "ainfra.lock"))
	if err != nil {
		t.Fatalf("read lock: %v", err)
	}
	if first.ManifestHash == "" {
		t.Fatal("committed lock has no manifest hash")
	}
	if second.ManifestHash != first.ManifestHash {
		t.Errorf("committed manifest hash changed after adding a personal layer: %q -> %q",
			first.ManifestHash, second.ManifestHash)
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
