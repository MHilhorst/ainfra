package resolve

import "sort"

// PortRequest names one allocated-port resolved field that needs a value.
type PortRequest struct {
	Instance string
	Field    string
}

// AllocatePorts assigns a distinct local port to every request. A request
// already present in prior (the lockfile's recorded allocations) keeps its
// recorded port — making ports sticky across runs. Fresh requests take the
// lowest free port at or above base. No human ever types a port (spec §4.3).
func AllocatePorts(reqs []PortRequest, prior map[string]map[string]int, base int) (map[string]map[string]int, error) {
	out := map[string]map[string]int{}
	used := map[int]bool{}
	for _, fields := range prior {
		for _, p := range fields {
			used[p] = true
		}
	}
	sorted := append([]PortRequest(nil), reqs...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Instance != sorted[j].Instance {
			return sorted[i].Instance < sorted[j].Instance
		}
		return sorted[i].Field < sorted[j].Field
	})
	for _, r := range sorted {
		if out[r.Instance] == nil {
			out[r.Instance] = map[string]int{}
		}
		if p, ok := prior[r.Instance][r.Field]; ok {
			out[r.Instance][r.Field] = p
			continue
		}
		p := base
		for used[p] {
			p++
		}
		used[p] = true
		out[r.Instance][r.Field] = p
	}
	return out, nil
}
