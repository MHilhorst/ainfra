package provider

import "sort"

// DiffResources computes the channel-agnostic three-way diff: desired (from the
// lockfile), observed (from the machine), prior (from the applied-state ledger).
// A resource in prior but no longer desired is a Delete; a desired resource
// missing from or differing on the machine is a Create or Update; a resource
// the tool never recorded as its own (in neither prior nor desired) is left
// untouched. Changes are returned sorted by ID for deterministic plan output.
func DiffResources(channel string, desired, observed, prior []Resource) ChannelPlan {
	byID := func(rs []Resource) map[string]Resource {
		m := map[string]Resource{}
		for _, r := range rs {
			m[r.ID] = r
		}
		return m
	}
	d, o, pr := byID(desired), byID(observed), byID(prior)

	plan := ChannelPlan{Channel: channel}
	for id, prior := range pr {
		if _, stillWanted := d[id]; !stillWanted {
			plan.Changes = append(plan.Changes, Change{
				Kind:     ChangeDelete,
				ID:       id,
				Detail:   "no longer in ainfra.yaml — will be removed",
				Resource: prior,
			})
		}
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
		case got.ContentHash != want.ContentHash:
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
	sort.Slice(plan.Changes, func(i, j int) bool { return plan.Changes[i].ID < plan.Changes[j].ID })
	return plan
}
