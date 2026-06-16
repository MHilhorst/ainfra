// Package secret resolves manifest secret references (op://, env://, ...) to
// values at session time. It never stores, caches, or writes a resolved value.
package secret

import (
	"fmt"
	"sort"
	"strings"
)

// Resolver turns one ref scheme into a credential value.
type Resolver interface {
	// Scheme is the URI scheme this resolver handles, e.g. "op", "env".
	Scheme() string
	// Resolve returns the secret value for ref. The value is held in memory
	// and never logged. ref is the full URI including its scheme.
	Resolve(ref string) (string, error)
	// Check verifies ref is resolvable without returning or exposing the value.
	Check(ref string) error
}

// Availabler is an optional Resolver capability: a backend that can verify it
// is ready (CLI installed, session valid) without resolving a specific ref.
// Used as an up-front preflight so an unusable backend fails before any config
// is written, instead of mid-apply on the first ref it cannot resolve.
type Availabler interface {
	Available() error
}

// Registry dispatches a ref to the Resolver registered for its scheme.
type Registry struct {
	resolvers map[string]Resolver
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{resolvers: map[string]Resolver{}}
}

// Add registers r under its scheme, replacing any prior resolver.
func (reg *Registry) Add(r Resolver) { reg.resolvers[r.Scheme()] = r }

// SchemeOf returns the scheme of a "scheme://rest" reference.
func SchemeOf(ref string) (string, error) {
	i := strings.Index(ref, "://")
	if i <= 0 {
		return "", fmt.Errorf("secret ref %q has no scheme", ref)
	}
	return ref[:i], nil
}

func (reg *Registry) schemes() string {
	out := make([]string, 0, len(reg.resolvers))
	for s := range reg.resolvers {
		out = append(out, s)
	}
	sort.Strings(out)
	return strings.Join(out, ", ")
}

func (reg *Registry) resolverFor(ref string) (Resolver, error) {
	scheme, err := SchemeOf(ref)
	if err != nil {
		return nil, err
	}
	r, ok := reg.resolvers[scheme]
	if !ok {
		return nil, fmt.Errorf("secret ref %q: unknown scheme %q (registered: %s)", ref, scheme, reg.schemes())
	}
	return r, nil
}

// Resolve dispatches ref to its scheme's resolver.
func (reg *Registry) Resolve(ref string) (string, error) {
	r, err := reg.resolverFor(ref)
	if err != nil {
		return "", err
	}
	return r.Resolve(ref)
}

// Check dispatches ref to its scheme's resolver for verification.
func (reg *Registry) Check(ref string) error {
	r, err := reg.resolverFor(ref)
	if err != nil {
		return err
	}
	return r.Check(ref)
}

// CheckBackend verifies the backend for scheme is ready, when its resolver
// supports a readiness probe (implements Availabler). A scheme with no probe,
// or an unregistered scheme, returns nil — readiness is unknowable, so it is
// not treated as a failure.
func (reg *Registry) CheckBackend(scheme string) error {
	r, ok := reg.resolvers[scheme]
	if !ok {
		return nil
	}
	if a, ok := r.(Availabler); ok {
		return a.Available()
	}
	return nil
}
