package secret

import "fmt"

// StubResolver registers a scheme that this increment does not implement, so
// a manifest using it stays well-formed but fails loudly at resolve time.
type StubResolver struct{ SchemeName string }

// Scheme returns the stubbed scheme name.
func (s StubResolver) Scheme() string { return s.SchemeName }

// Resolve always fails with a "not implemented" error.
func (s StubResolver) Resolve(ref string) (string, error) { return "", s.err(ref) }

// Check always fails with a "not implemented" error.
func (s StubResolver) Check(ref string) error { return s.err(ref) }

func (s StubResolver) err(ref string) error {
	return fmt.Errorf("secret %q: %s:// is not implemented in this increment (only op:// and env:// are supported)", ref, s.SchemeName)
}

// DefaultRegistry returns a Registry with every production resolver registered.
func DefaultRegistry() *Registry {
	reg := NewRegistry()
	reg.Add(EnvResolver{})
	reg.Add(OpResolver{Runner: ExecRunner{}})
	reg.Add(StubResolver{SchemeName: "doppler"})
	reg.Add(StubResolver{SchemeName: "vault"})
	reg.Add(StubResolver{SchemeName: "sops"})
	return reg
}
