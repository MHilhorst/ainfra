package secret

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Runner runs an external command and returns its trimmed stdout. It abstracts
// os/exec so tests can substitute a fake op binary.
type Runner interface {
	Run(name string, args ...string) (stdout string, err error)
}

// ExecRunner is the production Runner backed by os/exec.
type ExecRunner struct{}

// Run executes name with args and returns trimmed stdout. On a non-zero exit
// it returns the command's trimmed stderr as the error message.
func (ExecRunner) Run(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			return "", fmt.Errorf("%s", strings.TrimSpace(string(ee.Stderr)))
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// OpResolver resolves op://... references via the 1Password CLI (`op read`).
type OpResolver struct {
	Runner Runner
}

// Scheme returns "op".
func (OpResolver) Scheme() string { return "op" }

// Resolve returns the secret value for ref via `op read`.
func (o OpResolver) Resolve(ref string) (string, error) {
	val, err := o.Runner.Run("op", "read", ref)
	if err != nil {
		return "", opError(ref, err)
	}
	return val, nil
}

// Check verifies ref resolves without exposing the value.
func (o OpResolver) Check(ref string) error {
	if _, err := o.Runner.Run("op", "read", ref); err != nil {
		return opError(ref, err)
	}
	return nil
}

// Available reports whether the 1Password CLI is installed and has a usable
// session, without resolving any specific reference. It is the up-front
// readiness probe so `ainfra install` can fail fast — before writing any
// config — when op cannot resolve op:// references at all. `op whoami` is the
// cheapest call that exercises both the binary and the active session (it works
// with the desktop-app integration, a service-account token, or a Connect
// server), so any error from it means op is not ready.
func (o OpResolver) Available() error {
	if _, err := o.Runner.Run("op", "whoami"); err != nil {
		if hint := opUnavailableHint(err); hint != "" {
			return fmt.Errorf("1Password: %s", hint)
		}
		return fmt.Errorf("1Password CLI is not ready (`op whoami` failed): %s", strings.TrimSpace(err.Error()))
	}
	return nil
}

// opUnavailableHint maps a raw `op` CLI failure to an actionable remediation,
// or "" when the failure is not a recognizable CLI-readiness problem. The op
// CLI phrases the "not usable yet" state several ways depending on how the user
// authenticates (no account added, app integration locked, expired session),
// so all of them collapse to the same advice. Matching is case-insensitive
// because op capitalizes some of these messages (e.g. "No accounts configured").
func opUnavailableHint(err error) string {
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "executable file not found"):
		return "the 1Password CLI is not installed — see https://developer.1password.com/docs/cli/get-started/"
	case strings.Contains(msg, "no accounts configured"),
		strings.Contains(msg, "no account found"),
		strings.Contains(msg, "not currently signed in"),
		strings.Contains(msg, "no active session"),
		strings.Contains(msg, "not signed in"),
		strings.Contains(msg, "session expired"),
		strings.Contains(msg, "error initializing client"):
		return "not signed in to 1Password — run `op signin`, or enable the 1Password app integration: https://developer.1password.com/docs/cli/app-integration/"
	default:
		return ""
	}
}

// opError maps a raw `op` CLI failure for a specific ref to an actionable
// message. It never includes a secret value.
func opError(ref string, err error) error {
	if hint := opUnavailableHint(err); hint != "" {
		return fmt.Errorf("secret %q: %s", ref, hint)
	}
	return fmt.Errorf("secret %q: %s", ref, strings.TrimSpace(err.Error()))
}
