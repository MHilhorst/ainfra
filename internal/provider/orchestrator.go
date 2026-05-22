package provider

import (
	"fmt"
	"sort"

	"github.com/MHilhorst/ainfra/internal/lockfile"
)

// channelOrder is the order providers are observed and applied. Channels that
// other channels depend on (cliTools install binaries, backgroundServices are
// required by MCP servers) come first.
var channelOrder = []string{
	"cliTools", "backgroundServices", "mcpServers",
	"skills", "plugins", "rules", "tools", "hooks", "commands",
}

// Orchestrator loads locks, reads the applied ledger, and drives all registered
// providers through plan and apply in a deterministic order.
type Orchestrator struct {
	root      string
	env       Env
	providers map[string]Provider
}

// NewOrchestrator builds an Orchestrator keyed by each provider's Channel().
func NewOrchestrator(root string, env Env, ps []Provider) *Orchestrator {
	m := make(map[string]Provider, len(ps))
	for _, p := range ps {
		m[p.Channel()] = p
	}
	return &Orchestrator{root: root, env: env, providers: m}
}

// PlanAll reads the applied ledger, computes observed state via each provider's
// Observe, and returns the per-channel diff of desired vs observed vs prior.
func (o *Orchestrator) PlanAll(desired *lockfile.Lock) (map[string]ChannelPlan, error) {
	prior, err := ReadApplied(o.root)
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
		plan := DiffResources(p.Channel(), desiredByCh[p.Channel()], observed, priorForCh)
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

	return WriteApplied(o.root, desired)
}

// PlanAllRendered computes the per-channel diff using rendered resources (which
// carry Payload) as the desired state rather than bare lockfile entries. The
// observed state is read from the machine and the prior state from the applied
// ledger as in PlanAll.
func (o *Orchestrator) PlanAllRendered(rendered map[string][]Resource) (map[string]ChannelPlan, error) {
	prior, err := ReadApplied(o.root)
	if err != nil {
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
		plan := DiffResources(p.Channel(), desiredForCh, observed, priorForCh)
		result[ch] = plan
	}
	return result, nil
}

// ApplyAllRendered applies rendered resources (which carry Payload) and on
// success writes the applied ledger from desired (the lockfile that produced
// the rendered resources). This is the correct path for apply: the lockfile
// supplies content hashes for drift detection while the rendered resources
// supply Payload for file writes. When env.DryRun is set, providers still run
// but the applied ledger is not written.
func (o *Orchestrator) ApplyAllRendered(rendered map[string][]Resource, desired *lockfile.Lock) error {
	plans, err := o.PlanAllRendered(rendered)
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

	// A dry run exercises every provider's Apply (each no-ops its own writes)
	// but must not record a ledger — the machine was not reconciled.
	if o.env.DryRun {
		return nil
	}
	return WriteApplied(o.root, desired)
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
