package lockfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSecretRefRoundTrips(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ainfra.lock")
	in := &Lock{
		Version: 1,
		Secrets: map[string]SecretRef{
			"AINFRA_SECRET_MCPSERVERS_LINEAR_TOKEN": {
				Var:    "AINFRA_SECRET_MCPSERVERS_LINEAR_TOKEN",
				Ref:    "op://Engineering/linear/mcp",
				Scheme: "op",
				Scope:  "shared",
				Layer:  "repo",
			},
		},
	}
	if err := Write(path, in); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("lock not written: %v", err)
	}
	out, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	got, ok := out.Secrets["AINFRA_SECRET_MCPSERVERS_LINEAR_TOKEN"]
	if !ok {
		t.Fatal("Secrets entry missing after round-trip")
	}
	if got.Ref != "op://Engineering/linear/mcp" || got.Scheme != "op" || got.Layer != "repo" {
		t.Errorf("round-tripped SecretRef = %+v", got)
	}
}
