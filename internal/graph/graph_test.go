package graph

import "testing"

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
	if _, err := g.TopoSort(); err == nil {
		t.Fatal("want cycle error")
	}
}
