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
