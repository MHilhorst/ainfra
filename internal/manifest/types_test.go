package manifest

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestManifestUnmarshalsMultiDBShape(t *testing.T) {
	src := []byte(`
version: 1
cliTools:
  ssh:
    versionConstraint: ">=8.0"
templates:
  t:
    params:
      host: { type: string, required: true }
mcpServers:
  analytics-db:
    template: t
    params: { host: a.example }
`)
	var m Manifest
	if err := yaml.Unmarshal(src, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m.Version != 1 {
		t.Errorf("version = %d, want 1", m.Version)
	}
	if _, ok := m.CLITools["ssh"]; !ok {
		t.Error("cliTools.ssh missing")
	}
	inst := m.MCPServers["analytics-db"]
	if inst.Template != "t" {
		t.Errorf("template = %q, want t", inst.Template)
	}
	if inst.Params["host"] != "a.example" {
		t.Errorf("params.host = %v", inst.Params["host"])
	}
}
