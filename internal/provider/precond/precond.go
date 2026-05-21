// Package precond runs verify-only manifest preconditions.
package precond

import "github.com/MHilhorst/ainfra/internal/provider"

// Precondition is one verify-only check.
type Precondition struct {
	ID          string
	Command     string // a shell command; exit 0 means the precondition holds
	Remediation string
}

// Failure is a precondition that did not hold.
type Failure struct {
	ID          string
	Remediation string
}

// CheckAll runs every precondition and returns the failures.
func CheckAll(env provider.Env, ps []Precondition) []Failure {
	var out []Failure
	for _, p := range ps {
		if p.Command == "" {
			continue
		}
		if _, err := env.Runner.Run("sh", "-c", p.Command); err != nil {
			out = append(out, Failure{ID: p.ID, Remediation: p.Remediation})
		}
	}
	return out
}
