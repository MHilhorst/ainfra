// Package graph builds and orders the aistack dependency graph (spec §9).
package graph

import (
	"fmt"
	"sort"
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

// AddEdge records that from requires to.
func (g *Graph) AddEdge(from, to string) { g.deps[from] = append(g.deps[from], to) }

// TopoSort returns nodes leaves-first (dependencies before dependents).
// It errors on a cycle or an edge to an unknown node.
func (g *Graph) TopoSort() ([]string, error) {
	const (
		white = 0 // unvisited
		gray  = 1 // on the current DFS stack
		black = 2 // finished
	)
	state := map[string]int{}
	var order []string
	var visit func(string) error
	visit = func(n string) error {
		switch state[n] {
		case gray:
			return fmt.Errorf("dependency cycle through %q", n)
		case black:
			return nil
		}
		state[n] = gray
		deps := append([]string(nil), g.deps[n]...)
		sort.Strings(deps) // deterministic output
		for _, d := range deps {
			if !g.nodes[d] {
				return fmt.Errorf("node %q requires unknown node %q", n, d)
			}
			if err := visit(d); err != nil {
				return err
			}
		}
		state[n] = black
		order = append(order, n)
		return nil
	}
	ids := make([]string, 0, len(g.nodes))
	for n := range g.nodes {
		ids = append(ids, n)
	}
	sort.Strings(ids)
	for _, n := range ids {
		if err := visit(n); err != nil {
			return nil, err
		}
	}
	return order, nil
}
