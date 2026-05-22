package resolve

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MHilhorst/ainfra/internal/lockfile"
	"github.com/MHilhorst/ainfra/internal/provider"
)

const secretManifest = `version: 1
mcpServers:
  linear:
    transport: http
    url: https://mcp.linear.app/sse
    headers:
      Authorization: "Bearer ${secret.token}"
    secret:
      token:
        mode: direct
        ref: "op://Engineering/linear/mcp"
`

func writeSecretManifest(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ainfra.yaml"), []byte(secretManifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	return dir
}

func TestRunLockRecordsSecretRefs(t *testing.T) {
	dir := writeSecretManifest(t)
	if err := RunLock(dir, provider.ExecRunner{}); err != nil {
		t.Fatalf("RunLock: %v", err)
	}
	lock, err := lockfile.Read(filepath.Join(dir, "ainfra.lock"))
	if err != nil {
		t.Fatalf("Read lock: %v", err)
	}
	sr, ok := lock.Secrets["AINFRA_SECRET_MCPSERVERS_LINEAR_TOKEN"]
	if !ok {
		t.Fatalf("lock.Secrets = %+v, want the linear token", lock.Secrets)
	}
	if sr.Ref != "op://Engineering/linear/mcp" {
		t.Errorf("SecretRef.Ref = %q", sr.Ref)
	}
}

func TestRenderResourcesRendersPlaceholderIntoHeaders(t *testing.T) {
	dir := writeSecretManifest(t)
	res, err := RenderResources(dir, provider.ExecRunner{})
	if err != nil {
		t.Fatalf("RenderResources: %v", err)
	}
	var headers map[string]string
	for _, r := range res["mcpServers"] {
		if r.ID == "linear" {
			headers, _ = r.Payload["headers"].(map[string]string)
		}
	}
	got := headers["Authorization"]
	if !strings.Contains(got, "${AINFRA_SECRET_MCPSERVERS_LINEAR_TOKEN}") {
		t.Errorf("Authorization header = %q, want the placeholder", got)
	}
}
