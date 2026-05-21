// Package provider defines the channel reconciliation contract and the shared
// machinery (diff, environment, orchestration) every channel provider uses.
package provider

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
type Change struct {
	Kind   ChangeKind
	ID     string
	Detail string
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

// ApplyResult records what a provider's Apply actually did.
type ApplyResult struct {
	Channel string
	Applied []Change
}

// Provider reconciles one channel. Observe reads machine state; Apply mutates
// it. The diff between desired and observed is channel-agnostic and is computed
// by the shared DiffResources function, not by the provider.
type Provider interface {
	Channel() string
	Observe(env Env) ([]Resource, error)
	Apply(env Env, plan ChannelPlan) (ApplyResult, error)
}
