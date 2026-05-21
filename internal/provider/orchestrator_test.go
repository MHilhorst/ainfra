package provider

import (
	"path/filepath"
	"testing"

	"github.com/MHilhorst/ainfra/internal/lockfile"
)

// recordingProvider records the order in which Apply is called by appending to
// the shared slice.
type recordingProvider struct {
	channel string
	order   *[]string
}

func (r *recordingProvider) Channel() string                 { return r.channel }
func (r *recordingProvider) Observe(Env) ([]Resource, error) { return []Resource{}, nil }
func (r *recordingProvider) Apply(_ Env, p ChannelPlan) (ApplyResult, error) {
	*r.order = append(*r.order, r.channel)
	return ApplyResult{Channel: r.channel, Applied: p.Changes}, nil
}

// stubProvider observes a fixed resource set and records applies.
type stubProvider struct {
	channel  string
	observed []Resource
	applied  []ChannelPlan
}

func (s *stubProvider) Channel() string                 { return s.channel }
func (s *stubProvider) Observe(Env) ([]Resource, error) { return s.observed, nil }
func (s *stubProvider) Apply(_ Env, p ChannelPlan) (ApplyResult, error) {
	s.applied = append(s.applied, p)
	return ApplyResult{Channel: s.channel, Applied: p.Changes}, nil
}

func newTestLock() *lockfile.Lock {
	return &lockfile.Lock{Version: 1, Entries: lockfile.Entries{
		Skills: map[string]lockfile.Entry{"s": {Layer: "repo", ContentHash: "h1"}},
	}}
}

func TestOrchestratorPlanAndApply(t *testing.T) {
	root := t.TempDir()
	skills := &stubProvider{channel: "skills"} // observes nothing -> "s" is a create
	o := NewOrchestrator(root, Env{}, []Provider{skills})

	plan, err := o.PlanAll(newTestLock())
	if err != nil {
		t.Fatal(err)
	}
	if plan["skills"].Empty() {
		t.Fatalf("expected a create for skill s, got %+v", plan["skills"])
	}

	if err := o.ApplyAll(newTestLock()); err != nil {
		t.Fatalf("ApplyAll: %v", err)
	}
	if len(skills.applied) != 1 {
		t.Errorf("provider Apply not called: %+v", skills.applied)
	}
	if _, err := lockfile.Read(filepath.Join(root, ".ainfra", "applied.lock")); err != nil {
		t.Errorf("applied ledger not written: %v", err)
	}
}

func TestOrchestratorChannelOrder(t *testing.T) {
	root := t.TempDir()
	applyOrder := &[]string{}

	// Register mcpServers before cliTools intentionally — the orchestrator must
	// still process cliTools first per channelOrder.
	mcp := &recordingProvider{channel: "mcpServers", order: applyOrder}
	cli := &recordingProvider{channel: "cliTools", order: applyOrder}

	// Build a lock that has entries for both channels so Apply is called.
	lock := &lockfile.Lock{
		Version: 1,
		Entries: lockfile.Entries{
			MCPServers: map[string]lockfile.Entry{"srv": {Layer: "repo", ContentHash: "h1"}},
			CLITools:   map[string]lockfile.Entry{"tool": {Layer: "repo", ContentHash: "h2"}},
		},
	}

	o := NewOrchestrator(root, Env{}, []Provider{mcp, cli})
	if err := o.ApplyAll(lock); err != nil {
		t.Fatal(err)
	}

	if len(*applyOrder) != 2 {
		t.Fatalf("expected 2 applies, got %v", *applyOrder)
	}
	if (*applyOrder)[0] != "cliTools" || (*applyOrder)[1] != "mcpServers" {
		t.Errorf("wrong apply order: got %v, want [cliTools mcpServers]", *applyOrder)
	}
}
