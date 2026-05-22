package provider

import (
	"errors"
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
	if _, err := o.ApplyAllRendered(rendered, newTestLock()); err != nil {
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
	if _, err := o.ApplyAllRendered(rendered, newTestLock()); err != nil {
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

func TestNodeRef(t *testing.T) {
	cases := []struct {
		channel string
		id      string
		want    string
	}{
		// channelPrefix["cliTools"] == "cli"
		{"cliTools", "ssh", "cli:ssh"},
		// channelPrefix["backgroundServices"] == "svc"
		{"backgroundServices", "db", "svc:db"},
		// channelPrefix["tools"] == "tools" (prefix equals channel name)
		{"tools", "x", "tools:x"},
		// unknown channel falls through to default: channel + ":" + id
		{"custom", "x", "custom:x"},
	}
	for _, tc := range cases {
		got := nodeRef(tc.channel, tc.id)
		if got != tc.want {
			t.Errorf("nodeRef(%q, %q) = %q, want %q", tc.channel, tc.id, got, tc.want)
		}
	}
}

func TestBuildLedgerFailedFallsBackToPrior(t *testing.T) {
	prior := &lockfile.Lock{Version: 1, Entries: lockfile.Entries{
		Skills:   map[string]lockfile.Entry{"s": {Layer: "repo", ContentHash: "old"}},
		CLITools: map[string]lockfile.Entry{},
	}}
	desired := &lockfile.Lock{Version: 1, Entries: lockfile.Entries{
		Skills:   map[string]lockfile.Entry{"s": {Layer: "repo", ContentHash: "new"}},
		CLITools: map[string]lockfile.Entry{"x": {Layer: "repo", ContentHash: "h"}},
	}}
	results := []ApplyResult{
		{Channel: "skills", Applied: []Change{{Kind: ChangeUpdate, ID: "s"}}},
		{Channel: "cliTools", Failed: []ChangeFailure{{Change: Change{Kind: ChangeCreate, ID: "x"}}}},
	}

	ledger := buildLedger(prior, desired, results)

	// "s" succeeded -> desired entry ("new").
	if got := ledger.Entries.Skills["s"].ContentHash; got != "new" {
		t.Errorf("skills[s] hash = %q, want %q (succeeded -> desired)", got, "new")
	}
	// "x" failed to create and had no prior entry -> absent.
	if _, ok := ledger.Entries.CLITools["x"]; ok {
		t.Errorf("cliTools[x] present; want absent (failed create with no prior)")
	}
}

func TestBuildLedgerNoFailuresEqualsDesired(t *testing.T) {
	prior := &lockfile.Lock{Version: 1, Entries: lockfile.Entries{
		Skills: map[string]lockfile.Entry{"s": {Layer: "repo", ContentHash: "old"}},
	}}
	desired := &lockfile.Lock{Version: 1, Entries: lockfile.Entries{
		Skills: map[string]lockfile.Entry{"s": {Layer: "repo", ContentHash: "new"}},
	}}
	results := []ApplyResult{
		{Channel: "skills", Applied: []Change{{Kind: ChangeUpdate, ID: "s"}}},
	}

	ledger := buildLedger(prior, desired, results)
	if got := ledger.Entries.Skills["s"].ContentHash; got != "new" {
		t.Errorf("skills[s] hash = %q, want %q (no failures -> desired)", got, "new")
	}
}

// TestBuildLedgerCarriesMarketplaces guards against the marketplaces channel
// being dropped from the ledger — if it is, every apply re-detects drift.
func TestBuildLedgerCarriesMarketplaces(t *testing.T) {
	prior := &lockfile.Lock{Version: 1}
	desired := &lockfile.Lock{Version: 1, Entries: lockfile.Entries{
		Marketplaces: map[string]lockfile.Entry{"trein-vertraging": {Layer: "repo", ContentHash: "h"}},
	}}
	results := []ApplyResult{
		{Channel: "marketplaces", Applied: []Change{{Kind: ChangeCreate, ID: "trein-vertraging"}}},
	}

	ledger := buildLedger(prior, desired, results)
	if got := ledger.Entries.Marketplaces["trein-vertraging"].ContentHash; got != "h" {
		t.Errorf("marketplaces[trein-vertraging] hash = %q, want %q (must be recorded)", got, "h")
	}
}

func TestBuildLedgerFailedDeleteKeepsPriorEntry(t *testing.T) {
	prior := &lockfile.Lock{Version: 1, Entries: lockfile.Entries{
		Skills: map[string]lockfile.Entry{"old": {Layer: "repo", ContentHash: "h"}},
	}}
	desired := &lockfile.Lock{Version: 1, Entries: lockfile.Entries{
		Skills: map[string]lockfile.Entry{},
	}}
	results := []ApplyResult{
		{Channel: "skills", Failed: []ChangeFailure{{Change: Change{Kind: ChangeDelete, ID: "old"}}}},
	}

	ledger := buildLedger(prior, desired, results)

	// "old" was deleted from desired but the delete failed -> prior entry must be
	// retained because the resource is still present on the machine.
	entry, ok := ledger.Entries.Skills["old"]
	if !ok {
		t.Fatal("skills[old] absent; want prior entry retained after failed delete")
	}
	if entry.ContentHash != "h" {
		t.Errorf("skills[old].ContentHash = %q, want %q", entry.ContentHash, "h")
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

// scriptedProvider returns a fixed ApplyResult/error and records the plan it
// received, so tests can drive orchestrator failure paths.
type scriptedProvider struct {
	channel  string
	observed []Resource
	result   ApplyResult
	err      error
	gotPlan  ChannelPlan
}

func (s *scriptedProvider) Channel() string                 { return s.channel }
func (s *scriptedProvider) Observe(Env) ([]Resource, error) { return s.observed, nil }
func (s *scriptedProvider) Apply(_ Env, p ChannelPlan) (ApplyResult, error) {
	s.gotPlan = p
	r := s.result
	r.Channel = s.channel
	return r, s.err
}

func TestApplyAllRenderedContinuesPastChannelFailure(t *testing.T) {
	root := t.TempDir()
	// cliTools is processed before skills; cliTools errors catastrophically.
	cliTools := &scriptedProvider{channel: "cliTools", err: errors.New("brew is broken")}
	skills := &scriptedProvider{channel: "skills"}
	o := NewOrchestrator(root, Env{}, []Provider{cliTools, skills})

	rendered := map[string][]Resource{
		"cliTools": {{ID: "x", Channel: "cliTools", ContentHash: "h"}},
		"skills":   {{ID: "s", Channel: "skills", ContentHash: "h"}},
	}
	results, err := o.ApplyAllRendered(rendered, newTestLock())
	if err == nil {
		t.Fatal("expected an aggregated error, got nil")
	}
	if _, ok := err.(*ApplyError); !ok {
		t.Errorf("error type = %T, want *ApplyError", err)
	}
	// skills must still have been applied despite cliTools failing.
	if skills.gotPlan.Empty() {
		t.Error("skills channel was not applied after cliTools failed")
	}
	// cliTools must be reported with its catastrophic failure recorded.
	failedCli := false
	for _, r := range results {
		if r.Channel == "cliTools" && len(r.Failed) > 0 {
			failedCli = true
		}
	}
	if !failedCli {
		t.Errorf("cliTools not reported with a failure; results = %+v", results)
	}
}

func TestApplyAllRenderedSkipsBlockedDependents(t *testing.T) {
	root := t.TempDir()
	// cliTools fails resource "x"; mcpServers "m" requires "cli:x".
	cliTools := &scriptedProvider{
		channel: "cliTools",
		result: ApplyResult{
			Failed: []ChangeFailure{{Change: Change{Kind: ChangeCreate, ID: "x"}, Err: errors.New("install failed")}},
		},
	}
	mcp := &scriptedProvider{channel: "mcpServers"}
	o := NewOrchestrator(root, Env{}, []Provider{cliTools, mcp})

	rendered := map[string][]Resource{
		"cliTools":   {{ID: "x", Channel: "cliTools", ContentHash: "h"}},
		"mcpServers": {{ID: "m", Channel: "mcpServers", ContentHash: "h", Requires: []string{"cli:x"}}},
	}
	results, err := o.ApplyAllRendered(rendered, newTestLock())
	if err == nil {
		t.Fatal("expected an aggregated error, got nil")
	}
	// "m" must not have been handed to the mcpServers provider.
	for _, c := range mcp.gotPlan.Changes {
		if c.ID == "m" && c.Kind != ChangeNoop {
			t.Errorf("blocked resource 'm' was passed to the provider: %+v", c)
		}
	}
	// "m" must be reported as skipped.
	skippedM := false
	for _, r := range results {
		if r.Channel != "mcpServers" {
			continue
		}
		for _, s := range r.Skipped {
			if s.Change.ID == "m" {
				skippedM = true
			}
		}
	}
	if !skippedM {
		t.Errorf("resource 'm' not reported as skipped; results = %+v", results)
	}
}

func TestApplyAllRenderedWritesPartialLedger(t *testing.T) {
	root := t.TempDir()
	cliTools := &scriptedProvider{
		channel: "cliTools",
		result: ApplyResult{
			Failed: []ChangeFailure{{Change: Change{Kind: ChangeCreate, ID: "x"}, Err: errors.New("nope")}},
		},
	}
	skills := &scriptedProvider{
		channel:  "skills",
		observed: []Resource{{ID: "s", Channel: "skills"}}, // present -> "s" is an update
		result:   ApplyResult{Applied: []Change{{Kind: ChangeUpdate, ID: "s"}}},
	}
	o := NewOrchestrator(root, Env{}, []Provider{cliTools, skills})

	desired := &lockfile.Lock{Version: 1, Entries: lockfile.Entries{
		Skills:   map[string]lockfile.Entry{"s": {Layer: "repo", ContentHash: "new"}},
		CLITools: map[string]lockfile.Entry{"x": {Layer: "repo", ContentHash: "h"}},
	}}
	rendered := map[string][]Resource{
		"cliTools": {{ID: "x", Channel: "cliTools", ContentHash: "h"}},
		"skills":   {{ID: "s", Channel: "skills", ContentHash: "new"}},
	}
	if _, err := o.ApplyAllRendered(rendered, desired); err == nil {
		t.Fatal("expected an aggregated error, got nil")
	}

	ledger, err := lockfile.Read(filepath.Join(root, ".ainfra", "applied.lock"))
	if err != nil {
		t.Fatalf("ledger not written: %v", err)
	}
	if got := ledger.Entries.Skills["s"].ContentHash; got != "new" {
		t.Errorf("ledger skills[s] = %q, want %q (succeeded)", got, "new")
	}
	if _, ok := ledger.Entries.CLITools["x"]; ok {
		t.Errorf("ledger cliTools[x] present; want absent (failed create)")
	}
}
