package graph

import (
	"strings"
	"testing"
)

func TestTopoSortLeavesFirst(t *testing.T) {
	g := New()
	g.AddNode("mcp")
	g.AddNode("tunnel")
	g.AddNode("ssh")
	g.AddEdge("mcp", "tunnel") // mcp requires tunnel
	g.AddEdge("tunnel", "ssh") // tunnel requires ssh

	order, err := g.TopoSort()
	if err != nil {
		t.Fatalf("TopoSort: %v", err)
	}
	pos := map[string]int{}
	for i, n := range order {
		pos[n] = i
	}
	if !(pos["ssh"] < pos["tunnel"] && pos["tunnel"] < pos["mcp"]) {
		t.Errorf("order not leaves-first: %v", order)
	}
}

func TestTopoSortDetectsCycle(t *testing.T) {
	g := New()
	g.AddNode("a")
	g.AddNode("b")
	g.AddEdge("a", "b")
	g.AddEdge("b", "a")
	_, err := g.TopoSort()
	if err == nil {
		t.Fatal("want cycle error")
	}
	if !strings.Contains(err.Error(), "->") {
		t.Errorf("cycle error should show the path, got %q", err)
	}
}

func TestTopoSortRejectsUnknownDestination(t *testing.T) {
	g := New()
	g.AddNode("a")
	g.AddEdge("a", "ghost") // ghost was never AddNode'd
	if _, err := g.TopoSort(); err == nil {
		t.Fatal("want error for edge to unknown node")
	}
}

func TestTopoSortRejectsUnregisteredSource(t *testing.T) {
	g := New()
	g.AddNode("b")
	g.AddEdge("ghost", "b") // ghost was never AddNode'd
	if _, err := g.TopoSort(); err == nil {
		t.Fatal("want error for edge from unregistered node")
	}
}

func TestTopoSortDiamond(t *testing.T) {
	g := New()
	for _, n := range []string{"a", "b", "c", "d"} {
		g.AddNode(n)
	}
	g.AddEdge("a", "b")
	g.AddEdge("a", "c")
	g.AddEdge("b", "d")
	g.AddEdge("c", "d")
	order, err := g.TopoSort()
	if err != nil {
		t.Fatalf("TopoSort: %v", err)
	}
	if len(order) != 4 {
		t.Fatalf("want 4 nodes once each, got %v", order)
	}
	pos := map[string]int{}
	for i, n := range order {
		pos[n] = i
	}
	if !(pos["d"] < pos["b"] && pos["d"] < pos["c"] && pos["b"] < pos["a"] && pos["c"] < pos["a"]) {
		t.Errorf("diamond order wrong: %v", order)
	}
}

func TestTopoSortEmptyGraph(t *testing.T) {
	order, err := New().TopoSort()
	if err != nil {
		t.Fatalf("TopoSort: %v", err)
	}
	if len(order) != 0 {
		t.Errorf("want empty order, got %v", order)
	}
}
