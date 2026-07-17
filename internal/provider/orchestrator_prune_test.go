package provider

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/MHilhorst/ainfra/internal/lockfile"
)

func fixedNow() time.Time { return time.Date(2026, 7, 17, 11, 0, 0, 0, time.UTC) }

// prunableStub is a stubProvider that implements Pruner, so the orchestrator
// will pass DiffOpts{Prune} to its diff.
type prunableStub struct {
	channel   string
	observed  []Resource
	applied   []ChannelPlan
	backedUp  []string
	backupErr error
}

func (p *prunableStub) Channel() string                 { return p.channel }
func (p *prunableStub) Observe(Env) ([]Resource, error) { return p.observed, nil }
func (p *prunableStub) Apply(_ Env, plan ChannelPlan) (ApplyResult, error) {
	p.applied = append(p.applied, plan)
	return ApplyResult{Channel: p.channel, Applied: plan.Changes}, nil
}
func (p *prunableStub) Backup(_ Env, r Resource, _ string) error {
	if p.backupErr != nil {
		return p.backupErr
	}
	p.backedUp = append(p.backedUp, r.ID)
	return nil
}

func emptyLock() *lockfile.Lock {
	return &lockfile.Lock{Version: 1, Entries: lockfile.Entries{
		MCPServers:         map[string]lockfile.Entry{},
		BackgroundServices: map[string]lockfile.Entry{},
		Hooks:              map[string]lockfile.Entry{},
		Commands:           map[string]lockfile.Entry{},
		CLITools:           map[string]lockfile.Entry{},
		Skills:             map[string]lockfile.Entry{},
		Marketplaces:       map[string]lockfile.Entry{},
		Plugins:            map[string]lockfile.Entry{},
		Rules:              map[string]lockfile.Entry{},
		Tools:              map[string]lockfile.Entry{},
	}}
}

func deletesFor(plan ChannelPlan, id string) int {
	n := 0
	for _, c := range plan.Changes {
		if c.ID == id && c.Kind == ChangeDelete {
			n++
		}
	}
	return n
}

// setupPrune wires a repo-scope orchestrator with one pruneable provider
// observing an untracked resource.
func setupPrune(t *testing.T, scope Scope, ch string, observed []Resource) (*Orchestrator, *prunableStub, *MemFilesystem) {
	t.Helper()
	mem := NewMemFilesystem()
	p := &prunableStub{channel: ch, observed: observed}
	o := NewOrchestratorScoped(t.TempDir(), scope, Env{FS: mem}, []Provider{p})
	o.EnablePrune(fixedNow)
	return o, p, mem
}

// The headline guarantee: an untracked resource the user has never been shown
// is reported, not deleted.
func TestPruneFirstRunOffersAndDoesNotDelete(t *testing.T) {
	o, _, _ := setupPrune(t, ScopeRepo, "skills", []Resource{{ID: "stray", Channel: "skills"}})

	plans, err := o.PlanAllRendered(map[string][]Resource{})
	if err != nil {
		t.Fatal(err)
	}
	if n := deletesFor(plans["skills"], "stray"); n != 0 {
		t.Errorf("delete count = %d, want 0; prune must never delete on first sight", n)
	}
	offered := o.NewlyOffered()
	if len(offered) != 1 || offered[0].ID != "stray" {
		t.Errorf("NewlyOffered = %+v, want one entry for stray", offered)
	}
}

// Once the user has been shown an entry and left it undeclared, a later run
// removes it.
func TestPruneSecondRunDeletes(t *testing.T) {
	o, _, mem := setupPrune(t, ScopeRepo, "skills", []Resource{{ID: "stray", Channel: "skills"}})
	seed := &OfferedLedger{Offered: map[string]OfferedEntry{
		"skills:stray": {FirstOfferedAt: "2026-07-16T10:00:00Z"},
	}}
	if err := WriteOffered(mem, mustOfferedRepo(t, o.root), seed); err != nil {
		t.Fatal(err)
	}

	plans, err := o.PlanAllRendered(map[string][]Resource{})
	if err != nil {
		t.Fatal(err)
	}
	if n := deletesFor(plans["skills"], "stray"); n != 1 {
		t.Errorf("delete count = %d, want 1; an entry offered on a previous run is armed", n)
	}
	if len(o.NewlyOffered()) != 0 {
		t.Errorf("NewlyOffered = %+v, want empty", o.NewlyOffered())
	}
}

// Declaring an entry between runs must spare it, even though it was offered.
func TestPruneDeclaredBetweenRunsIsSpared(t *testing.T) {
	o, _, mem := setupPrune(t, ScopeRepo, "skills", []Resource{{ID: "stray", Channel: "skills"}})
	seed := &OfferedLedger{Offered: map[string]OfferedEntry{
		"skills:stray": {FirstOfferedAt: "2026-07-16T10:00:00Z"},
	}}
	if err := WriteOffered(mem, mustOfferedRepo(t, o.root), seed); err != nil {
		t.Fatal(err)
	}

	// The user declared it: it is now desired.
	plans, err := o.PlanAllRendered(map[string][]Resource{
		"skills": {{ID: "stray", Channel: "skills"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if n := deletesFor(plans["skills"], "stray"); n != 0 {
		t.Errorf("delete count = %d, want 0; a declared resource must never be pruned", n)
	}
}

// A preview must not arm a deletion: otherwise --dry-run --prune followed by a
// real --prune would delete on what the user experienced as the first run.
func TestPruneDryRunWritesNoLedger(t *testing.T) {
	mem := NewMemFilesystem()
	p := &prunableStub{channel: "skills", observed: []Resource{{ID: "stray", Channel: "skills"}}}
	o := NewOrchestratorScoped(t.TempDir(), ScopeRepo, Env{FS: mem, DryRun: true}, []Provider{p})
	o.EnablePrune(fixedNow)

	if _, err := o.ApplyAllRendered(map[string][]Resource{}, emptyLock()); err != nil {
		t.Fatal(err)
	}

	if _, err := mem.ReadFile(mustOfferedRepo(t, o.root)); err == nil {
		t.Error("--dry-run wrote the offered ledger; a preview must not arm a deletion")
	}
}

// A corrupt ledger must re-offer rather than delete unannounced.
func TestPruneCorruptLedgerReOffers(t *testing.T) {
	o, _, mem := setupPrune(t, ScopeRepo, "skills", []Resource{{ID: "stray", Channel: "skills"}})
	if err := mem.WriteFile(mustOfferedRepo(t, o.root), []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}

	plans, err := o.PlanAllRendered(map[string][]Resource{})
	if err != nil {
		t.Fatal(err)
	}
	if n := deletesFor(plans["skills"], "stray"); n != 0 {
		t.Errorf("delete count = %d, want 0; a corrupt ledger must fail open", n)
	}
	if !o.OfferedLedgerCorrupt() {
		t.Error("OfferedLedgerCorrupt = false, want true so the caller can warn")
	}
}

// A provider that does not implement Pruner never has untracked resources
// removed, regardless of --prune.
func TestPruneSkipsNonPruner(t *testing.T) {
	mem := NewMemFilesystem()
	p := &stubProvider{channel: "hooks", observed: []Resource{{ID: "stray", Channel: "hooks"}}}
	o := NewOrchestratorScoped(t.TempDir(), ScopeRepo, Env{FS: mem}, []Provider{p})
	o.EnablePrune(fixedNow)

	plans, err := o.PlanAllRendered(map[string][]Resource{})
	if err != nil {
		t.Fatal(err)
	}
	if n := deletesFor(plans["hooks"], "stray"); n != 0 {
		t.Errorf("delete count = %d, want 0; a non-Pruner channel is never pruned", n)
	}
	if len(o.NewlyOffered()) != 0 {
		t.Error("a non-Pruner channel must not even be offered")
	}
}

// MCP is repo-scope only for prune: in user scope MCP.Observe reads
// $HOME/.mcp.json, which is not where Claude Code keeps user MCP servers.
func TestPruneSkipsMCPInUserScope(t *testing.T) {
	mem := NewMemFilesystem()
	p := &prunableStub{channel: "mcpServers", observed: []Resource{{ID: "stray", Channel: "mcpServers"}}}
	o := NewOrchestratorScoped(t.TempDir(), ScopeUser, Env{FS: mem, UserScope: true}, []Provider{p})
	o.EnablePrune(fixedNow)

	plans, err := o.PlanAllRendered(map[string][]Resource{})
	if err != nil {
		t.Fatal(err)
	}
	if n := deletesFor(plans["mcpServers"], "stray"); n != 0 {
		t.Errorf("delete count = %d, want 0; user-scope MCP prune would act on the wrong file", n)
	}
}

// The same channel still prunes in repo scope, so the guard above is scoped
// rather than a blanket disable.
func TestPruneAllowsMCPInRepoScope(t *testing.T) {
	o, _, mem := setupPrune(t, ScopeRepo, "mcpServers", []Resource{{ID: "stray", Channel: "mcpServers"}})
	seed := &OfferedLedger{Offered: map[string]OfferedEntry{
		"mcpServers:stray": {FirstOfferedAt: "2026-07-16T10:00:00Z"},
	}}
	if err := WriteOffered(mem, mustOfferedRepo(t, o.root), seed); err != nil {
		t.Fatal(err)
	}

	plans, err := o.PlanAllRendered(map[string][]Resource{})
	if err != nil {
		t.Fatal(err)
	}
	if n := deletesFor(plans["mcpServers"], "stray"); n != 1 {
		t.Errorf("delete count = %d, want 1", n)
	}
}

// Never delete what could not be backed up.
func TestPruneBackupFailureCancelsDelete(t *testing.T) {
	mem := NewMemFilesystem()
	p := &prunableStub{
		channel:   "skills",
		observed:  []Resource{{ID: "stray", Channel: "skills"}},
		backupErr: errors.New("disk full"),
	}
	o := NewOrchestratorScoped(t.TempDir(), ScopeRepo, Env{FS: mem}, []Provider{p})
	o.EnablePrune(fixedNow)
	seed := &OfferedLedger{Offered: map[string]OfferedEntry{
		"skills:stray": {FirstOfferedAt: "2026-07-16T10:00:00Z"},
	}}
	if err := WriteOffered(mem, mustOfferedRepo(t, o.root), seed); err != nil {
		t.Fatal(err)
	}

	results, err := o.ApplyAllRendered(map[string][]Resource{}, emptyLock())
	if err == nil {
		t.Error("err = nil, want a failure surfaced for the cancelled delete")
	}

	for _, plan := range p.applied {
		if deletesFor(plan, "stray") != 0 {
			t.Error("the delete was applied despite its backup failing")
		}
	}
	found := false
	for _, res := range results {
		for _, f := range res.Failed {
			if f.Change.ID == "stray" {
				found = true
			}
		}
	}
	if !found {
		t.Error("the cancelled delete was not reported as a failure")
	}
}

// After a real run, the offered ledger records what was reported so a later
// run can arm it.
func TestPruneWritesOfferedLedgerAfterApply(t *testing.T) {
	o, _, mem := setupPrune(t, ScopeRepo, "skills", []Resource{{ID: "stray", Channel: "skills"}})

	if _, err := o.ApplyAllRendered(map[string][]Resource{}, emptyLock()); err != nil {
		t.Fatal(err)
	}

	raw, err := mem.ReadFile(mustOfferedRepo(t, o.root))
	if err != nil {
		t.Fatalf("offered ledger not written: %v", err)
	}
	var l OfferedLedger
	if err := json.Unmarshal(raw, &l); err != nil {
		t.Fatal(err)
	}
	got, ok := l.Offered["skills:stray"]
	if !ok {
		t.Fatalf("skills:stray missing from ledger: %s", raw)
	}
	if got.FirstOfferedAt != fixedNow().UTC().Format(time.RFC3339) {
		t.Errorf("FirstOfferedAt = %q, want %q", got.FirstOfferedAt, fixedNow().UTC().Format(time.RFC3339))
	}
}

// A pruned resource is gone, so its ledger row must not linger and re-offer a
// resource that no longer exists.
func TestPruneDeletedEntryDropsFromLedger(t *testing.T) {
	o, _, mem := setupPrune(t, ScopeRepo, "skills", []Resource{{ID: "stray", Channel: "skills"}})
	seed := &OfferedLedger{Offered: map[string]OfferedEntry{
		"skills:stray": {FirstOfferedAt: "2026-07-16T10:00:00Z"},
	}}
	if err := WriteOffered(mem, mustOfferedRepo(t, o.root), seed); err != nil {
		t.Fatal(err)
	}

	if _, err := o.ApplyAllRendered(map[string][]Resource{}, emptyLock()); err != nil {
		t.Fatal(err)
	}

	raw, err := mem.ReadFile(mustOfferedRepo(t, o.root))
	if err != nil {
		t.Fatal(err)
	}
	var l OfferedLedger
	if err := json.Unmarshal(raw, &l); err != nil {
		t.Fatal(err)
	}
	if _, ok := l.Offered["skills:stray"]; ok {
		t.Errorf("deleted entry still in ledger: %s", raw)
	}
}

// Without --prune nothing changes: the historical contract.
func TestNoPruneLeavesUntrackedAlone(t *testing.T) {
	mem := NewMemFilesystem()
	p := &prunableStub{channel: "skills", observed: []Resource{{ID: "stray", Channel: "skills"}}}
	o := NewOrchestratorScoped(t.TempDir(), ScopeRepo, Env{FS: mem}, []Provider{p})
	// EnablePrune deliberately not called.

	plans, err := o.PlanAllRendered(map[string][]Resource{})
	if err != nil {
		t.Fatal(err)
	}
	if n := deletesFor(plans["skills"], "stray"); n != 0 {
		t.Errorf("delete count = %d, want 0 without --prune", n)
	}
	if _, err := mem.ReadFile(mustOfferedRepo(t, o.root)); err == nil {
		t.Error("a non-prune run wrote the offered ledger")
	}
}

// A provider whose Observe leaves Resource.Channel empty must still produce a
// usable ledger key. Otherwise the row would be ":<id>", never match on the
// next run, and the entry would be re-offered forever instead of arming.
func TestPruneStampsChannelOnOffer(t *testing.T) {
	mem := NewMemFilesystem()
	p := &prunableStub{channel: "skills", observed: []Resource{{ID: "stray"}}} // no Channel set
	o := NewOrchestratorScoped(t.TempDir(), ScopeRepo, Env{FS: mem}, []Provider{p})
	o.EnablePrune(fixedNow)

	if _, err := o.ApplyAllRendered(map[string][]Resource{}, emptyLock()); err != nil {
		t.Fatal(err)
	}

	raw, err := mem.ReadFile(mustOfferedRepo(t, o.root))
	if err != nil {
		t.Fatal(err)
	}
	var l OfferedLedger
	if err := json.Unmarshal(raw, &l); err != nil {
		t.Fatal(err)
	}
	if _, ok := l.Offered["skills:stray"]; !ok {
		t.Errorf("ledger = %s; want a skills:stray row keyed off the plan's channel", raw)
	}
}

// mustOfferedRepo resolves the repo-scope offered ledger path the same way the
// orchestrator does.
func mustOfferedRepo(t *testing.T, root string) string {
	t.Helper()
	p, err := OfferedPathRepo(root, "")
	if err != nil {
		t.Fatal(err)
	}
	return p
}
