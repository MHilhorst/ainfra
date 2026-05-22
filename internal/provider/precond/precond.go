// Package precond runs verify-only manifest preconditions.
package precond

import (
	"net"

	"github.com/MHilhorst/ainfra/internal/provider"
)

// Precondition is one verify-only check. Kind selects how it is evaluated:
// "dns-resolves" looks up Host; anything else runs Command as a shell command
// (exit 0 means the precondition holds).
type Precondition struct {
	ID          string
	Kind        string // "dns-resolves" | "shell" (default)
	Command     string // shell command, for the default kind
	Host        string // hostname, for kind dns-resolves
	Remediation string
}

// Failure is a precondition that did not hold.
type Failure struct {
	ID          string
	Remediation string
}

// CheckAll runs every precondition and returns the failures. A precondition
// with nothing to evaluate (empty Command, or empty Host for dns-resolves) is
// skipped rather than failed.
func CheckAll(env provider.Env, ps []Precondition) []Failure {
	var out []Failure
	for _, p := range ps {
		switch p.Kind {
		case "dns-resolves":
			if p.Host == "" {
				continue
			}
			if _, err := net.LookupHost(p.Host); err != nil {
				out = append(out, Failure{ID: p.ID, Remediation: p.Remediation})
			}
		default:
			if p.Command == "" {
				continue
			}
			if _, err := env.Runner.Run("sh", "-c", p.Command); err != nil {
				out = append(out, Failure{ID: p.ID, Remediation: p.Remediation})
			}
		}
	}
	return out
}
