package provider

import "sort"

// DiffOpts tunes the diff. The zero value is the historical contract: a
// resource ainfra never recorded as its own is left untouched.
type DiffOpts struct {
	// Prune treats an observed resource that is in neither desired nor prior
	// as an implicit tombstone: an untracked resource becomes a delete.
	//
	// Off by default, and the caller must guard the deletes it produces.
	// Untracked does not mean unwanted — it usually means the user never got
	// around to declaring the thing — so removing on sight destroys real work.
	// The orchestrator gates these deletes behind the offered ledger, which
	// requires an entry to have been reported on an earlier run.
	Prune bool
}

// DiffResources computes the channel-agnostic three-way diff: desired (from the
// lockfile), observed (from the machine), prior (from the applied-state ledger).
// A resource in prior but no longer desired is a Delete; a desired resource
// missing from or differing on the machine is a Create or Update; a resource
// the tool never recorded as its own (in neither prior nor desired) is left
// untouched. Changes are returned sorted by ID for deterministic plan output.
//
// A desired resource flagged Tombstone (enabled: false in the manifest) is an
// explicit "ensure absent" instruction: it is removed whenever present on the
// machine, even if ainfra never installed it (observed but not prior). It is
// never created or updated. This is what makes "absent from the manifest" (leave
// alone) differ from "explicitly retired" (remove everywhere).
//
// opts.Prune adds a third policy on that same axis: "absent from the manifest
// and pruning is on" is removed too, flagged Change.Prune. Those deletes are
// candidates, not decisions: the caller must guard them (see DiffOpts.Prune).
func DiffResources(channel string, desired, observed, prior []Resource, opts DiffOpts) ChannelPlan {
	byID := func(rs []Resource) map[string]Resource {
		m := map[string]Resource{}
		for _, r := range rs {
			m[r.ID] = r
		}
		return m
	}
	o, pr := byID(observed), byID(prior)

	// Split desired into live resources and tombstones; the two are diffed
	// against the machine by different rules.
	d := map[string]Resource{}
	tombstones := map[string]Resource{}
	for _, r := range desired {
		if r.Tombstone {
			tombstones[r.ID] = r
		} else {
			d[r.ID] = r
		}
	}

	plan := ChannelPlan{Channel: channel}

	// Tombstones: delete whatever is on the machine, regardless of prior.
	for id := range tombstones {
		if got, onMachine := o[id]; onMachine {
			plan.Changes = append(plan.Changes, Change{
				Kind:     ChangeDelete,
				ID:       id,
				Detail:   "disabled in ainfra.yaml — will be removed",
				Resource: got,
			})
		}
	}

	for id, prior := range pr {
		if _, stillWanted := d[id]; stillWanted {
			continue
		}
		if _, tombstoned := tombstones[id]; tombstoned {
			continue // already handled above
		}
		plan.Changes = append(plan.Changes, Change{
			Kind:     ChangeDelete,
			ID:       id,
			Detail:   "no longer in ainfra.yaml — will be removed",
			Resource: prior,
		})
	}
	for id, want := range d {
		got, onMachine := o[id]
		switch {
		case !onMachine:
			plan.Changes = append(plan.Changes, Change{
				Kind:     ChangeCreate,
				ID:       id,
				Detail:   "new — will be installed",
				Resource: want,
			})
		case got.ContentHash != want.ContentHash && want.AlwaysRefresh:
			// Unpinned: the hashes are meant to differ (see
			// Resource.AlwaysRefresh), so this is the steady state rather
			// than drift, and reporting it as "out of sync" would describe a
			// divergence no run can ever resolve.
			plan.Changes = append(plan.Changes, Change{
				Kind:     ChangeRefresh,
				ID:       id,
				Detail:   "unpinned — will check for updates",
				Resource: want,
			})
		case got.ContentHash != want.ContentHash:
			// A differing resource that ainfra never recorded is not its own
			// drift: the file belongs to the user, and overwriting it without
			// saying so destroys their only copy. Flag it so the orchestrator
			// backs it up, and name it in the plan — "out of sync" reads as
			// routine correction and is exactly what hides this.
			if _, known := pr[id]; !known {
				plan.Changes = append(plan.Changes, Change{
					Kind:     ChangeUpdate,
					ID:       id,
					Detail:   "not installed by ainfra — will be overwritten (a copy is kept)",
					Resource: want,
					Adopts:   true,
				})
				continue
			}
			plan.Changes = append(plan.Changes, Change{
				Kind:     ChangeUpdate,
				ID:       id,
				Detail:   "out of sync — will be updated",
				Resource: want,
			})
		default:
			plan.Changes = append(plan.Changes, Change{
				Kind:     ChangeNoop,
				ID:       id,
				Detail:   "up to date",
				Resource: want,
			})
		}
	}
	// Prune: an observed resource in neither desired nor prior is untracked.
	// Treat it as an implicit tombstone so `install --prune` can remove config
	// no manifest declares.
	if opts.Prune {
		for id, got := range o {
			if _, stillWanted := d[id]; stillWanted {
				continue
			}
			if _, known := pr[id]; known {
				continue // already a Delete from the prior loop above
			}
			if _, tombstoned := tombstones[id]; tombstoned {
				continue // already handled above
			}
			plan.Changes = append(plan.Changes, Change{
				Kind:     ChangeDelete,
				ID:       id,
				Detail:   "not declared in ainfra — will be removed",
				Resource: got,
				Prune:    true,
			})
		}
	}

	sort.Slice(plan.Changes, func(i, j int) bool { return plan.Changes[i].ID < plan.Changes[j].ID })
	return plan
}
