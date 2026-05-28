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
//	tracked   — declared in some ainfra manifest layer AND present locally
//	untracked — present locally, not declared in any ainfra manifest
//	missing   — declared in some ainfra manifest, not installed locally
//
// Drift detection (lockfile hash vs local file hash) is left to
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
	Notes         []string     `json:"notes,omitempty"`
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

	repoScan, _, err := adopt.Scan(ctx.Dir)
	if err != nil {
		ui.RenderError(ctx.Stderr, errColor, fmt.Errorf("scanning %s: %w", ctx.Dir, err))
		return 1
	}
	// Skills aren't covered by the adopt scanner yet, so walk
	// <root>/.claude/skills/ directly — each subdirectory is one skill.
	repoScan.Skills = mergeStringMap(repoScan.Skills, scanSkills(ctx.Dir))
	// Some repos (older Claude Code conventions, including this team's
	// train-service) store MCP config at .claude/mcp.json with a "servers"
	// key instead of the standard root .mcp.json with "mcpServers". Surface
	// those servers too so 'inspect' tells the truth about what's there.
	repoScan.MCPServers = mergeStringMap(repoScan.MCPServers,
		readMCPFallback(filepath.Join(ctx.Dir, ".claude", "mcp.json")))

	// Personal-layer entries source files out of ~/.claude/ rather than the
	// repo, so a repo-only scan would report them as missing even when
	// they're present. Track the user-scope scan separately so the default
	// view can still surface a repo-local file when the manifest claims its
	// id is "personal".
	var userScan manifest.Manifest
	if home, herr := os.UserHomeDir(); herr == nil {
		userScan, _, _ = adopt.ScanLayout(adopt.UserLayout(home))
		userScan.Skills = mergeStringMap(userScan.Skills, scanSkills(filepath.Join(home, ".claude")))
	}
	scanned := mergeManifests(repoScan, userScan)

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

	rows := collectInspectRows(repoScan, scanned, layers, lock)
	if !includeAll {
		rows = filterPersonalOnlyRows(rows, repoLocalIDSet(repoScan))
	}
	stats := summarize(rows)

	report := inspectReport{
		Repo:          ctx.Dir,
		HasManifest:   hasManifest,
		HasLock:       hasLock,
		Notes:         inspectNotes(ctx.Dir),
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
func collectInspectRows(repoScan, scanned manifest.Manifest, layers map[manifest.Layer]*manifest.Manifest, lock *lockfile.Lock) []inspectRow {
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
			default: // !disk[id] && manifold[id] != ""
				row.Status = statusMissing
			}
			// A repo-local file whose only manifest coverage is the user's
			// global personal layer is not team-managed by this repo —
			// re-classify as local-only so the row matches the user's
			// mental model ("does this repo manage this?" → no).
			if row.Status == statusTracked && row.Layer == string(manifest.LayerPersonal) && repoFileForChannel(repoScan, ch.name, id) {
				row.Status = statusUntracked
				row.Layer = ""
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
		hints = append(hints, "ainfra init --adopt    # create ainfra.yaml from what's already here, so teammates get the same setup")
		return hints
	}
	if s.Untracked > 0 {
		hints = append(hints, "ainfra init --adopt --force    # fold the local-only entries into ainfra.yaml so they're shared")
	}
	if s.Missing > 0 {
		hints = append(hints, "ainfra install                 # install the entries ainfra.yaml expects but that aren't here yet")
	}
	return hints
}

func renderInspectTable(w io.Writer, noColor bool, report inspectReport) {
	c := ui.NewColorizer(w, noColor)
	fmt.Fprintf(w, "Repo: %s\n", report.Repo)
	fmt.Fprintf(w, "ainfra: %s, %s\n",
		yesNo(report.HasManifest, "ainfra.yaml present", "no ainfra.yaml"),
		yesNo(report.HasLock, "ainfra.lock present", "no ainfra.lock"),
	)
	for _, n := range report.Notes {
		fmt.Fprintf(w, "Note: %s\n", c.Dim(n))
	}
	fmt.Fprintln(w)

	if len(report.Rows) == 0 {
		fmt.Fprintln(w, "Nothing for this repo: no MCP servers, commands, hooks, rules, or skills declared in ainfra.yaml or present in the repo.")
		if !report.HasManifest {
			fmt.Fprintln(w, "\nThis repo has not been adopted by ainfra. Run with --all to also list your global personal config.")
		} else {
			fmt.Fprintln(w, "\nRun with --all to also list your global personal config (applies across every repo).")
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
		fmt.Fprintln(w, c.Bold(channelLabel(ch)))
		for _, r := range rows {
			fmt.Fprintf(w, "  %s %-32s %s\n", statusMarker(c, r.Status), r.ID, c.Dim(statusPhrase(r)))
		}
		fmt.Fprintln(w)
	}

	fmt.Fprintf(w, "Summary: %d managed, %d local-only, %d not installed.\n",
		report.Summary.Tracked, report.Summary.Untracked, report.Summary.Missing)
	renderInspectLegend(w, c, report.Summary, report.HasManifest)
	renderInspectHints(w, report)
}

// channelLabel maps a channel name to a friendlier section header.
func channelLabel(ch string) string {
	switch ch {
	case "mcpServers":
		return "MCP servers"
	case "commands":
		return "commands"
	case "hooks":
		return "hooks"
	case "rules":
		return "rules"
	case "skills":
		return "skills"
	default:
		return ch
	}
}

// statusPhrase turns the inspectStatus + layer into one short noun phrase
// for the right of the id. No imperatives, no parenthetical — repeated on
// every row, so kept terse. The Legend block below the table is where the
// full sentence lives.
func statusPhrase(r inspectRow) string {
	switch r.Status {
	case statusTracked:
		switch r.Layer {
		case string(manifest.LayerPersonal):
			return "managed by your personal config"
		case string(manifest.LayerTeam):
			return "managed by ainfra (team)"
		default:
			return "managed by ainfra"
		}
	case statusUntracked:
		return "local-only"
	case statusMissing:
		return "declared but not installed"
	default:
		return ""
	}
}

// renderInspectLegend prints a one-time guide to the status markers — only
// for the statuses that actually appear in this report, with phrasing that
// adapts to whether the repo has an ainfra.yaml at all (so the legend
// doesn't tell the user something obvious about a file that doesn't exist).
func renderInspectLegend(w io.Writer, c ui.Colorizer, s inspectStats, hasManifest bool) {
	if s.Tracked+s.Untracked+s.Missing == 0 {
		return
	}
	fmt.Fprintln(w, "\n"+c.Bold("Legend:"))
	if s.Tracked > 0 {
		fmt.Fprintf(w, "  %s  managed by ainfra — kept in sync across machines that run ainfra\n", c.Green("OK"))
	}
	if s.Untracked > 0 {
		if hasManifest {
			fmt.Fprintf(w, "  %s  local-only — present here but ainfra.yaml does not declare it (teammates won't get it)\n", c.Yellow("++"))
		} else {
			fmt.Fprintf(w, "  %s  local-only — only here, not shared with teammates (no ainfra.yaml in this repo yet)\n", c.Yellow("++"))
		}
	}
	if s.Missing > 0 {
		fmt.Fprintf(w, "  %s  declared but not installed — ainfra.yaml expects it, but the file is not here yet\n", c.Red("--"))
	}
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

// inspectNotes surfaces relevant config files that 'inspect' intentionally
// does not classify so the user knows what was skipped vs missed.
func inspectNotes(dir string) []string {
	var notes []string
	if fileExists(filepath.Join(dir, ".claude", "settings.local.json")) {
		notes = append(notes, ".claude/settings.local.json present — gitignored local settings, not inspected.")
	}
	return notes
}

// filterPersonalOnlyRows drops rows whose only relationship to this repo is
// the user's global personal manifest. Rows for repo/team layer entries pass
// through, as do untracked rows (the question "is this repo using this?" is
// answered yes for repo/team and unknown for untracked). An entry whose
// manifest layer is personal but whose on-disk file lives inside the repo
// is *not* hidden — e.g. a repo's own CLAUDE.md collides on id "claude-md"
// with the personal-layer rule, and the repo-local file is what the user
// asked about. Pass --all to disable this filter entirely.
func filterPersonalOnlyRows(rows []inspectRow, repoLocal map[string]bool) []inspectRow {
	out := make([]inspectRow, 0, len(rows))
	for _, r := range rows {
		if r.Layer != string(manifest.LayerPersonal) {
			out = append(out, r)
			continue
		}
		if repoLocal[r.Channel+":"+r.ID] {
			out = append(out, r)
			continue
		}
		if r.Status == statusUntracked {
			out = append(out, r)
		}
	}
	return out
}

// repoFileForChannel reports whether a (channel, id) artefact was found
// inside the repo by the repo-scoped adopt scan (as opposed to the
// user-scope scan). Used to decide that a row whose manifest layer is
// personal but whose actual file lives in the repo should be classified
// as local-only rather than personal-managed.
func repoFileForChannel(repoScan manifest.Manifest, channel, id string) bool {
	switch channel {
	case "mcpServers":
		_, ok := repoScan.MCPServers[id]
		return ok
	case "commands":
		_, ok := repoScan.Commands[id]
		return ok
	case "hooks":
		_, ok := repoScan.Hooks[id]
		return ok
	case "rules":
		_, ok := repoScan.Rules[id]
		return ok
	case "skills":
		_, ok := repoScan.Skills[id]
		return ok
	}
	return false
}

// repoLocalIDSet returns the set of (channel, id) keys whose on-disk
// artefact lives inside the repo at scan time. Used by the personal-filter
// to keep repo-local files visible even when their id collides with a
// personal-layer manifest entry (e.g. a repo's CLAUDE.md vs the personal
// claude-md rule).
func repoLocalIDSet(repoScan manifest.Manifest) map[string]bool {
	out := map[string]bool{}
	for id := range repoScan.MCPServers {
		out["mcpServers:"+id] = true
	}
	for id := range repoScan.Commands {
		out["commands:"+id] = true
	}
	for id := range repoScan.Hooks {
		out["hooks:"+id] = true
	}
	for id := range repoScan.Rules {
		out["rules:"+id] = true
	}
	for id := range repoScan.Skills {
		out["skills:"+id] = true
	}
	return out
}

// readMCPFallback handles non-standard MCP config files that the adopt
// scanner skips: ones at <repo>/.claude/mcp.json (instead of <repo>/.mcp.json)
// and ones that use a top-level "servers" key (instead of "mcpServers").
// Older Claude Code conventions used this layout — without surfacing it,
// 'inspect' silently misreports a repo as having no MCP servers.
func readMCPFallback(path string) map[string]manifest.MCPServer {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var doc map[string]json.RawMessage
	if json.Unmarshal(raw, &doc) != nil {
		return nil
	}
	body, ok := doc["mcpServers"]
	if !ok {
		body, ok = doc["servers"]
	}
	if !ok {
		return nil
	}
	var entries map[string]map[string]any
	if json.Unmarshal(body, &entries) != nil {
		return nil
	}
	out := map[string]manifest.MCPServer{}
	for id, fields := range entries {
		srv := manifest.MCPServer{}
		if cmd, ok := fields["command"].(string); ok {
			srv.Command = cmd
		}
		if args, ok := fields["args"].([]any); ok {
			for _, a := range args {
				if s, ok := a.(string); ok {
					srv.Args = append(srv.Args, s)
				}
			}
		}
		out[id] = srv
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
