package main

import (
	"strings"
	"testing"

	"github.com/MHilhorst/ainfra/internal/lockfile"
	"github.com/MHilhorst/ainfra/internal/secret"
)

func TestCheckSecretsReportsUnresolvableRefs(t *testing.T) {
	committed := &lockfile.Lock{
		Secrets: map[string]lockfile.SecretRef{
			"AINFRA_SECRET_OK":  {Var: "AINFRA_SECRET_OK", Ref: "op://Eng/ok/f", Scheme: "op"},
			"AINFRA_SECRET_BAD": {Var: "AINFRA_SECRET_BAD", Ref: "op://Eng/bad/f", Scheme: "op"},
		},
	}
	reg := secret.NewRegistry()
	reg.Add(checkSecretResolver{})

	failures := checkSecrets(committed, &lockfile.Lock{}, reg)
	if len(failures) != 1 {
		t.Fatalf("got %d failures, want 1: %v", len(failures), failures)
	}
	if failures[0] == "" {
		t.Error("failure message is empty")
	}
}

// checkSecretResolver fails Check for any ref containing "bad".
type checkSecretResolver struct{}

func (checkSecretResolver) Scheme() string                 { return "op" }
func (checkSecretResolver) Resolve(string) (string, error) { return "v", nil }
func (checkSecretResolver) Check(ref string) error {
	if strings.Contains(ref, "bad") {
		return stringError("item not found")
	}
	return nil
}
