package resolve

import (
	"strings"
	"testing"

	"github.com/MHilhorst/ainfra/internal/manifest"
)

func TestCollectSecretRefsHandlesRefAndLiteral(t *testing.T) {
	raw := map[string]any{
		"token":  map[string]any{"mode": "direct", "ref": "op://Eng/linear/mcp"},
		"region": map[string]any{"mode": "direct", "value": "eu-west-1"},
	}
	refs, vals, _, err := collectSecretRefs("mcpServers", "linear", manifest.LayerRepo, raw, nil)
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
	refs, vals, _, err := collectSecretRefs("mcpServers", "db", manifest.LayerTeam, raw, top)
	if err != nil {
		t.Fatalf("collectSecretRefs: %v", err)
	}
	v := "AINFRA_SECRET_MCPSERVERS_DB_KEY"
	if vals["key"] != "${"+v+"}" || refs[v].Scope != "personal" {
		t.Errorf("refs=%+v vals=%+v", refs, vals)
	}
}

func TestCollectSecretRefsUsesDeclaredEnvName(t *testing.T) {
	// A secret declaring `env:` is exported under that name, not a generated one.
	top := map[string]manifest.Secret{
		"flare-api-token": {Mode: "direct", Ref: "op://Eng/flare/credential", Env: "FLARE_API_TOKEN"},
	}
	raw := map[string]any{"token": "flare-api-token"}
	refs, vals, _, err := collectSecretRefs("mcpServers", "flare", manifest.LayerTeam, raw, top)
	if err != nil {
		t.Fatalf("collectSecretRefs: %v", err)
	}
	if vals["token"] != "${FLARE_API_TOKEN}" {
		t.Errorf("vals[token] = %q, want ${FLARE_API_TOKEN}", vals["token"])
	}
	if _, ok := refs["FLARE_API_TOKEN"]; !ok {
		t.Errorf("refs missing FLARE_API_TOKEN key: %+v", refs)
	}
	if refs["FLARE_API_TOKEN"].Var != "FLARE_API_TOKEN" {
		t.Errorf("ref Var = %q, want FLARE_API_TOKEN", refs["FLARE_API_TOKEN"].Var)
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

func TestSubstituteSecretsRejectsBoundButUnusedSecret(t *testing.T) {
	// A binding with no reference and no export target can never reach the tool.
	srv := &manifest.MCPServer{
		Command: "npx",
		Args:    []string{"-y", "@modelcontextprotocol/server-github"},
	}
	raw := map[string]any{
		"token": map[string]any{"mode": "direct", "ref": "op://Eng/github/pat"},
	}
	_, err := substituteSecrets(srv, "mcpServers", "github", manifest.LayerRepo, raw, nil)
	if err == nil {
		t.Fatal("want error for a bound-but-unused secret, got nil")
	}
	if !strings.Contains(err.Error(), "bound but never used") || !strings.Contains(err.Error(), "${secret.token}") {
		t.Errorf("error = %v, want it to explain how to wire the secret", err)
	}
}

func TestSubstituteSecretsAllowsBindingWiredIntoEnv(t *testing.T) {
	srv := &manifest.MCPServer{
		Env: map[string]string{"GITHUB_PERSONAL_ACCESS_TOKEN": "${secret.token}"},
	}
	raw := map[string]any{
		"token": map[string]any{"mode": "direct", "ref": "op://Eng/github/pat"},
	}
	if _, err := substituteSecrets(srv, "mcpServers", "github", manifest.LayerRepo, raw, nil); err != nil {
		t.Fatalf("substituteSecrets: %v", err)
	}
	if got := srv.Env["GITHUB_PERSONAL_ACCESS_TOKEN"]; got != "${AINFRA_SECRET_MCPSERVERS_GITHUB_TOKEN}" {
		t.Errorf("env = %q, want the placeholder", got)
	}
}

func TestSubstituteSecretsWiresSecretInArgs(t *testing.T) {
	// A secret referenced in args is substituted and counts as used — args are
	// expanded by the client the same as env, so this is a valid wiring.
	srv := &manifest.MCPServer{
		Command: "some-server",
		Args:    []string{"--api-key=${secret.token}"},
	}
	raw := map[string]any{
		"token": map[string]any{"mode": "direct", "ref": "op://Eng/svc/key"},
	}
	if _, err := substituteSecrets(srv, "mcpServers", "svc", manifest.LayerRepo, raw, nil); err != nil {
		t.Fatalf("substituteSecrets: %v", err)
	}
	if got := srv.Args[0]; got != "--api-key=${AINFRA_SECRET_MCPSERVERS_SVC_TOKEN}" {
		t.Errorf("args[0] = %q, want the placeholder substituted", got)
	}
}

func TestSubstituteSecretsAllowsBindingWithEnvExportTarget(t *testing.T) {
	// A secret declaring `env:` is exported to the environment and inherited by
	// the server, so it is wired even without an in-place ${secret.*} reference.
	top := map[string]manifest.Secret{
		"github-token": {Mode: "direct", Ref: "op://Eng/github/pat", Env: "GITHUB_PERSONAL_ACCESS_TOKEN"},
	}
	srv := &manifest.MCPServer{Command: "npx"}
	raw := map[string]any{"token": "github-token"}
	if _, err := substituteSecrets(srv, "mcpServers", "github", manifest.LayerRepo, raw, top); err != nil {
		t.Fatalf("substituteSecrets: %v", err)
	}
}
