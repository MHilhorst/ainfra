package resolve

import (
	"testing"

	"github.com/MHilhorst/ainfra/internal/manifest"
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

func TestInstantiateInterpolatesArgs(t *testing.T) {
	tmpl := manifest.Template{
		Params: map[string]manifest.Param{
			"root": {Type: "string", Required: true},
		},
		Produces: manifest.Produces{
			MCPServer: &manifest.MCPServer{
				Command: "npx",
				Version: "0.6.2",
				Args:    []string{"-y", "@modelcontextprotocol/server-filesystem", "${params.root}"},
			},
		},
	}
	a, err := Instantiate("repo", manifest.MCPServer{Params: map[string]any{"root": "."}}, tmpl, nil)
	if err != nil {
		t.Fatalf("Instantiate a: %v", err)
	}
	b, err := Instantiate("docs", manifest.MCPServer{Params: map[string]any{"root": "./docs"}}, tmpl, nil)
	if err != nil {
		t.Fatalf("Instantiate b: %v", err)
	}
	if got := a.MCPServer.Args[2]; got != "." {
		t.Errorf("instance a args[2] = %q, want %q", got, ".")
	}
	if got := b.MCPServer.Args[2]; got != "./docs" {
		t.Errorf("instance b args[2] = %q, want %q", got, "./docs")
	}
	if a.MCPServer.Args[0] != "-y" {
		t.Errorf("non-interpolated args mangled: %v", a.MCPServer.Args)
	}
}

func TestInstantiateInterpolatesCommandAndURL(t *testing.T) {
	tmpl := manifest.Template{
		Params: map[string]manifest.Param{
			"host": {Type: "string", Required: true},
			"bin":  {Type: "string", Required: true},
		},
		Produces: manifest.Produces{
			MCPServer: &manifest.MCPServer{
				Transport: "http",
				Command:   "${params.bin}",
				URL:       "https://${params.host}/sse",
				Version:   "1.0.0",
			},
		},
	}
	inst := manifest.MCPServer{Params: map[string]any{"host": "mcp.example.com", "bin": "node"}}
	out, err := Instantiate("svc", inst, tmpl, nil)
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	if out.MCPServer.Command != "node" {
		t.Errorf("command = %q, want %q", out.MCPServer.Command, "node")
	}
	if out.MCPServer.URL != "https://mcp.example.com/sse" {
		t.Errorf("url = %q, want %q", out.MCPServer.URL, "https://mcp.example.com/sse")
	}
}

func TestInstantiateRejectsBadArgsReference(t *testing.T) {
	tmpl := manifest.Template{
		Produces: manifest.Produces{
			MCPServer: &manifest.MCPServer{
				Command: "npx",
				Version: "1.0.0",
				Args:    []string{"${params.missing}"},
			},
		},
	}
	_, err := Instantiate("x", manifest.MCPServer{Template: "t"}, tmpl, nil)
	if err == nil {
		t.Fatal("want error for bad ${...} reference in args")
	}
}

func TestInstantiateInterpolatesHeaders(t *testing.T) {
	tmpl := manifest.Template{
		Params: map[string]manifest.Param{
			"region": {Type: "string", Required: true},
		},
		Produces: manifest.Produces{
			MCPServer: &manifest.MCPServer{
				Transport: "http",
				URL:       "https://mcp.example.com",
				Headers:   map[string]string{"X-Region": "${params.region}"},
			},
		},
	}
	inst := manifest.MCPServer{Params: map[string]any{"region": "eu-west-1"}}
	out, err := Instantiate("svc", inst, tmpl, map[string]any{})
	if err != nil {
		t.Fatalf("Instantiate: %v", err)
	}
	if out.MCPServer.Headers["X-Region"] != "eu-west-1" {
		t.Errorf("headers = %v, want X-Region=eu-west-1", out.MCPServer.Headers)
	}
}
