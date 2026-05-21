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

func TestLockPipelineResolvesScheduledJobs(t *testing.T) {
	dir := t.TempDir()
	manifestYAML := `version: 1
targets: [hub, laptop]
cliTools:
  claude: { versionConstraint: ">=1" }
scheduledJobs:
  nightly-health:
    schedule: "0 6 * * *"
    command: claude -p "check replication lag"
    runsOn: [hub]
    requires: [ { cliTool: claude } ]
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
	for _, want := range []string{"scheduledJobs:", "nightly-health", "runsOn:", "hub", "contentHash:"} {
		if !strings.Contains(out, want) {
			t.Errorf("lock missing %q\n---\n%s", want, out)
		}
	}
}

func TestLockPipelineRejectsRunsOnOutsideVocabulary(t *testing.T) {
	dir := t.TempDir()
	manifestYAML := `version: 1
targets: [hub]
scheduledJobs:
  bad:
    schedule: "0 6 * * *"
    command: echo x
    runsOn: [mars]
`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(manifestYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RunLock(dir); err == nil {
		t.Fatal("want validation error for runsOn outside the vocabulary")
	}
}

func TestLockPipelineAcceptsCrossLayerVocabulary(t *testing.T) {
	dir := t.TempDir()
	// The repo manifest's job targets `hub`, but `hub` is declared only in the
	// personal layer's vocabulary. The merged vocabulary (repo + personal) must
	// make this job validate.
	repo := `version: 1
scheduledJobs:
  triage:
    schedule: "0 */4 * * *"
    command: echo triage
    runsOn: [hub]
`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(repo), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ainfra.personal.yaml"),
		[]byte("version: 1\ntargets: [hub]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RunLock(dir); err != nil {
		t.Fatalf("cross-layer vocabulary must validate: %v", err)
	}
}
