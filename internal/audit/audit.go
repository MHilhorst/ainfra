// Package audit implements `ainfra audit`: a read-only, layered inventory
// of Claude Code config across the Global (~/.claude) and Project (.claude/)
// filesystem layers, cross-referenced against ainfra's manifest + lockfile
// for management status and source annotation.
//
// audit composes against existing infrastructure rather than duplicating it:
//   - internal/adopt.Layout / ScanLayout for the channels adopt already
//     enumerates (mcpServers, hooks, commands, rules);
//   - internal/manifest.LoadLayers + internal/lockfile.Read for managed
//     status and source annotation;
//   - new filesystem scanners in scan.go for the channels adopt does not
//     enumerate today (skills, plugins, agents, settings).
package audit

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/MHilhorst/ainfra/internal/cli"
	"github.com/MHilhorst/ainfra/internal/lockfile"
	"github.com/MHilhorst/ainfra/internal/manifest"
	"github.com/MHilhorst/ainfra/internal/ui"
)

// Layer is the display layer the row was discovered in. Distinct from
// manifest.Layer (team/repo/personal): audit displays the on-disk
// filesystem layer and folds the manifest layer into per-row Source.
type Layer string

const (
	// LayerGlobal is the user-wide ~/.claude/ tree.
	LayerGlobal Layer = "global"
	// LayerProject is the current repo's .claude/ tree.
	LayerProject Layer = "project"
)

// Status is a bitset of management states. A row can be both managed and
// stale, or managed and shadowed; the renderer composes the visible tags.
type Status struct {
	Managed   bool `json:"managed,omitempty"`
	Unmanaged bool `json:"unmanaged,omitempty"`
	Shadowed  bool `json:"shadowed,omitempty"`
	Stale     bool `json:"stale,omitempty"`
	Drift     bool `json:"drift,omitempty"`
	// Gitignored marks rows backed by a gitignored on-disk artifact
	// (currently only .claude/settings.local.json). The renderer surfaces
	// this distinctly so the reader knows the row is per-developer.
	Gitignored bool `json:"gitignored,omitempty"`
}

// Row is one inventory line.
type Row struct {
	Layer   Layer  `json:"layer"`
	Channel string `json:"channel"`
	ID      string `json:"id"`
	// Version mirrors lockfile.Entry.Version when the row is managed; "" otherwise.
	Version string `json:"version,omitempty"`
	Status  Status `json:"status"`
	// Source is the human-friendly origin annotation, e.g.
	// "from: repo manifest", "from: personal manifest",
	// "from: github:org/team-config@1.2.0". Empty for unmanaged rows.
	Source string `json:"source,omitempty"`
	// ShadowedBy names the winning layer when Status.Shadowed is true.
	ShadowedBy string `json:"shadowedBy,omitempty"`
	// Detail is optional channel-specific annotation surfaced by the
	// renderer (e.g. settings.json summary: "12 permissions · hooks block").
	Detail string `json:"detail,omitempty"`
}

// FooterNote is a one-line layer-level message rendered before the summary
// (e.g. "Project section omitted — no .claude/ found in this directory").
type FooterNote struct {
	Layer   Layer  `json:"layer,omitempty"`
	Message string `json:"message"`
}

// FooterSummary is the audit-level summary line rendered after all rows.
type FooterSummary struct {
	Adoptable int    `json:"adoptable"`
	Stale     int    `json:"stale"`
	Drift     int    `json:"drift"`
	// Suggested is the next command we recommend, or empty when there's
	// nothing actionable.
	Suggested string `json:"suggested,omitempty"`
	// Healthy is true when there are no unmanaged-adoptable rows and no
	// stale/drift signals.
	Healthy bool `json:"healthy"`
	// NoConfigDetected is true when audit found zero rows across both
	// layers (genuinely fresh machine). Renderer adjusts the footer text.
	NoConfigDetected bool `json:"noConfigDetected,omitempty"`
}

// Options controls Run's output.
type Options struct {
	JSON bool
}

// Run is the entry point invoked by `cmd/ainfra/cmd_audit.go`. It scans both
// layers, reconciles against manifest + lockfile, and renders.
func Run(ctx cli.Context, opts Options) int {
	errColor := ui.NewColorizer(ctx.Stderr, ctx.NoColor)

	var rows []Row
	var notes []FooterNote

	// Global layer (~/.claude/). Always considered.
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		globalRows, globalNotes, err := scanGlobal(home)
		if err != nil {
			ui.RenderError(ctx.Stderr, errColor, fmt.Errorf("audit: scanning global layer: %w", err))
			return 1
		}
		rows = append(rows, globalRows...)
		notes = append(notes, globalNotes...)
	} else {
		notes = append(notes, FooterNote{
			Layer:   LayerGlobal,
			Message: "Global section omitted — could not resolve user home directory",
		})
	}

	// Project layer (<ctx.Dir>/.claude/). Omitted entirely when the dir has
	// neither .claude/ nor ainfra.yaml — R5.
	if projectApplicable(ctx.Dir) {
		projectRows, projectNotes, err := scanProject(ctx.Dir)
		if err != nil {
			ui.RenderError(ctx.Stderr, errColor, fmt.Errorf("audit: scanning project layer: %w", err))
			return 1
		}
		rows = append(rows, projectRows...)
		notes = append(notes, projectNotes...)
	} else {
		notes = append(notes, FooterNote{
			Layer:   LayerProject,
			Message: "Project section omitted — no .claude/ or ainfra.yaml in this directory",
		})
	}

	// Reconcile: cross-reference rows with manifest + lockfile to compute
	// Status and Source. Reconcile tolerates missing manifests and lockfiles.
	layers, _ := manifest.LoadLayers(ctx.Dir)
	committed, _ := lockfile.Read(filepath.Join(ctx.Dir, "ainfra.lock"))
	personal, _ := lockfile.Read(filepath.Join(ctx.Dir, "ainfra.personal.lock"))
	rows = Reconcile(rows, layers, committed, personal)

	footer := BuildFooter(rows)

	if opts.JSON {
		return RenderJSON(ctx.Stdout, rows, footer, notes)
	}
	return RenderText(ctx.Stdout, ui.NewColorizer(ctx.Stdout, ctx.NoColor), rows, footer, notes, ctx.Dir)
}

// projectApplicable reports whether the Project layer section should be
// rendered for dir. Per R5, audit omits Project when dir has neither
// `.claude/` nor `ainfra.yaml`.
func projectApplicable(dir string) bool {
	if dir == "" {
		return false
	}
	if info, err := os.Stat(filepath.Join(dir, ".claude")); err == nil && info.IsDir() {
		return true
	}
	if _, err := os.Stat(filepath.Join(dir, "ainfra.yaml")); err == nil {
		return true
	}
	return false
}
