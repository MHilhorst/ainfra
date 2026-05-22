// Package provider defines the channel reconciliation contract and the shared
// machinery (diff, environment, orchestration) every channel provider uses.
package provider

import "fmt"

// ChangeKind classifies a single planned mutation.
type ChangeKind int

const (
	ChangeNoop ChangeKind = iota
	ChangeCreate
	ChangeUpdate
	ChangeDelete
)

// String renders a ChangeKind as the one-character plan symbol.
func (k ChangeKind) String() string {
	switch k {
	case ChangeCreate:
		return "+"
	case ChangeUpdate:
		return "~"
	case ChangeDelete:
		return "-"
	default:
		return " "
	}
}

// Resource is one channel entry in a provider-neutral shape. Desired resources
// come from the lockfile; observed resources are built by a provider's Observe.
type Resource struct {
	ID          string
	Channel     string
	Layer       string
	ContentHash string
	Requires    []string
	Payload     map[string]any
}

// Change is one planned mutation of a single resource.
//
// Resource holds the target resource for the change: the desired resource for
// ChangeCreate, ChangeUpdate, and ChangeNoop; the prior resource for
// ChangeDelete. Providers use this to read the payload they must render.
type Change struct {
	Kind     ChangeKind
	ID       string
	Detail   string
	Resource Resource
}

// ChannelPlan is the set of changes one provider would make.
type ChannelPlan struct {
	Channel string
	Changes []Change
}

// Empty reports whether the plan would change nothing.
func (p ChannelPlan) Empty() bool {
	for _, c := range p.Changes {
		if c.Kind != ChangeNoop {
			return false
		}
	}
	return true
}

// ChangeFailure is one Change whose apply was attempted and did not succeed.
type ChangeFailure struct {
	Change Change
	Err    error
}

// ChangeSkip is one Change deliberately not attempted because a resource it
// requires failed earlier in the same apply run.
type ChangeSkip struct {
	Change Change
	Reason string
}

// ApplyResult records what a provider's Apply actually did. Applied holds the
// changes that succeeded; Failed holds changes attempted that errored; Skipped
// holds changes the orchestrator blocked before the provider saw them.
type ApplyResult struct {
	Channel string
	Applied []Change
	Failed  []ChangeFailure
	Skipped []ChangeSkip
}

// Provider reconciles one channel. Observe reads machine state; Apply mutates
// it. The diff between desired and observed is channel-agnostic and is computed
// by the shared DiffResources function, not by the provider.
type Provider interface {
	Channel() string
	Observe(env Env) ([]Resource, error)
	Apply(env Env, plan ChannelPlan) (ApplyResult, error)
}

// ApplyError aggregates the per-resource failures of a partial apply. When it
// is returned the applied ledger has already been written for everything that
// succeeded.
type ApplyError struct {
	Errs []error
}

// Error summarizes the failures. The full per-resource list is on Errs.
func (e *ApplyError) Error() string {
	if len(e.Errs) == 0 {
		return "apply failed"
	}
	if len(e.Errs) == 1 {
		return e.Errs[0].Error()
	}
	return fmt.Sprintf("%d resources failed to apply", len(e.Errs))
}

// Unwrap exposes the per-resource errors to errors.Is and errors.As.
func (e *ApplyError) Unwrap() []error { return e.Errs }
