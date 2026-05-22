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

// opError maps a raw `op` CLI failure to an actionable message. It never
// includes a secret value.
func opError(ref string, err error) error {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "executable file not found"):
		return fmt.Errorf("secret %q: the 1Password CLI is not installed — see https://developer.1password.com/docs/cli/get-started/", ref)
	case strings.Contains(msg, "not currently signed in"), strings.Contains(msg, "no active session"):
		return fmt.Errorf("secret %q: not signed in to 1Password — run: op signin", ref)
	default:
		return fmt.Errorf("secret %q: %s", ref, msg)
	}
}
