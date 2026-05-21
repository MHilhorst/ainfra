package resolve

// MergeEntry is one channel entry as it enters the merge. Value is an opaque
// payload (the caller stores the real config); Merge only arbitrates winners.
type MergeEntry struct {
	Value       any
	Overridable bool
	Layer       string
}

// LayerEntries is one config layer's channel entries, keyed by entry id.
type LayerEntries struct {
	Layer   string
	Entries map[string]MergeEntry
}

// Merge applies the Option-C precedence rule (spec §1): a higher-authority
// layer wins unless its entry is Overridable, in which case the next
// lower-authority layer's entry replaces it. The layers slice must be in
// descending authority order — team first, personal last.
// RunLock does not yet call Merge — it is the precedence primitive the follow-up plan's apply/check commands build on.
func Merge(layers []LayerEntries) (map[string]MergeEntry, error) {
	out := map[string]MergeEntry{}
	for _, layer := range layers {
		for id, e := range layer.Entries {
			e.Layer = layer.Layer
			cur, exists := out[id]
			if !exists {
				out[id] = e
				continue
			}
			if cur.Overridable {
				out[id] = e // sanctioned override by lower layer
			}
			// else: higher-authority non-overridable entry stands.
		}
	}
	return out, nil
}
