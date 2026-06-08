package resolve

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MHilhorst/ainfra/internal/lockfile"
	"github.com/MHilhorst/ainfra/internal/provider"
)

// The service content hash must fold in the script-generator version, so that
// improving the generator forces existing installs (whose service spec is
// unchanged) to rewrite start.sh/stop.sh on the next apply. If the hash equaled
// a bare {kind,spec} hash, the diff would be a Noop and the stub script would
// survive forever.
func TestServiceContentHash_FoldsInGeneratorVersion(t *testing.T) {
	kind := "ssh-tunnel"
	spec := map[string]any{"localPort": "13307", "sshHost": "h"}

	withGen := serviceContentHash(kind, spec)
	bare := lockfile.ContentHash(map[string]any{"kind": kind, "spec": spec})

	if withGen == bare {
		t.Errorf("service hash does not fold in the generator version; a generator change could not force a re-render")
	}
	if withGen != serviceContentHash(kind, spec) {
		t.Errorf("service content hash is not stable across calls")
	}
}

// A template that produces a background service with lifecycle.generateHook:
// SessionStart must, in addition to the backgroundServices resource, emit a
// hooks resource so the generated start.sh actually runs. Without this the
// tunnel scripts are written but never executed.
func TestRenderResources_ServiceGeneratesSessionStartHook(t *testing.T) {
	dir := t.TempDir()
	manifestYAML := `version: 1
templates:
  tunnel:
    params:
      sshHost: { type: string, required: true }
    resolved:
      tunnelPort: { kind: allocated-port }
    produces:
      mcpServer:
        transport: stdio
        command: npx
        args: ["-y", "@benborla29/mcp-server-mysql"]
        version: "2.0.8"
        env: { P: "${resolved.tunnelPort}" }
        requires: [ { service: "${instance.id}-tunnel" } ]
      backgroundService:
        id: "${instance.id}-tunnel"
        kind: ssh-tunnel
        spec:
          localPort: "${resolved.tunnelPort}"
          remoteHost: "127.0.0.1"
          remotePort: 3306
          sshUser: deploy
          sshHost: "${params.sshHost}"
        lifecycle:
          generateHook: SessionStart
mcpServers:
  db-prod:
    template: tunnel
    params:
      sshHost: example-prod
`
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(manifestYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	resources, err := RenderResources(dir, provider.ExecRunner{})
	if err != nil {
		t.Fatalf("RenderResources: %v", err)
	}

	hooks := resources["hooks"]
	var found *provider.Resource
	for i := range hooks {
		if ev, _ := hooks[i].Payload["event"].(string); ev == "SessionStart" {
			found = &hooks[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("no SessionStart hook generated for the tunnel service; hooks=%+v", hooks)
	}
	cmd, _ := found.Payload["command"].(string)
	if !strings.Contains(cmd, "db-prod-tunnel") || !strings.Contains(cmd, "start.sh") {
		t.Errorf("hook command should run the service start.sh, got %q", cmd)
	}
	if found.ContentHash == "" {
		t.Errorf("generated hook resource needs a ContentHash for drift detection")
	}
}
