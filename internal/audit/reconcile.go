package audit

import (
	"fmt"
	"sort"

	"github.com/MHilhorst/ainfra/internal/lockfile"
	"github.com/MHilhorst/ainfra/internal/manifest"
)

// adoptableChannels are the channels `ainfra adopt` can emit into a
// manifest today. Unmanaged rows in other channels are visible but not
// counted toward the adoption footer — the suggested next command must
// not lie about what adopt will pick up.
var adoptableChannels = map[string]bool{
	"mcpServers": true,
	"hooks":      true,
	"commands":   true,
	"rules":      true,
}

// Reconcile tags each scanned row with management Status and Source by
// cross-referencing it against the manifest layers and the lockfile.
//
// Layer mapping rule (the brainstorm's central reconciliation):
//
//   - Filesystem LayerGlobal (~/.claude/) maps to manifest LayerPersonal
//     entries — the user's cross-repo personal manifest is what lands
//     under ~/.claude/. Entries from a published team manifest reach
//     LayerGlobal via being extended into the personal layer.
//
//   - Filesystem LayerProject (.claude/) maps to manifest LayerRepo and
//     LayerTeam entries — both live in the current repo's manifest stack.
//
// Reconcile tolerates nil inputs (no manifest, no lockfile) — see R2.
func Reconcile(rows []Row, layers map[manifest.Layer]*manifest.Manifest, committed, personal *lockfile.Lock) []Row {
	merged := mergeLocks(committed, personal)

	for i := range rows {
		r := &rows[i]
		lockEntry, ok := lookupLockEntry(merged, r.Channel, r.ID)
		if !ok {
			// Not in the lockfile → unmanaged. Keep existing Unmanaged tag.
			continue
		}
		r.Status.Unmanaged = false
		r.Status.Managed = true
		r.Version = lockEntry.Version
		r.Source = sourceForEntry(layers, manifest.Layer(lockEntry.Layer), r.Channel, r.ID)
	}

	// Cross-layer shadowing within the displayed filesystem layers.
	// Re-uses the precedence team < repo < personal that cmd_list.go
	// already encodes: an entry declared at multiple manifest layers
	// has the lower-priority occurrence rendered as shadowed.
	annotateShadowed(rows, layers)
	return rows
}

// BuildFooter walks rows and computes the audit-level summary.
func BuildFooter(rows []Row) FooterSummary {
	var s FooterSummary
	var managedCount int
	for _, r := range rows {
		switch {
		case r.Status.Unmanaged && adoptableChannels[r.Channel]:
			s.Adoptable++
		case r.Status.Managed:
			managedCount++
		}
		if r.Status.Stale {
			s.Stale++
		}
		if r.Status.Drift {
			s.Drift++
		}
	}
	s.NoConfigDetected = len(rows) == 0
	s.Healthy = s.Adoptable == 0 && s.Stale == 0 && s.Drift == 0

	if !s.Healthy {
		s.Suggested = suggestedCommand(rows, s)
	}
	return s
}

// suggestedCommand picks the most useful next command for the user based on
// where adoptable rows live and whether stale rows exist.
func suggestedCommand(rows []Row, s FooterSummary) string {
	if s.Adoptable > 0 {
		// Prefer global adoption when global has unmanaged adoptable rows —
		// it's the cross-repo win.
		for _, r := range rows {
			if r.Layer == LayerGlobal && r.Status.Unmanaged && adoptableChannels[r.Channel] {
				return "ainfra adopt --scope=user"
			}
		}
		return "ainfra adopt"
	}
	if s.Stale > 0 || s.Drift > 0 {
		return "ainfra install"
	}
	return ""
}

// mergeLocks unions committed and personal locks. Personal entries take
// precedence on a key collision — same shape as cmd/ainfra/commands.go.
func mergeLocks(committed, personal *lockfile.Lock) *lockfile.Lock {
	if committed == nil {
		committed = &lockfile.Lock{}
	}
	if personal == nil {
		personal = &lockfile.Lock{}
	}
	merge := func(a, b map[string]lockfile.Entry) map[string]lockfile.Entry {
		out := make(map[string]lockfile.Entry, len(a)+len(b))
		for k, v := range a {
			out[k] = v
		}
		for k, v := range b {
			out[k] = v
		}
		return out
	}
	return &lockfile.Lock{
		Entries: lockfile.Entries{
			MCPServers:         merge(committed.Entries.MCPServers, personal.Entries.MCPServers),
			BackgroundServices: merge(committed.Entries.BackgroundServices, personal.Entries.BackgroundServices),
			Hooks:              merge(committed.Entries.Hooks, personal.Entries.Hooks),
			Commands:           merge(committed.Entries.Commands, personal.Entries.Commands),
			CLITools:           merge(committed.Entries.CLITools, personal.Entries.CLITools),
			Skills:             merge(committed.Entries.Skills, personal.Entries.Skills),
			Marketplaces:       merge(committed.Entries.Marketplaces, personal.Entries.Marketplaces),
			Plugins:            merge(committed.Entries.Plugins, personal.Entries.Plugins),
			Rules:              merge(committed.Entries.Rules, personal.Entries.Rules),
			Tools:              merge(committed.Entries.Tools, personal.Entries.Tools),
		},
	}
}

// lookupLockEntry returns the lockfile entry for (channel, id), if any.
func lookupLockEntry(l *lockfile.Lock, channel, id string) (lockfile.Entry, bool) {
	if l == nil {
		return lockfile.Entry{}, false
	}
	var m map[string]lockfile.Entry
	switch channel {
	case "mcpServers":
		m = l.Entries.MCPServers
	case "hooks":
		m = l.Entries.Hooks
	case "commands":
		m = l.Entries.Commands
	case "skills":
		m = l.Entries.Skills
	case "plugins":
		m = l.Entries.Plugins
	case "rules":
		m = l.Entries.Rules
	case "cliTools":
		m = l.Entries.CLITools
	case "tools":
		m = l.Entries.Tools
	case "marketplaces":
		m = l.Entries.Marketplaces
	case "backgroundServices":
		m = l.Entries.BackgroundServices
	default:
		return lockfile.Entry{}, false
	}
	e, ok := m[id]
	return e, ok
}

// sourceForEntry resolves the human-friendly origin annotation for a
// managed row. team-layer entries surface as the manifest's extends source;
// repo and personal map to their friendly labels.
func sourceForEntry(layers map[manifest.Layer]*manifest.Manifest, layer manifest.Layer, channel, id string) string {
	switch layer {
	case manifest.LayerTeam:
		// Best-effort: if the repo layer declares an extends source, surface it.
		if repo, ok := layers[manifest.LayerRepo]; ok && repo != nil && len(repo.Extends) > 0 {
			return fmt.Sprintf("from: %s", repo.Extends[0].Location)
		}
		return "from: team manifest"
	case manifest.LayerRepo:
		return "from: repo manifest"
	case manifest.LayerPersonal:
		return "from: personal manifest"
	default:
		if layer == "" {
			return "from: lockfile"
		}
		return fmt.Sprintf("from: %s", layer)
	}
}

// annotateShadowed walks rows grouped by (channel, id) and tags lower-
// priority occurrences as shadowed. Precedence team < repo < personal
// matches cmd_list.go's annotateShadowed and applies per filesystem
// layer: a (channel, id) declared at multiple manifest layers visible
// in both filesystem layers gets the loser tagged.
func annotateShadowed(rows []Row, layers map[manifest.Layer]*manifest.Manifest) {
	// Determine, per (channel, id), the set of declaring filesystem layers.
	type key struct{ channel, id string }
	occurrences := map[key][]int{}
	for i, r := range rows {
		k := key{r.Channel, r.ID}
		occurrences[k] = append(occurrences[k], i)
	}
	for _, idxs := range occurrences {
		if len(idxs) < 2 {
			continue
		}
		// Sort by precedence: Project beats Global for shadowing display
		// (the project layer is the more specific override surface).
		sort.SliceStable(idxs, func(i, j int) bool {
			return layerPriority(rows[idxs[i]].Layer) < layerPriority(rows[idxs[j]].Layer)
		})
		winnerIdx := idxs[0]
		winnerLayer := rows[winnerIdx].Layer
		for _, idx := range idxs[1:] {
			rows[idx].Status.Shadowed = true
			rows[idx].ShadowedBy = string(winnerLayer)
		}
	}
	_ = layers // reserved for future per-manifest-layer shadowing logic
}

func layerPriority(l Layer) int {
	switch l {
	case LayerProject:
		return 0
	case LayerGlobal:
		return 1
	}
	return 9
}
