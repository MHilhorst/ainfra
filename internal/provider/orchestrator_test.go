package provider

import (
	"os"
	"path/filepath"
	"strings"
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

func TestOrchestratorBackfillsObservedHash(t *testing.T) {
	root := t.TempDir()

	// Write an applied ledger with one skills entry that has a known hash.
	priorLock := &lockfile.Lock{
		Version: 1,
		Entries: lockfile.Entries{
			Skills: map[string]lockfile.Entry{
				"s": {Layer: "repo", ContentHash: "sha256:abc"},
			},
		},
	}
	if err := WriteApplied(root, priorLock); err != nil {
		t.Fatalf("WriteApplied: %v", err)
	}

	// stubProvider whose Observe returns a resource with empty ContentHash.
	skills := &stubProvider{
		channel: "skills",
		observed: []Resource{
			{ID: "s", Channel: "skills"}, // ContentHash intentionally empty
		},
	}

	o := NewOrchestrator(root, Env{}, []Provider{skills})

	// Case 1: desired hash matches prior -> should be Noop -> plan is Empty.
	desiredMatch := &lockfile.Lock{
		Version: 1,
		Entries: lockfile.Entries{
			Skills: map[string]lockfile.Entry{
				"s": {Layer: "repo", ContentHash: "sha256:abc"},
			},
		},
	}
	plan, err := o.PlanAll(desiredMatch)
	if err != nil {
		t.Fatalf("PlanAll (match): %v", err)
	}
	if !plan["skills"].Empty() {
		t.Errorf("expected empty plan when desired hash matches backfilled hash, got %+v", plan["skills"])
	}

	// Case 2: desired hash differs from prior -> should be Update -> plan is NOT Empty.
	desiredDiffer := &lockfile.Lock{
		Version: 1,
		Entries: lockfile.Entries{
			Skills: map[string]lockfile.Entry{
				"s": {Layer: "repo", ContentHash: "sha256:xyz"},
			},
		},
	}
	plan2, err := o.PlanAll(desiredDiffer)
	if err != nil {
		t.Fatalf("PlanAll (differ): %v", err)
	}
	if plan2["skills"].Empty() {
		t.Errorf("expected non-empty plan when desired hash differs from backfilled hash, got empty")
	}
}

func TestApplyAllRenderedDryRunSkipsLedger(t *testing.T) {
	root := t.TempDir()
	skills := &stubProvider{channel: "skills"} // observes nothing -> "s" is a create
	o := NewOrchestrator(root, Env{DryRun: true}, []Provider{skills})

	rendered := map[string][]Resource{
		"skills": {{ID: "s", Channel: "skills", ContentHash: "h1"}},
	}
	if err := o.ApplyAllRendered(rendered, newTestLock()); err != nil {
		t.Fatalf("ApplyAllRendered (dry run): %v", err)
	}
	if len(skills.applied) != 1 {
		t.Errorf("dry run should still call provider Apply, got %+v", skills.applied)
	}
	if _, err := os.Stat(filepath.Join(root, ".ainfra", "applied.lock")); !os.IsNotExist(err) {
		t.Errorf("dry run wrote the applied ledger; want it skipped (stat err = %v)", err)
	}
}

func TestApplyAllRenderedWritesLedgerWhenNotDryRun(t *testing.T) {
	root := t.TempDir()
	skills := &stubProvider{channel: "skills"}
	o := NewOrchestrator(root, Env{}, []Provider{skills})

	rendered := map[string][]Resource{
		"skills": {{ID: "s", Channel: "skills", ContentHash: "h1"}},
	}
	if err := o.ApplyAllRendered(rendered, newTestLock()); err != nil {
		t.Fatalf("ApplyAllRendered: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".ainfra", "applied.lock")); err != nil {
		t.Errorf("non-dry-run apply did not write the applied ledger: %v", err)
	}
}

func TestSplitBlocked(t *testing.T) {
	plan := ChannelPlan{
		Channel: "mcpServers",
		Changes: []Change{
			{Kind: ChangeCreate, ID: "free", Resource: Resource{ID: "free"}},
			{Kind: ChangeCreate, ID: "blocked", Resource: Resource{ID: "blocked", Requires: []string{"cli:ssh"}}},
			{Kind: ChangeNoop, ID: "noop", Resource: Resource{ID: "noop", Requires: []string{"cli:ssh"}}},
		},
	}
	failedRefs := map[string]bool{"cli:ssh": true}

	runnable, skipped := splitBlocked(plan, failedRefs)

	gotRunnable := []string{}
	for _, c := range runnable.Changes {
		gotRunnable = append(gotRunnable, c.ID)
	}
	// "free" runs; "blocked" is skipped; "noop" stays runnable (a noop is free).
	if len(gotRunnable) != 2 || gotRunnable[0] != "free" || gotRunnable[1] != "noop" {
		t.Errorf("runnable = %v, want [free noop]", gotRunnable)
	}
	if len(skipped) != 1 || skipped[0].Change.ID != "blocked" {
		t.Fatalf("skipped = %+v, want one skip for 'blocked'", skipped)
	}
	if !strings.Contains(skipped[0].Reason, "cli:ssh") {
		t.Errorf("skip reason should name the failed dependency, got %q", skipped[0].Reason)
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
