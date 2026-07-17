package provider

import (
	"fmt"
	"path/filepath"
	"sort"
	"time"

	"github.com/MHilhorst/ainfra/internal/lockfile"
)

// channelOrder is the order providers are observed and applied. Channels that
// other channels depend on (cliTools install binaries, backgroundServices are
// required by MCP servers) come first.
var channelOrder = []string{
	"cliTools", "backgroundServices", "mcpServers",
	"skills", "marketplaces", "plugins", "rules", "tools", "hooks", "commands",
}

// Scope distinguishes a repo-scope orchestrator (writes to repo paths and
// the repo applied ledger) from a user-scope one (writes to ~/.claude/ and
// the user-global applied ledger). Each scope uses the same providers and
// the same diff/apply flow — only the read/write paths differ.
type Scope int

const (
	// ScopeRepo is the historical orchestrator behavior: write to env.Root/.claude/
	// and persist the applied ledger to env.Root/.ainfra/applied*.lock.
	ScopeRepo Scope = iota
	// ScopeUser writes to $HOME/.claude/ and persists the applied ledger to
	// $XDG_CONFIG_HOME/ainfra/applied*.lock. Personal-layer entries from the
	// global ainfra config land here.
	ScopeUser
)

// Orchestrator loads locks, reads the applied ledger, and drives all registered
// providers through plan and apply in a deterministic order.
type Orchestrator struct {
	root      string
	scope     Scope
	env       Env
	providers map[string]Provider

	// prune and its companions implement `install --prune`. See EnablePrune.
	prune        bool
	now          func() time.Time
	offered      *OfferedLedger
	offeredBad   bool
	newlyOffered []Change
	backupDir    string
}

// EnablePrune turns on --prune for this orchestrator: untracked resources
// become candidate deletes, guarded by the offered ledger so nothing is
// removed the first time the user is shown it.
//
// now is injected so tests can pin the backup directory name and the offer
// timestamps.
func (o *Orchestrator) EnablePrune(now func() time.Time) {
	o.prune = true
	o.now = now
}

// NewlyOffered returns the untracked resources the last plan reported to the
// user for the first time. They are deliberately absent from the plan: prune
// never deletes on first sight, so the caller should print these as "not
// declared, nothing removed yet".
func (o *Orchestrator) NewlyOffered() []Change { return o.newlyOffered }

// OfferedLedgerCorrupt reports whether the offered ledger could not be parsed
// and was treated as empty, so the caller can warn. Everything was re-offered
// and nothing was deleted.
func (o *Orchestrator) OfferedLedgerCorrupt() bool { return o.offeredBad }

// RecordOffered persists the offers from the last plan without applying
// anything.
//
// A prune run whose plan is otherwise empty still has work to do: the entries
// it just reported must be recorded, or the next run would offer them again
// instead of arming them, and prune could never remove anything. ApplyAllRendered
// records offers itself; this is for the caller's early-return path where there
// are no changes to apply.
func (o *Orchestrator) RecordOffered() error {
	if !o.prune || o.env.DryRun {
		return nil
	}
	return o.writeOffered(nil)
}

func (o *Orchestrator) clock() time.Time {
	if o.now != nil {
		return o.now()
	}
	return time.Now()
}

// offeredPath resolves the offered ledger for this orchestrator's scope.
func (o *Orchestrator) offeredPath() (string, error) {
	if o.scope == ScopeUser {
		return OfferedPathUser()
	}
	return OfferedPath(o.root), nil
}

// pruneableInScope reports whether p's untracked resources may be removed in
// this orchestrator's scope.
func (o *Orchestrator) pruneableInScope(p Provider) bool {
	if _, ok := p.(Pruner); !ok {
		return false
	}
	// MCP in user scope would read $HOME/.mcp.json, which is not where Claude
	// Code keeps user-level MCP servers (~/.claude.json, a different file and
	// format ainfra does not read yet). Pruning there would act on the wrong
	// file, so the channel is repo-scope-only for prune.
	if o.scope == ScopeUser && p.Channel() == "mcpServers" {
		return false
	}
	return true
}

// loadOffered reads the offered ledger once per orchestrator.
func (o *Orchestrator) loadOffered() error {
	if !o.prune || o.offered != nil {
		return nil
	}
	path, err := o.offeredPath()
	if err != nil {
		return err
	}
	l, corrupt, err := ReadOffered(o.env.FS, path)
	if err != nil {
		return err
	}
	o.offered, o.offeredBad = l, corrupt
	return nil
}

// guardPrune drops prune deletes the user has not been shown before, recording
// them in newlyOffered instead.
//
// This is what makes --prune safe. Untracked usually means "never got around
// to declaring it", not "unwanted", so an entry must appear in one run's report
// before a later run may delete it. The guard lives here rather than in the
// caller because ApplyAllRendered re-plans internally: a caller-side guard
// would be bypassed on apply and the unoffered deletes would execute.
func (o *Orchestrator) guardPrune(plan ChannelPlan) ChannelPlan {
	out := ChannelPlan{Channel: plan.Channel}
	for _, c := range plan.Changes {
		if !c.Prune {
			out.Changes = append(out.Changes, c)
			continue
		}
		if _, seen := o.offered.Offered[OfferedKey(plan.Channel, c.ID)]; seen {
			out.Changes = append(out.Changes, c) // armed: reported on an earlier run
			continue
		}
		o.newlyOffered = append(o.newlyOffered, c)
	}
	return out
}

// diffOptsFor returns the diff options for one provider in this scope.
func (o *Orchestrator) diffOptsFor(p Provider) DiffOpts {
	if o.prune && o.pruneableInScope(p) {
		return DiffOpts{Prune: true}
	}
	return DiffOpts{}
}

// backupPrunes copies each prune delete's on-disk state into the run's backup
// directory, dropping any change whose backup failed. Never delete what could
// not be backed up: the resource may be the user's only copy.
func (o *Orchestrator) backupPrunes(p Provider, plan ChannelPlan) (ChannelPlan, []ChangeFailure) {
	pr, ok := p.(Pruner)
	if !ok {
		return plan, nil
	}
	out := ChannelPlan{Channel: plan.Channel}
	var failed []ChangeFailure
	for _, c := range plan.Changes {
		if !c.Prune {
			out.Changes = append(out.Changes, c)
			continue
		}
		if err := pr.Backup(o.env, c.Resource, o.runBackupDir()); err != nil {
			failed = append(failed, ChangeFailure{
				Change: c,
				Err:    fmt.Errorf("backup failed, not deleting: %w", err),
			})
			continue
		}
		out.Changes = append(out.Changes, c)
	}
	return out, failed
}

// runBackupDir is this run's backup directory, computed once so every channel
// shares one timestamped tree. .ainfra/ is git-ignored, so backups never land
// in git.
func (o *Orchestrator) runBackupDir() string {
	if o.backupDir == "" {
		o.backupDir = filepath.Join(o.root, ".ainfra", "pruned-"+o.clock().UTC().Format("20060102T150405Z"))
	}
	return o.backupDir
}

// writeOffered persists the offered ledger after an apply. A row exists only
// for a resource that is still untracked and still on disk: entries the user
// declared drop out because they are no longer untracked, and entries this run
// deleted drop out because they are gone.
func (o *Orchestrator) writeOffered(results []ApplyResult) error {
	deleted := map[string]bool{}
	for _, res := range results {
		for _, c := range res.Applied {
			if c.Prune && c.Kind == ChangeDelete {
				deleted[OfferedKey(res.Channel, c.ID)] = true
			}
		}
	}

	next := &OfferedLedger{Version: offeredLedgerVersion, Offered: map[string]OfferedEntry{}}
	stamp := o.clock().UTC().Format(time.RFC3339)

	keep := func(key string) {
		if deleted[key] {
			return
		}
		if prev, ok := o.offered.Offered[key]; ok {
			next.Offered[key] = prev
			return
		}
		next.Offered[key] = OfferedEntry{FirstOfferedAt: stamp}
	}

	// Reported this run for the first time.
	for _, c := range o.newlyOffered {
		keep(OfferedKey(c.Resource.Channel, c.ID))
	}
	// Armed but not removed (e.g. its backup failed): keep the row so the next
	// run can retry rather than re-offering an entry the user already saw.
	for _, res := range results {
		for _, f := range res.Failed {
			if f.Change.Prune {
				keep(OfferedKey(res.Channel, f.Change.ID))
			}
		}
		for _, s := range res.Skipped {
			if s.Change.Prune {
				keep(OfferedKey(res.Channel, s.Change.ID))
			}
		}
	}

	path, err := o.offeredPath()
	if err != nil {
		return err
	}
	return WriteOffered(o.env.FS, path, next)
}

// NewOrchestrator builds a repo-scope Orchestrator keyed by each provider's
// Channel(). This is the historical constructor; use NewOrchestratorScoped
// to build a user-scope variant.
func NewOrchestrator(root string, env Env, ps []Provider) *Orchestrator {
	return NewOrchestratorScoped(root, ScopeRepo, env, ps)
}

// NewOrchestratorScoped builds an Orchestrator at the given scope. ScopeUser
// reads/writes the user-global applied ledger; ScopeRepo behaves identically
// to the historical NewOrchestrator.
func NewOrchestratorScoped(root string, scope Scope, env Env, ps []Provider) *Orchestrator {
	m := make(map[string]Provider, len(ps))
	for _, p := range ps {
		m[p.Channel()] = p
	}
	return &Orchestrator{root: root, scope: scope, env: env, providers: m}
}

// readApplied dispatches the ledger read to the right scope.
func (o *Orchestrator) readApplied() (*lockfile.Lock, error) {
	if o.scope == ScopeUser {
		return ReadAppliedUserForAgent(o.env.Agent)
	}
	return ReadAppliedForAgent(o.root, o.env.Agent)
}

// writeApplied dispatches the ledger write to the right scope.
func (o *Orchestrator) writeApplied(l *lockfile.Lock) error {
	if o.scope == ScopeUser {
		return WriteAppliedUserForAgent(o.env.Agent, l)
	}
	return WriteAppliedForAgent(o.root, o.env.Agent, l)
}

// PlanAll reads the applied ledger, computes observed state via each provider's
// Observe, and returns the per-channel diff of desired vs observed vs prior.
func (o *Orchestrator) PlanAll(desired *lockfile.Lock) (map[string]ChannelPlan, error) {
	prior, err := o.readApplied()
	if err != nil {
		return nil, err
	}

	desiredByCh := ResourcesByChannel(desired)
	priorByCh := ResourcesByChannel(prior)

	result := make(map[string]ChannelPlan, len(o.providers))
	for _, ch := range o.sortedChannels() {
		p := o.providers[ch]
		observed, err := p.Observe(o.env)
		if err != nil {
			return nil, err
		}
		priorForCh := priorByCh[p.Channel()]
		priorByID := make(map[string]Resource, len(priorForCh))
		for _, r := range priorForCh {
			priorByID[r.ID] = r
		}
		for i, obs := range observed {
			if obs.ContentHash == "" {
				if pr, ok := priorByID[obs.ID]; ok {
					observed[i].ContentHash = pr.ContentHash
				}
			}
		}
		plan := DiffResources(p.Channel(), desiredByCh[p.Channel()], observed, priorForCh, DiffOpts{})
		result[ch] = plan
	}
	return result, nil
}

// ApplyAll calls PlanAll, then applies each provider whose plan is non-empty in
// sorted channel order. On the first error it returns immediately without
// writing the ledger (partial apply leaves the ledger at the last consistent
// state). On full success it writes the applied ledger.
func (o *Orchestrator) ApplyAll(desired *lockfile.Lock) error {
	plans, err := o.PlanAll(desired)
	if err != nil {
		return err
	}

	for _, ch := range o.sortedChannels() {
		plan := plans[ch]
		if plan.Empty() {
			continue
		}
		p := o.providers[ch]
		if _, err := p.Apply(o.env, plan); err != nil {
			return err
		}
	}

	return o.writeApplied(desired)
}

// PlanAllRendered computes the per-channel diff using rendered resources (which
// carry Payload) as the desired state rather than bare lockfile entries. The
// observed state is read from the machine and the prior state from the applied
// ledger as in PlanAll.
func (o *Orchestrator) PlanAllRendered(rendered map[string][]Resource) (map[string]ChannelPlan, error) {
	prior, err := o.readApplied()
	if err != nil {
		return nil, err
	}
	o.newlyOffered = nil
	if err := o.loadOffered(); err != nil {
		return nil, err
	}

	priorByCh := ResourcesByChannel(prior)

	result := make(map[string]ChannelPlan, len(o.providers))
	for _, ch := range o.sortedChannels() {
		p := o.providers[ch]
		observed, err := p.Observe(o.env)
		if err != nil {
			return nil, err
		}
		priorForCh := priorByCh[p.Channel()]
		priorByID := make(map[string]Resource, len(priorForCh))
		for _, r := range priorForCh {
			priorByID[r.ID] = r
		}
		for i, obs := range observed {
			if obs.ContentHash == "" {
				if pr, ok := priorByID[obs.ID]; ok {
					observed[i].ContentHash = pr.ContentHash
				}
			}
		}
		desiredForCh := rendered[p.Channel()]
		opts := o.diffOptsFor(p)
		plan := DiffResources(p.Channel(), desiredForCh, observed, priorForCh, opts)
		if opts.Prune {
			plan = o.guardPrune(plan)
		}
		result[ch] = plan
	}
	return result, nil
}

// ApplyAllRendered applies rendered resources (which carry Payload) and writes
// the applied ledger. Unlike a fail-fast apply, it does not stop on the first
// error: it applies every channel, skips resources whose requires: dependency
// failed earlier in the run, and writes a partial ledger (succeeded resources
// take their desired entry; failed or skipped ones fall back to prior). It
// returns the per-channel results and, if anything failed, an *ApplyError.
// When env.DryRun is set, providers still run but the ledger is not written.
func (o *Orchestrator) ApplyAllRendered(rendered map[string][]Resource, desired *lockfile.Lock) ([]ApplyResult, error) {
	plans, err := o.PlanAllRendered(rendered)
	if err != nil {
		return nil, err
	}
	prior, err := o.readApplied()
	if err != nil {
		return nil, err
	}

	failedRefs := map[string]bool{} // node refs that failed or were skipped
	var results []ApplyResult
	var errs []error

	for _, ch := range o.sortedChannels() {
		plan := plans[ch]
		if plan.Empty() {
			continue
		}
		p := o.providers[ch]

		runnable, skipped := splitBlocked(plan, failedRefs)

		// Back up prune deletes before they are applied. A resource that could
		// not be backed up is dropped from the plan: never delete what has no
		// copy.
		var backupFailed []ChangeFailure
		if o.prune && !o.env.DryRun {
			runnable, backupFailed = o.backupPrunes(p, runnable)
		}

		res := ApplyResult{Channel: ch}
		r, applyErr := p.Apply(o.env, runnable)
		if applyErr != nil {
			// A catastrophic channel error fails every runnable change in it.
			for _, c := range runnable.Changes {
				if c.Kind != ChangeNoop {
					res.Failed = append(res.Failed, ChangeFailure{Change: c, Err: applyErr})
				}
			}
		} else {
			res = r
			res.Channel = ch
		}
		res.Skipped = append(res.Skipped, skipped...)
		res.Failed = append(res.Failed, backupFailed...)

		for _, f := range res.Failed {
			failedRefs[nodeRef(ch, f.Change.ID)] = true
			errs = append(errs, fmt.Errorf("%s %s: %w", ch, f.Change.ID, f.Err))
		}
		for _, s := range res.Skipped {
			failedRefs[nodeRef(ch, s.Change.ID)] = true
		}
		results = append(results, res)
	}

	ledger := buildLedger(prior, restrictToRendered(desired, rendered), results)
	if !o.env.DryRun {
		if werr := o.writeApplied(ledger); werr != nil {
			errs = append(errs, fmt.Errorf("writing applied ledger: %w", werr))
		}
	}
	// The offered ledger is never written on a dry run: a preview that armed a
	// deletion would make the next real run delete on what the user
	// experienced as the first run.
	if o.prune && !o.env.DryRun {
		if werr := o.writeOffered(results); werr != nil {
			errs = append(errs, fmt.Errorf("writing offered ledger: %w", werr))
		}
	}

	if len(errs) > 0 {
		return results, &ApplyError{Errs: errs}
	}
	return results, nil
}

// nodeRef returns the dependency-graph node ref for a resource — the same
// "<prefix>:<id>" scheme the resolve pipeline uses (e.g. "cli:ssh", "svc:db").
func nodeRef(channel, id string) string {
	if p, ok := channelPrefix[channel]; ok {
		return p + ":" + id
	}
	return channel + ":" + id
}

// splitBlocked partitions plan into the changes that may run and the changes
// blocked because a resource they require is in failedRefs. A blocked non-noop
// change becomes a ChangeSkip; noop changes always stay runnable.
func splitBlocked(plan ChannelPlan, failedRefs map[string]bool) (runnable ChannelPlan, skipped []ChangeSkip) {
	runnable = ChannelPlan{Channel: plan.Channel}
	for _, c := range plan.Changes {
		blockedBy := ""
		if c.Kind != ChangeNoop {
			for _, ref := range c.Resource.Requires {
				if failedRefs[ref] {
					blockedBy = ref
					break
				}
			}
		}
		if blockedBy != "" {
			skipped = append(skipped, ChangeSkip{
				Change: c,
				Reason: fmt.Sprintf("requires %q, which failed earlier in this apply", blockedBy),
			})
			continue
		}
		runnable.Changes = append(runnable.Changes, c)
	}
	return runnable, skipped
}

// buildLedger constructs the applied-state ledger after a (possibly partial)
// apply. A resource that failed or was skipped falls back to its prior entry
// (or is dropped if it had none); every other resource takes its desired entry.
// With no failures the result equals desired — today's behaviour.
// restrictToRendered narrows desired to the resources actually rendered for the
// target agent, one channel at a time.
//
// The lock is agent-agnostic: it carries every resource in the manifest,
// including ones gated to another agent via `agents:`. The rendered set holds
// only what the target agent owns, and PlanAllRendered already treats it as the
// desired state. The applied ledger is per-agent too (see appliedPathForAgent),
// so it must be built from the same view the plan used. Passing the unfiltered
// lock instead records another agent's resources in this agent's ledger, and
// the next run reads them back as prior-without-desired and plans a delete
// against a file this agent never wrote.
func restrictToRendered(desired *lockfile.Lock, rendered map[string][]Resource) *lockfile.Lock {
	d := desired.Entries
	return &lockfile.Lock{
		Version:      desired.Version,
		GeneratedAt:  desired.GeneratedAt,
		ManifestHash: desired.ManifestHash,
		Entries: lockfile.Entries{
			MCPServers:         ledgerEntries(d.MCPServers, rendered["mcpServers"]),
			BackgroundServices: ledgerEntries(d.BackgroundServices, rendered["backgroundServices"]),
			Hooks:              ledgerEntries(d.Hooks, rendered["hooks"]),
			Commands:           ledgerEntries(d.Commands, rendered["commands"]),
			CLITools:           ledgerEntries(d.CLITools, rendered["cliTools"]),
			Skills:             ledgerEntries(d.Skills, rendered["skills"]),
			Marketplaces:       ledgerEntries(d.Marketplaces, rendered["marketplaces"]),
			Plugins:            ledgerEntries(d.Plugins, rendered["plugins"]),
			Rules:              ledgerEntries(d.Rules, rendered["rules"]),
			Tools:              ledgerEntries(d.Tools, rendered["tools"]),
		},
	}
}

// ledgerEntries builds one channel's applied-ledger entries from the resources
// actually rendered for the target agent. The rendered resource carries the
// authoritative ContentHash — the exact value the next run's diff recomputes —
// so it overrides the lockfile entry's hash, which may be derived by a different
// formula (background services fold in the script-generator version; see
// resolve.serviceContentHash). Recording the lockfile hash instead makes every
// install re-detect drift and never converge.
//
// A rendered resource with no lockfile entry (e.g. a template-derived lifecycle
// hook synthesized only at render time) is still recorded, synthesized from the
// resource, so the next run sees it as up to date rather than new. Tombstones
// are "ensure absent" instructions and are never recorded as applied state. A
// channel with neither lock entries nor rendered resources stays nil so an
// untouched channel is not rewritten as an empty one.
func ledgerEntries(lockCh map[string]lockfile.Entry, rendered []Resource) map[string]lockfile.Entry {
	if lockCh == nil && len(rendered) == 0 {
		return nil
	}
	out := make(map[string]lockfile.Entry, len(rendered))
	for _, r := range rendered {
		if r.Tombstone {
			continue
		}
		e, ok := lockCh[r.ID]
		if !ok {
			e = lockfile.Entry{Layer: r.Layer, Requires: r.Requires}
		}
		e.ContentHash = r.ContentHash
		out[r.ID] = e
	}
	return out
}

func buildLedger(prior, desired *lockfile.Lock, results []ApplyResult) *lockfile.Lock {
	bad := map[string]bool{} // key: "<channel>/<id>"
	for _, r := range results {
		for _, f := range r.Failed {
			bad[r.Channel+"/"+f.Change.ID] = true
		}
		for _, s := range r.Skipped {
			bad[r.Channel+"/"+s.Change.ID] = true
		}
	}
	d, p := desired.Entries, prior.Entries
	return &lockfile.Lock{
		Version:      desired.Version,
		GeneratedAt:  desired.GeneratedAt,
		ManifestHash: desired.ManifestHash,
		Entries: lockfile.Entries{
			MCPServers:         mergeLedgerChannel("mcpServers", d.MCPServers, p.MCPServers, bad),
			BackgroundServices: mergeLedgerChannel("backgroundServices", d.BackgroundServices, p.BackgroundServices, bad),
			Hooks:              mergeLedgerChannel("hooks", d.Hooks, p.Hooks, bad),
			Commands:           mergeLedgerChannel("commands", d.Commands, p.Commands, bad),
			CLITools:           mergeLedgerChannel("cliTools", d.CLITools, p.CLITools, bad),
			Skills:             mergeLedgerChannel("skills", d.Skills, p.Skills, bad),
			Marketplaces:       mergeLedgerChannel("marketplaces", d.Marketplaces, p.Marketplaces, bad),
			Plugins:            mergeLedgerChannel("plugins", d.Plugins, p.Plugins, bad),
			Rules:              mergeLedgerChannel("rules", d.Rules, p.Rules, bad),
			Tools:              mergeLedgerChannel("tools", d.Tools, p.Tools, bad),
		},
	}
}

// mergeLedgerChannel merges one channel's desired and prior entry maps: a
// "<channel>/<id>" present in bad takes the prior entry (or is dropped if prior
// has none); otherwise the desired entry. Prior-only ids (e.g. a failed delete)
// are re-added when bad.
func mergeLedgerChannel(channel string, desiredCh, priorCh map[string]lockfile.Entry, bad map[string]bool) map[string]lockfile.Entry {
	out := make(map[string]lockfile.Entry, len(desiredCh))
	for id, e := range desiredCh {
		if bad[channel+"/"+id] {
			if pe, ok := priorCh[id]; ok {
				out[id] = pe
			}
			continue
		}
		out[id] = e
	}
	for id, pe := range priorCh {
		if _, inDesired := desiredCh[id]; inDesired {
			continue
		}
		if bad[channel+"/"+id] {
			out[id] = pe
		}
	}
	return out
}

// sortedChannels returns registered channel names in dependency-aware order.
// Channels listed in channelOrder come first (in that order); any remaining
// registered channels are appended alphabetically.
func (o *Orchestrator) sortedChannels() []string {
	seen := make(map[string]bool, len(o.providers))
	result := make([]string, 0, len(o.providers))

	for _, ch := range channelOrder {
		if _, ok := o.providers[ch]; ok {
			result = append(result, ch)
			seen[ch] = true
		}
	}

	remaining := make([]string, 0)
	for ch := range o.providers {
		if !seen[ch] {
			remaining = append(remaining, ch)
		}
	}
	sort.Strings(remaining)
	result = append(result, remaining...)
	return result
}
