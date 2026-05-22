package resolve

import (
	"testing"

	"github.com/MHilhorst/ainfra/internal/manifest"
)

func TestCollectSecretRefsHandlesRefAndLiteral(t *testing.T) {
	raw := map[string]any{
		"token":  map[string]any{"mode": "direct", "ref": "op://Eng/linear/mcp"},
		"region": map[string]any{"mode": "direct", "value": "eu-west-1"},
	}
	refs, vals, err := collectSecretRefs("mcpServers", "linear", manifest.LayerRepo, raw, nil)
	if err != nil {
		t.Fatalf("collectSecretRefs: %v", err)
	}
	wantVar := "AINFRA_SECRET_MCPSERVERS_LINEAR_TOKEN"
	if vals["token"] != "${"+wantVar+"}" {
		t.Errorf("vals[token] = %q, want the placeholder", vals["token"])
	}
	if vals["region"] != "eu-west-1" {
		t.Errorf("vals[region] = %q, want the literal value", vals["region"])
	}
	sr, ok := refs[wantVar]
	if !ok {
		t.Fatalf("refs missing %q", wantVar)
	}
	if sr.Ref != "op://Eng/linear/mcp" || sr.Scheme != "op" || sr.Layer != "repo" || sr.Scope != "shared" {
		t.Errorf("SecretRef = %+v", sr)
	}
	if _, ok := refs["AINFRA_SECRET_MCPSERVERS_LINEAR_REGION"]; ok {
		t.Error("a literal-value secret must not produce a SecretRef")
	}
}

func TestCollectSecretRefsResolvesTopLevelByID(t *testing.T) {
	top := map[string]manifest.Secret{
		"bastion": {Mode: "direct", Ref: "op://Eng/bastion/key", Scope: "personal"},
	}
	raw := map[string]any{"key": "bastion"}
	refs, vals, err := collectSecretRefs("mcpServers", "db", manifest.LayerTeam, raw, top)
	if err != nil {
		t.Fatalf("collectSecretRefs: %v", err)
	}
	v := "AINFRA_SECRET_MCPSERVERS_DB_KEY"
	if vals["key"] != "${"+v+"}" || refs[v].Scope != "personal" {
		t.Errorf("refs=%+v vals=%+v", refs, vals)
	}
}

func TestSubstituteSecretsReplacesTokensInHeaders(t *testing.T) {
	srv := &manifest.MCPServer{
		Headers: map[string]string{"Authorization": "Bearer ${secret.token}"},
	}
	raw := map[string]any{
		"token": map[string]any{"mode": "direct", "ref": "op://Eng/linear/mcp"},
	}
	refs, err := substituteSecrets(srv, "mcpServers", "linear", manifest.LayerRepo, raw, nil)
	if err != nil {
		t.Fatalf("substituteSecrets: %v", err)
	}
	want := "Bearer ${AINFRA_SECRET_MCPSERVERS_LINEAR_TOKEN}"
	if srv.Headers["Authorization"] != want {
		t.Errorf("Authorization = %q, want %q", srv.Headers["Authorization"], want)
	}
	if len(refs) != 1 {
		t.Errorf("got %d refs, want 1", len(refs))
	}
}
