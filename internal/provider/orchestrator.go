package provider

import (
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
