package resolve

// Entry is one channel entry as it enters the merge. Value is an opaque
// payload (the caller stores the real config); Merge only arbitrates winners.
type Entry struct {
	Value       any
	Overridable bool
	Layer       string
}

// LayerEntries is one layer's entries, in descending authority order
// when passed to Merge (team first, personal last).
type LayerEntries struct {
	Layer   string
	Entries map[string]Entry
}

// Merge applies the Option-C precedence rule (spec §1): a higher-authority
// layer wins unless its entry is Overridable, in which case the next
// lower-authority layer's entry replaces it.
func Merge(layers []LayerEntries) (map[string]Entry, error) {
	out := map[string]Entry{}
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
