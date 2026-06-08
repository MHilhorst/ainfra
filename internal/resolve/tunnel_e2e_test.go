package resolve_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MHilhorst/ainfra/internal/provider"
	"github.com/MHilhorst/ainfra/internal/provider/claudecode"
	"github.com/MHilhorst/ainfra/internal/resolve"
)

// End-to-end: a templated MCP server with an ssh-tunnel background service must,
// after RenderResources -> Services.Apply -> Hooks.Apply, leave a real
// (non-stub) start.sh AND a SessionStart hook in settings.json that runs it.
// This is the whole chain that was broken: scripts were stubs and never run.
func TestTunnelEndToEnd_StartScriptAndSessionStartHook(t *testing.T) {
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
          command: 'nc -z 127.0.0.1 ${resolved.tunnelPort} 2>/dev/null || ssh -f -N -L ${resolved.tunnelPort}:127.0.0.1:3306 ${params.sshHost}'
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

	resources, err := resolve.RenderResources(dir, provider.ExecRunner{})
	if err != nil {
		t.Fatalf("RenderResources: %v", err)
	}

	mem := provider.NewMemFilesystem()
	env := provider.Env{FS: mem, Root: "/repo"}

	// Apply the background service -> start.sh / stop.sh.
	svcChanges := make([]provider.Change, 0)
	for _, r := range resources["backgroundServices"] {
		svcChanges = append(svcChanges, provider.Change{Kind: provider.ChangeCreate, ID: r.ID, Resource: r})
	}
	if _, err := (claudecode.Services{}).Apply(env, provider.ChannelPlan{Channel: "backgroundServices", Changes: svcChanges}); err != nil {
		t.Fatalf("Services.Apply: %v", err)
	}

	// Apply the hooks -> settings.json.
	hookChanges := make([]provider.Change, 0)
	for _, r := range resources["hooks"] {
		hookChanges = append(hookChanges, provider.Change{Kind: provider.ChangeCreate, ID: r.ID, Resource: r})
	}
	if _, err := (claudecode.Hooks{}).Apply(env, provider.ChannelPlan{Channel: "hooks", Changes: hookChanges}); err != nil {
		t.Fatalf("Hooks.Apply: %v", err)
	}

	start, err := mem.ReadFile("/repo/.ainfra/services/db-prod-tunnel/start.sh")
	if err != nil {
		t.Fatalf("start.sh not written: %v", err)
	}
	if strings.Contains(string(start), "TODO") {
		t.Errorf("start.sh is still a stub:\n%s", start)
	}
	// The command comes verbatim from the config's spec.command (ainfra adds no
	// ssh knowledge): bare ssh-config alias, no forced sshUser.
	if !strings.Contains(string(start), "ssh -f -N -L") || !strings.Contains(string(start), "example-prod") {
		t.Errorf("start.sh missing the configured tunnel command:\n%s", start)
	}
	if strings.Contains(string(start), "deploy@") {
		t.Errorf("start.sh must not force a user; the command should use the bare alias:\n%s", start)
	}

	settings, err := mem.ReadFile("/repo/.claude/settings.json")
	if err != nil {
		t.Fatalf("settings.json not written: %v", err)
	}
	if !strings.Contains(string(settings), "SessionStart") {
		t.Errorf("settings.json has no SessionStart hook:\n%s", settings)
	}
	if !strings.Contains(string(settings), "db-prod-tunnel/start.sh") {
		t.Errorf("SessionStart hook does not run the tunnel start.sh:\n%s", settings)
	}
}
