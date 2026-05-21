// Package graph builds and orders the ainfra dependency graph (spec §9).
package graph

import (
	"fmt"
	"sort"
	"strings"
)

// Graph is a dependency graph. An edge from -> to means "from requires to".
type Graph struct {
	nodes map[string]bool
	deps  map[string][]string
}

// New returns an empty graph.
func New() *Graph {
	return &Graph{nodes: map[string]bool{}, deps: map[string][]string{}}
}

// AddNode registers a node. Idempotent.
func (g *Graph) AddNode(id string) { g.nodes[id] = true }

// AddEdge records that from requires to. Both endpoints must be registered
// with AddNode; TopoSort reports an edge whose endpoints are not.
func (g *Graph) AddEdge(from, to string) { g.deps[from] = append(g.deps[from], to) }

// TopoSort returns nodes leaves-first (dependencies before dependents). It
// errors on a cycle (naming the full cycle path) or on an edge whose endpoints
// are not registered nodes.
func (g *Graph) TopoSort() ([]string, error) {
	// Validate every edge endpoint is a registered node. Iterating sorted
	// keys keeps the reported error deterministic.
	for _, from := range sortedKeys(g.deps) {
		if !g.nodes[from] {
			return nil, fmt.Errorf("edge from unregistered node %q", from)
		}
		for _, to := range sortedCopy(g.deps[from]) {
			if !g.nodes[to] {
				return nil, fmt.Errorf("node %q requires unknown node %q", from, to)
			}
		}
	}

	const (
		white = 0 // unvisited
		gray  = 1 // on the current DFS stack
		black = 2 // finished
	)
	state := map[string]int{}
	var order []string
	var visit func(n string, path []string) error
	visit = func(n string, path []string) error {
		switch state[n] {
		case gray:
			return fmt.Errorf("dependency cycle: %s", strings.Join(cyclePath(path, n), " -> "))
		case black:
			return nil
		}
		state[n] = gray
		next := append(append([]string(nil), path...), n)
		for _, d := range sortedCopy(g.deps[n]) {
			if err := visit(d, next); err != nil {
				return err
			}
		}
		state[n] = black
		order = append(order, n)
		return nil
	}
	for _, n := range sortedKeys(g.nodes) {
		if err := visit(n, nil); err != nil {
			return nil, err
		}
	}
	return order, nil
}

// cyclePath returns the cycle: the segment of path from the first occurrence
// of n, with n appended to close the loop.
func cyclePath(path []string, n string) []string {
	start := 0
	for i, p := range path {
		if p == n {
			start = i
			break
		}
	}
	return append(append([]string(nil), path[start:]...), n)
}

// sortedCopy returns a sorted copy of s, leaving s untouched.
func sortedCopy(s []string) []string {
	out := append([]string(nil), s...)
	sort.Strings(out)
	return out
}

// sortedKeys returns the keys of m in sorted order.
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
