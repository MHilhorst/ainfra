package secret

import (
	"strings"
	"testing"
)

func TestStubResolverFailsClearly(t *testing.T) {
	_, err := StubResolver{SchemeName: "vault"}.Resolve("vault://path/key")
	if err == nil || !strings.Contains(err.Error(), "not implemented") {
		t.Errorf("error = %v, want it to say vault:// is not implemented", err)
	}
}

func TestDefaultRegistryHasAllSchemes(t *testing.T) {
	reg := DefaultRegistry()
	for _, ref := range []string{
		"env://X", "op://V/i/f", "doppler://p/c", "vault://s/k", "sops://f#k",
	} {
		if _, err := reg.resolverFor(ref); err != nil {
			t.Errorf("DefaultRegistry has no resolver for %q: %v", ref, err)
		}
	}
}
