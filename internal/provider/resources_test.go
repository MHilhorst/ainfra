package provider

import (
	"testing"

	"github.com/MHilhorst/ainfra/internal/lockfile"
)

func TestResourcesByChannel(t *testing.T) {
	l := &lockfile.Lock{Version: 1, Entries: lockfile.Entries{
		Skills: map[string]lockfile.Entry{"s": {Layer: "repo", ContentHash: "h", Requires: []string{"cli:node"}}},
		Hooks:  map[string]lockfile.Entry{"h": {Layer: "team", ContentHash: "hh"}},
	}}
	got := ResourcesByChannel(l)
	if len(got["skills"]) != 1 || got["skills"][0].ID != "s" || got["skills"][0].ContentHash != "h" {
		t.Errorf("skills = %+v", got["skills"])
	}
	if got["skills"][0].Requires[0] != "cli:node" {
		t.Errorf("requires not carried: %+v", got["skills"][0])
	}
	if len(got["hooks"]) != 1 {
		t.Errorf("hooks = %+v", got["hooks"])
	}
}

func TestApplyOrderRespectsRequires(t *testing.T) {
	l := &lockfile.Lock{Version: 1, Entries: lockfile.Entries{
		MCPServers: map[string]lockfile.Entry{
			"db": {Layer: "repo", ContentHash: "h", Requires: []string{"svc:tunnel"}},
		},
		BackgroundServices: map[string]lockfile.Entry{
			"tunnel": {Layer: "repo", ContentHash: "h2"},
		},
	}}
	order, err := ApplyOrder(l)
	if err != nil {
		t.Fatal(err)
	}
	pos := map[string]int{}
	for i, n := range order {
		pos[n] = i
	}
	if pos["svc:tunnel"] > pos["mcp:db"] {
		t.Errorf("svc:tunnel must come before mcp:db; order = %v", order)
	}
}
