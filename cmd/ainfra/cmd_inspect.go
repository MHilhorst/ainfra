package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/MHilhorst/ainfra/internal/adopt"
	"github.com/MHilhorst/ainfra/internal/cli"
	"github.com/MHilhorst/ainfra/internal/lockfile"
	"github.com/MHilhorst/ainfra/internal/manifest"
	"github.com/MHilhorst/ainfra/internal/ui"
)

// inspectStatus classifies one (channel, id) pair against the repo state.
//
//	tracked   — declared in some ainfra manifest layer AND present on disk
//	untracked — present on disk, not declared in any manifest layer
//	missing   — declared in some manifest layer, absent on disk
//
// Drift detection (lockfile hash vs on-disk hash) is left to
// `ainfra update --dry-run` so this command stays read-only and side-effect-free.
type inspectStatus string

const (
	statusTracked   inspectStatus = "tracked"
	statusUntracked inspectStatus = "untracked"
	statusMissing   inspectStatus = "missing"
)

// inspectRow is one row of the inspect report — JSON-stable.
type inspectRow struct {
	Channel string        `json:"channel"`
	ID      string        `json:"id"`
	Status  inspectStatus `json:"status"`
	Layer   string        `json:"layer,omitempty"`
	Detail  string        `json:"detail,omitempty"`
}

// inspectReport is the top-level structure emitted by `ainfra inspect --json`.
type inspectReport struct {
	Repo          string       `json:"repo"`
	HasManifest   bool         `json:"hasManifest"`
	HasLock       bool         `json:"hasLock"`
	Rows          []inspectRow `json:"rows"`
	Summary       inspectStats `json:"summary"`
	NextStepHints []string     `json:"nextStepHints,omitempty"`
}

type inspectStats struct {
	Tracked   int `json:"tracked"`
	Untracked int `json:"untracked"`
	Missing   int `json:"missing"`
}

func newInspectCommand() *cli.Command {
	var asJSON bool
	var includeAll bool
	return &cli.Command{
		Name:      "inspect",
		Summary:   "Inspect a repo's Claude Code config and what ainfra tracks",
		UsageLine: "ainfra inspect [--all] [--json]",
		Example:   "ainfra inspect",
		SetFlags: func(fs *flag.FlagSet) {
			fs.BoolVar(&asJSON, "json", false, "emit a JSON report instead of a table")
			fs.BoolVar(&includeAll, "all", false, "include personal-layer entries (global, not specific to this repo)")
		},
		Run: func(ctx cli.Context) int {
			return runInspect(ctx, asJSON, includeAll)
		},
	}
}

func runInspect(ctx cli.Context, asJSON, includeAll bool) int {
	errColor := ui.NewColorizer(ctx.Stderr, ctx.NoColor)

	scanned, _, err := adopt.Scan(ctx.Dir)
	if err != nil {
		ui.RenderError(ctx.Stderr, errColor, fmt.Errorf("scanning %s: %w", ctx.Dir, err))
		return 1
	}
	// Skills aren't covered by the adopt scanner yet, so walk
	// <root>/.claude/skills/ directly — each subdirectory is one skill.
	if skills := scanSkills(ctx.Dir); len(skills) > 0 {
		if scanned.Skills == nil {
			scanned.Skills = map[string]manifest.Skill{}
		}
		for id, s := range skills {
			scanned.Skills[id] = s
		}
	}
	// Personal-layer entries source files out of ~/.claude/ rather than the
	// repo, so a repo-only scan would report them as missing even when
	// they're present. Merge in a user-scope scan; conflicts are resolved
	// in favor of the repo scan since IDs there refer to in-repo files.
	if home, herr := os.UserHomeDir(); herr == nil {
		userScan, _, _ := adopt.ScanLayout(adopt.UserLayout(home))
		scanned = mergeManifests(scanned, userScan)
		if skills := scanSkills(filepath.Join(home, ".claude")); len(skills) > 0 {
			if scanned.Skills == nil {
				scanned.Skills = map[string]manifest.Skill{}
			}
			for id, s := range skills {
				if _, exists := scanned.Skills[id]; !exists {
					scanned.Skills[id] = s
				}
			}
		}
	}

	// LoadLayers errors loudly on a malformed manifest; on a virgin repo it
	// just returns an empty map. Treat both as "no manifest" so inspect
	// works the same on a fresh clone and on an adopted repo.
	var layers map[manifest.Layer]*manifest.Manifest
	if l, lerr := manifest.LoadLayers(ctx.Dir); lerr == nil {
		layers = l
	}
	hasManifest := manifestExists(ctx.Dir)

	lockPath := filepath.Join(ctx.Dir, "ainfra.lock")
	hasLock := fileExists(lockPath)
	var lock *lockfile.Lock
	if hasLock {
		if l, lerr := lockfile.Read(lockPath); lerr == nil {
			lock = l
		}
	}

	rows := collectInspectRows(scanned, layers, lock)
	if !includeAll {
		rows = filterPersonalOnlyRows(rows)
	}
	stats := summarize(rows)

	report := inspectReport{
		Repo:          ctx.Dir,
		HasManifest:   hasManifest,
		HasLock:       hasLock,
		Rows:          rows,
		Summary:       stats,
		NextStepHints: nextStepHints(hasManifest, stats),
	}

	if asJSON {
		return emitInspectJSON(ctx.Stdout, report, errColor, ctx.Stderr)
	}
	renderInspectTable(ctx.Stdout, ctx.NoColor, report)
	return 0
}

// collectInspectRows walks every supported channel and classifies each id.
// scanned holds what was discovered on disk; layers holds what's declared
// across all loaded manifest layers; lock is consulted for the layer name
// when an entry only appears in the lockfile (rare — committed lock with
// pruned manifest).
func collectInspectRows(scanned manifest.Manifest, layers map[manifest.Layer]*manifest.Manifest, lock *lockfile.Lock) []inspectRow {
	var rows []inspectRow

	type channel struct {
		name       string
		diskIDs    func(manifest.Manifest) []string
		manifIDs   func(*manifest.Manifest) []string
		lockIDs    func(*lockfile.Lock) []string
		entryLayer func(*lockfile.Lock, string) string
	}
	channels := []channel{
		{
			name:     "mcpServers",
			diskIDs:  func(m manifest.Manifest) []string { return keys(m.MCPServers) },
			manifIDs: func(m *manifest.Manifest) []string { return keys(m.MCPServers) },
			lockIDs:  func(l *lockfile.Lock) []string { return keys(l.Entries.MCPServers) },
			entryLayer: func(l *lockfile.Lock, id string) string {
				if l == nil {
					return ""
				}
				return l.Entries.MCPServers[id].Layer
			},
		},
		{
			name:     "commands",
			diskIDs:  func(m manifest.Manifest) []string { return keys(m.Commands) },
			manifIDs: func(m *manifest.Manifest) []string { return keys(m.Commands) },
			lockIDs:  func(l *lockfile.Lock) []string { return keys(l.Entries.Commands) },
			entryLayer: func(l *lockfile.Lock, id string) string {
				if l == nil {
					return ""
				}
				return l.Entries.Commands[id].Layer
			},
		},
		{
			name:     "hooks",
			diskIDs:  func(m manifest.Manifest) []string { return keys(m.Hooks) },
			manifIDs: func(m *manifest.Manifest) []string { return keys(m.Hooks) },
			lockIDs:  func(l *lockfile.Lock) []string { return keys(l.Entries.Hooks) },
			entryLayer: func(l *lockfile.Lock, id string) string {
				if l == nil {
					return ""
				}
				return l.Entries.Hooks[id].Layer
			},
		},
		{
			name:     "rules",
			diskIDs:  func(m manifest.Manifest) []string { return keys(m.Rules) },
			manifIDs: func(m *manifest.Manifest) []string { return keys(m.Rules) },
			lockIDs:  func(l *lockfile.Lock) []string { return keys(l.Entries.Rules) },
			entryLayer: func(l *lockfile.Lock, id string) string {
				if l == nil {
					return ""
				}
				return l.Entries.Rules[id].Layer
			},
		},
		{
			name:     "skills",
			diskIDs:  func(m manifest.Manifest) []string { return keys(m.Skills) },
			manifIDs: func(m *manifest.Manifest) []string { return keys(m.Skills) },
			lockIDs:  func(l *lockfile.Lock) []string { return keys(l.Entries.Skills) },
			entryLayer: func(l *lockfile.Lock, id string) string {
				if l == nil {
					return ""
				}
				return l.Entries.Skills[id].Layer
			},
		},
	}

	for _, ch := range channels {
		disk := stringSet(ch.diskIDs(scanned))
		manifold := map[string]string{} // id -> layer
		for layerName, m := range layers {
			if m == nil {
				continue
			}
			for _, id := range ch.manifIDs(m) {
				if _, ok := manifold[id]; !ok {
					manifold[id] = string(layerName)
				}
			}
		}
		// IDs in the lock but not in any current manifest layer are
		// orphaned-lock entries — surface them as tracked-but-missing so
		// the user notices the stale lock. Skip when there's no lock at all.
		var lockIDList []string
		if lock != nil {
			lockIDList = ch.lockIDs(lock)
		}
		for _, id := range lockIDList {
			if _, ok := manifold[id]; !ok {
				manifold[id] = ch.entryLayer(lock, id)
			}
		}

		ids := union(disk, mapKeys(manifold))
		sort.Strings(ids)

		for _, id := range ids {
			row := inspectRow{Channel: ch.name, ID: id, Layer: manifold[id]}
			switch {
			case disk[id] && manifold[id] != "":
				row.Status = statusTracked
			case disk[id] && manifold[id] == "":
				row.Status = statusUntracked
				row.Detail = "on disk, not in ainfra.yaml — run `ainfra init --adopt --force` to absorb"
			default: // !disk[id] && manifold[id] != ""
				row.Status = statusMissing
				row.Detail = "in manifest, not on disk — run `ainfra install` to materialize"
			}
			rows = append(rows, row)
		}
	}

	return rows
}

func summarize(rows []inspectRow) inspectStats {
	var s inspectStats
	for _, r := range rows {
		switch r.Status {
		case statusTracked:
			s.Tracked++
		case statusUntracked:
			s.Untracked++
		case statusMissing:
			s.Missing++
		}
	}
	return s
}

func nextStepHints(hasManifest bool, s inspectStats) []string {
	var hints []string
	if !hasManifest {
		hints = append(hints, "ainfra init --adopt    # turn this inventory into ainfra.yaml")
		return hints
	}
	if s.Untracked > 0 {
		hints = append(hints, "ainfra init --adopt --force    # absorb untracked entries into ainfra.yaml")
	}
	if s.Missing > 0 {
		hints = append(hints, "ainfra install    # materialize missing manifest entries on disk")
	}
	return hints
}

func renderInspectTable(w io.Writer, noColor bool, report inspectReport) {
	c := ui.NewColorizer(w, noColor)
	fmt.Fprintf(w, "Repo: %s\n", report.Repo)
	fmt.Fprintf(w, "ainfra: %s, %s\n\n",
		yesNo(report.HasManifest, "ainfra.yaml present", "no ainfra.yaml"),
		yesNo(report.HasLock, "ainfra.lock present", "no ainfra.lock"),
	)

	if len(report.Rows) == 0 {
		fmt.Fprintln(w, "No repo-scope mcpServers, commands, hooks, rules, or skills found on disk or in this repo's manifest.")
		if !report.HasManifest {
			fmt.Fprintln(w, "\nNothing to inspect — this is a virgin repo (re-run with --all to see your personal-layer config).")
		} else {
			fmt.Fprintln(w, "\nRe-run with --all to also list your personal-layer entries (apply globally, not specific to this repo).")
		}
		renderInspectHints(w, report)
		return
	}

	byChannel := map[string][]inspectRow{}
	for _, r := range report.Rows {
		byChannel[r.Channel] = append(byChannel[r.Channel], r)
	}
	channelOrder := []string{"mcpServers", "commands", "hooks", "rules", "skills"}

	for _, ch := range channelOrder {
		rows, ok := byChannel[ch]
		if !ok {
			continue
		}
		fmt.Fprintln(w, c.Bold(ch))
		for _, r := range rows {
			marker := statusMarker(c, r.Status)
			label := fmt.Sprintf("%-30s %-10s", r.ID, string(r.Status))
			if r.Layer != "" {
				label = fmt.Sprintf("%s layer=%s", label, r.Layer)
			}
			fmt.Fprintf(w, "  %s %s\n", marker, label)
			if r.Detail != "" {
				fmt.Fprintf(w, "        %s\n", c.Dim(r.Detail))
			}
		}
		fmt.Fprintln(w)
	}

	fmt.Fprintf(w, "Summary: %d tracked, %d untracked, %d missing.\n",
		report.Summary.Tracked, report.Summary.Untracked, report.Summary.Missing)
	renderInspectHints(w, report)
}

func renderInspectHints(w io.Writer, report inspectReport) {
	if len(report.NextStepHints) == 0 {
		return
	}
	fmt.Fprintln(w, "\nNext:")
	for _, h := range report.NextStepHints {
		fmt.Fprintf(w, "  %s\n", h)
	}
}

func statusMarker(c ui.Colorizer, s inspectStatus) string {
	switch s {
	case statusTracked:
		return c.Green("OK")
	case statusUntracked:
		return c.Yellow("++")
	case statusMissing:
		return c.Red("--")
	default:
		return "??"
	}
}

func yesNo(cond bool, yes, no string) string {
	if cond {
		return yes
	}
	return no
}

func emitInspectJSON(stdout io.Writer, report inspectReport, errColor ui.Colorizer, stderr io.Writer) int {
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		ui.RenderError(stderr, errColor, err)
		return 1
	}
	return 0
}

func manifestExists(dir string) bool {
	return fileExists(filepath.Join(dir, "ainfra.yaml"))
}

// filterPersonalOnlyRows drops rows whose only relationship to this repo is
// the user's global personal manifest. Rows for repo/team layer entries pass
// through, as do untracked rows (the question "is this repo using this?" is
// answered yes for repo/team and unknown for untracked). Pass --all to
// disable this filter.
func filterPersonalOnlyRows(rows []inspectRow) []inspectRow {
	out := make([]inspectRow, 0, len(rows))
	for _, r := range rows {
		if r.Status == statusTracked && r.Layer == string(manifest.LayerPersonal) {
			continue
		}
		if r.Status == statusMissing && r.Layer == string(manifest.LayerPersonal) {
			continue
		}
		out = append(out, r)
	}
	return out
}

// scanSkills walks <root>/.claude/skills/ and returns each subdirectory as a
// Skill draft. A skill is "a directory containing a SKILL.md" by convention;
// we accept any subdirectory as a candidate id so the inspect view stays
// generous about what counts as installed.
func scanSkills(root string) map[string]manifest.Skill {
	dir := filepath.Join(root, ".claude", "skills")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	out := map[string]manifest.Skill{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		out[e.Name()] = manifest.Skill{}
	}
	return out
}

// mergeManifests unions IDs across a primary and secondary draft manifest.
// Repo entries (primary) win on conflicts because their IDs refer to
// in-repo files; the user-scope manifest fills in personal-layer IDs that
// only exist under ~/.claude/.
func mergeManifests(primary, secondary manifest.Manifest) manifest.Manifest {
	primary.MCPServers = mergeStringMap(primary.MCPServers, secondary.MCPServers)
	primary.Commands = mergeStringMap(primary.Commands, secondary.Commands)
	primary.Hooks = mergeStringMap(primary.Hooks, secondary.Hooks)
	primary.Rules = mergeStringMap(primary.Rules, secondary.Rules)
	primary.Skills = mergeStringMap(primary.Skills, secondary.Skills)
	return primary
}

func mergeStringMap[V any](primary, secondary map[string]V) map[string]V {
	if len(secondary) == 0 {
		return primary
	}
	if primary == nil {
		primary = map[string]V{}
	}
	for k, v := range secondary {
		if _, ok := primary[k]; !ok {
			primary[k] = v
		}
	}
	return primary
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func mapKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func stringSet(in []string) map[string]bool {
	out := make(map[string]bool, len(in))
	for _, s := range in {
		out[s] = true
	}
	return out
}

func union(a map[string]bool, b []string) []string {
	out := make(map[string]bool, len(a)+len(b))
	for k := range a {
		out[k] = true
	}
	for _, k := range b {
		out[k] = true
	}
	keys := make([]string, 0, len(out))
	for k := range out {
		keys = append(keys, k)
	}
	return keys
}
