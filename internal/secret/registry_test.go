package secret

import (
	"errors"
	"strings"
	"testing"
)

var errNotReady = errors.New("backend not ready")

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

// availableResolver is a Resolver that also reports backend readiness.
type availableResolver struct {
	fakeResolver
	avail error
}

func (a availableResolver) Available() error { return a.avail }

func TestRegistryCheckBackend(t *testing.T) {
	reg := NewRegistry()
	reg.Add(fakeResolver{scheme: "env"})                                 // no Availabler
	reg.Add(availableResolver{fakeResolver: fakeResolver{scheme: "op"}}) // ready
	reg.Add(availableResolver{fakeResolver: fakeResolver{scheme: "vault"}, avail: errNotReady})

	if err := reg.CheckBackend("env"); err != nil {
		t.Errorf("scheme without a probe should pass: %v", err)
	}
	if err := reg.CheckBackend("op"); err != nil {
		t.Errorf("ready backend: %v", err)
	}
	if err := reg.CheckBackend("vault"); err == nil {
		t.Error("unready backend: want error, got nil")
	}
	if err := reg.CheckBackend("missing"); err != nil {
		t.Errorf("unregistered scheme should pass (unknowable): %v", err)
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
