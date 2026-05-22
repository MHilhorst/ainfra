package secret

import (
	"strings"
	"testing"
)

// fakeResolver is a Resolver returning a fixed value, for tests.
type fakeResolver struct{ scheme, value string }

func (f fakeResolver) Scheme() string                 { return f.scheme }
func (f fakeResolver) Resolve(string) (string, error) { return f.value, nil }
func (f fakeResolver) Check(string) error             { return nil }

func TestRegistryResolveDispatchesByScheme(t *testing.T) {
	reg := NewRegistry()
	reg.Add(fakeResolver{scheme: "op", value: "secret-value"})

	got, err := reg.Resolve("op://Vault/item/field")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "secret-value" {
		t.Errorf("Resolve = %q, want %q", got, "secret-value")
	}
}

func TestRegistryUnknownSchemeErrors(t *testing.T) {
	reg := NewRegistry()
	reg.Add(fakeResolver{scheme: "op"})

	_, err := reg.Resolve("vault://path/key")
	if err == nil {
		t.Fatal("Resolve with unknown scheme: want error, got nil")
	}
	if !strings.Contains(err.Error(), "vault") || !strings.Contains(err.Error(), "op") {
		t.Errorf("error %q should name the bad scheme and the registered schemes", err)
	}
}

func TestSchemeOf(t *testing.T) {
	got, err := SchemeOf("op://Vault/item")
	if err != nil || got != "op" {
		t.Fatalf("SchemeOf = %q, %v; want \"op\", nil", got, err)
	}
	if _, err := SchemeOf("no-scheme"); err == nil {
		t.Error("SchemeOf(\"no-scheme\"): want error, got nil")
	}
}
