package resolve

import (
	"sort"

	"github.com/MHilhorst/ainfra/internal/manifest"
)

// MergeTools folds the tools channel across config layers (spec §1.1).
//
// Unlike the id-keyed channels that Merge arbitrates, tools is a singleton
// whose list fields are *additive*: a lower-authority layer extends a higher
// layer's lists but can never shrink them. That is the deliberate
// freedom/guardrail balance — a developer may add permissions for their own
// tooling, yet cannot lift a team deny or re-enable a team-disabled built-in.
//
// layers may be given in any order; the union is order-independent. After the
// union, a pattern present in more than one permission tier is resolved to the
// strictest tier: deny beats ask beats allow. Output lists are sorted, so the
// result is stable for lockfile hashing.
func MergeTools(layers ...*manifest.Tools) *manifest.Tools {
	var disabled, allow, ask, deny []string
	present := false
	for _, t := range layers {
		if t == nil {
			continue
		}
		present = true
		if t.Builtins != nil {
			disabled = append(disabled, t.Builtins.Disabled...)
		}
		if t.Permissions != nil {
			allow = append(allow, t.Permissions.Allow...)
			ask = append(ask, t.Permissions.Ask...)
			deny = append(deny, t.Permissions.Deny...)
		}
	}
	if !present {
		return nil
	}
	// Strictness cascade: a pattern in deny is dropped from ask and allow; a
	// pattern in ask is dropped from allow.
	deny = dedup(deny)
	ask = without(dedup(ask), deny)
	allow = without(without(dedup(allow), deny), ask)
	return &manifest.Tools{
		Builtins:    &manifest.Builtins{Disabled: dedup(disabled)},
		Permissions: &manifest.Permissions{Allow: allow, Ask: ask, Deny: deny},
	}
}

// dedup returns the sorted, unique members of in.
func dedup(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// without returns the members of in (already sorted) absent from remove.
func without(in, remove []string) []string {
	drop := map[string]bool{}
	for _, s := range remove {
		drop[s] = true
	}
	out := []string{}
	for _, s := range in {
		if !drop[s] {
			out = append(out, s)
		}
	}
	return out
}
