package resolve

import (
	"testing"

	"github.com/MHilhorst/aistack/internal/manifest"
)

func TestInstantiateProducesNamespacedService(t *testing.T) {
	tmpl := manifest.Template{
		Params:   map[string]manifest.Param{"host": {Type: "string", Required: true}},
		Secrets:  map[string]manifest.TemplateSecret{"pw": {Required: true}},
		Resolved: map[string]manifest.ResolvedField{"port": {Kind: "allocated-port"}},
		Produces: manifest.Produces{
			MCPServer: &manifest.MCPServer{
				Command: "npx", Version: "1.0.0",
				Env:      map[string]string{"H": "${params.host}", "P": "${resolved.port}"},
				Requires: []manifest.Require{{Service: "${instance.id}-tunnel"}},
			},
			BackgroundService: &manifest.BackgroundService{
				ID: "${instance.id}-tunnel", Kind: "ssh-tunnel",
			},
		},
	}
	inst := manifest.MCPServer{
		Template: "t",
		Params:   map[string]any{"host": "db.example"},
		Secret:   map[string]any{"pw": map[string]any{"ref": "op://x"}},
	}
	got, err := Instantiate("analytics-db", inst, tmpl, map[string]any{"port": 13306})
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	if got.MCPServer.Env["H"] != "db.example" || got.MCPServer.Env["P"] != "13306" {
		t.Errorf("env not interpolated: %+v", got.MCPServer.Env)
	}
	if got.MCPServer.Requires[0].Service != "analytics-db-tunnel" {
		t.Errorf("requires not interpolated: %+v", got.MCPServer.Requires)
	}
	if got.Service.ID != "analytics-db-tunnel" {
		t.Errorf("service id = %q", got.Service.ID)
	}
}

func TestInstantiateRejectsMissingRequiredParam(t *testing.T) {
	tmpl := manifest.Template{Params: map[string]manifest.Param{"host": {Required: true}}}
	_, err := Instantiate("x", manifest.MCPServer{Template: "t"}, tmpl, nil)
	if err == nil {
		t.Fatal("want error for missing required param")
	}
}

func TestInstantiateDoesNotAliasTemplateBetweenInstances(t *testing.T) {
	tmpl := manifest.Template{
		Produces: manifest.Produces{
			MCPServer: &manifest.MCPServer{
				Command: "npx",
				Version: "1.0.0",
				Args:    []string{"-y", "pkg"},
			},
		},
	}
	a, err := Instantiate("a", manifest.MCPServer{Template: "t"}, tmpl, nil)
	if err != nil {
		t.Fatalf("Instantiate a: %v", err)
	}
	b, err := Instantiate("b", manifest.MCPServer{Template: "t"}, tmpl, nil)
	if err != nil {
		t.Fatalf("Instantiate b: %v", err)
	}
	a.MCPServer.Args[0] = "MUTATED"
	if b.MCPServer.Args[0] == "MUTATED" {
		t.Error("instance b aliases instance a's Args")
	}
	if tmpl.Produces.MCPServer.Args[0] == "MUTATED" {
		t.Error("instance a aliases the template's Args")
	}
}

func TestInstantiateRejectsBadRequiresReference(t *testing.T) {
	tmpl := manifest.Template{
		Produces: manifest.Produces{
			MCPServer: &manifest.MCPServer{
				Command:  "npx",
				Version:  "1.0.0",
				Requires: []manifest.Require{{Service: "${instance.bogus}-tunnel"}},
			},
		},
	}
	_, err := Instantiate("x", manifest.MCPServer{Template: "t"}, tmpl, nil)
	if err == nil {
		t.Fatal("want error for bad ${...} reference in requires")
	}
}
