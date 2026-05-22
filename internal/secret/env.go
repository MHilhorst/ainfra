package secret

import (
	"fmt"
	"os"
	"strings"
)

// EnvResolver resolves env://VARNAME refs from the process environment. It is
// the always-works fallback for a developer who injects secrets themselves.
type EnvResolver struct{}

// Scheme returns "env".
func (EnvResolver) Scheme() string { return "env" }

// Resolve returns the value of the named environment variable.
func (EnvResolver) Resolve(ref string) (string, error) {
	name := strings.TrimPrefix(ref, "env://")
	if name == "" {
		return "", fmt.Errorf("env ref %q has no variable name", ref)
	}
	v, ok := os.LookupEnv(name)
	if !ok || v == "" {
		return "", fmt.Errorf("env ref %q: variable %s is not set", ref, name)
	}
	return v, nil
}

// Check verifies the variable is set without returning its value.
func (e EnvResolver) Check(ref string) error {
	_, err := e.Resolve(ref)
	return err
}
