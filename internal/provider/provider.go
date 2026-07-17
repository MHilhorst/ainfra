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
	// ChangeRefresh is a mutation the resource asked for unconditionally
	// rather than one drift made necessary: the desired resource is marked
	// AlwaysRefresh, so a hash difference is the expected steady state and
	// not a signal that anything has diverged. Applied exactly like
	// ChangeUpdate; it exists to keep "will be refreshed every run" from
	// reading as "has drifted", which is a claim about the machine.
	ChangeRefresh
)

// String renders a ChangeKind as the one-character plan symbol. ChangeRefresh
// shares the "~" mutation symbol with ChangeUpdate — both change the machine;
// they differ in why, which the detail phrase carries.
func (k ChangeKind) String() string {
	switch k {
	case ChangeCreate:
		return "+"
	case ChangeUpdate, ChangeRefresh:
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
	// Tombstone marks a desired resource as "ensure absent": an explicit
	// instruction (enabled: false in the manifest) to remove it from the
	// machine if present, distinct from a resource the manifest simply does
	// not mention (which is left untouched). DiffResources never creates or
	// updates a tombstone.
	Tombstone bool
	// AlwaysRefresh marks a desired resource whose ContentHash is not
	// expected to ever equal the observed hash, because the manifest pins no
	// version and the upstream one is therefore authoritative. The mismatch
	// is the design, not drift: the provider re-runs its update every time so
	// the resource tracks upstream. DiffResources emits ChangeRefresh instead
	// of ChangeUpdate for these, so the plan does not report permanent,
	// unfixable divergence for a resource behaving exactly as configured.
	AlwaysRefresh bool
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
	// Prune marks a delete synthesized because the resource is untracked —
	// present on the machine but in neither the manifest nor the applied
	// ledger — rather than one the manifest asked for. The orchestrator uses
	// it to find the deletes subject to the offered-ledger guard and to back
	// them up. It is a structural flag rather than a Detail string match,
	// which would be fragile.
	Prune bool
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

// ChangeWarning is one Change that succeeded but with a non-fatal condition
// worth surfacing — for example, a post-install version mismatch where Claude
// Code resolved a different version than the manifest pinned.
type ChangeWarning struct {
	Change Change
	Reason string
}

// ApplyResult records what a provider's Apply actually did. Applied holds the
// changes that succeeded; Failed holds changes attempted that errored; Skipped
// holds changes the orchestrator blocked before the provider saw them;
// Warnings holds changes that succeeded with a non-fatal caveat.
type ApplyResult struct {
	Channel  string
	Applied  []Change
	Failed   []ChangeFailure
	Skipped  []ChangeSkip
	Warnings []ChangeWarning
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
// is returned the applied ledger has been written for everything that
// succeeded — unless the apply was a dry run, which writes no ledger.
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
