package audit

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"github.com/MHilhorst/ainfra/internal/ui"
)

// channelOrder fixes the per-layer channel order so the renderer output is
// stable across runs.
var channelOrder = []string{
	"mcpServers",
	"hooks",
	"commands",
	"skills",
	"plugins",
	"agents",
	"settings",
	"rules",
}

// RenderText prints rows + footer in the human layout. Returns the process
// exit code.
func RenderText(w io.Writer, c ui.Colorizer, rows []Row, footer FooterSummary, notes []FooterNote, dir string) int {
	byLayer := groupByLayer(rows)
	notesByLayer := groupNotesByLayer(notes)

	renderLayer(w, c, "GLOBAL (~/.claude)", byLayer[LayerGlobal], notesByLayer[LayerGlobal])
	fmt.Fprintln(w)
	renderLayer(w, c, fmt.Sprintf("PROJECT (.claude in %s)", filepath.Base(dir)), byLayer[LayerProject], notesByLayer[LayerProject])

	renderFooter(w, c, footer, notesByLayer[""])
	return 0
}

// RenderJSON emits one JSON object per row followed by a final footer
// object keyed `{"footer": ...}`. Notes are emitted before rows so streaming
// consumers see them first.
func RenderJSON(w io.Writer, rows []Row, footer FooterSummary, notes []FooterNote) int {
	enc := json.NewEncoder(w)
	for _, n := range notes {
		if err := enc.Encode(map[string]any{"note": n}); err != nil {
			return 1
		}
	}
	for _, r := range rows {
		if err := enc.Encode(r); err != nil {
			return 1
		}
	}
	if err := enc.Encode(map[string]any{"footer": footer}); err != nil {
		return 1
	}
	return 0
}

func renderLayer(w io.Writer, c ui.Colorizer, header string, rows []Row, notes []FooterNote) {
	fmt.Fprintln(w, c.Bold(header))
	for _, n := range notes {
		fmt.Fprintf(w, "  %s\n", c.Yellow(n.Message))
	}
	if len(rows) == 0 {
		if len(notes) == 0 {
			fmt.Fprintln(w, "  (no entries)")
		}
		return
	}

	grouped := groupByChannel(rows)
	for _, ch := range channelOrder {
		chRows, ok := grouped[ch]
		if !ok {
			continue
		}
		fmt.Fprintf(w, "  %s\n", c.Bold(ch))
		for _, r := range chRows {
			renderRow(w, c, r)
		}
	}
	// Any channels not in channelOrder (shouldn't happen, but stay honest).
	for ch, chRows := range grouped {
		if containsString(channelOrder, ch) {
			continue
		}
		fmt.Fprintf(w, "  %s\n", c.Bold(ch))
		for _, r := range chRows {
			renderRow(w, c, r)
		}
	}
}

func renderRow(w io.Writer, c ui.Colorizer, r Row) {
	version := r.Version
	if version == "" {
		version = "—"
	}
	tags := tagString(r.Status, r.ShadowedBy)
	source := r.Source
	if source != "" {
		source = "  " + c.Dim(source)
	}
	detail := r.Detail
	if detail != "" {
		detail = "  " + c.Dim("("+detail+")")
	}
	fmt.Fprintf(w, "    %-32s %-10s %s%s%s\n", r.ID, version, tags, source, detail)
}

// tagString composes the visible status tags from a Status bitset.
func tagString(s Status, shadowedBy string) string {
	var parts []string
	if s.Managed {
		parts = append(parts, "[managed]")
	}
	if s.Unmanaged {
		parts = append(parts, "[unmanaged]")
	}
	if s.Shadowed {
		if shadowedBy != "" {
			parts = append(parts, fmt.Sprintf("[shadowed-by: %s]", shadowedBy))
		} else {
			parts = append(parts, "[shadowed]")
		}
	}
	if s.Stale {
		parts = append(parts, "[stale]")
	}
	if s.Drift {
		parts = append(parts, "[drift]")
	}
	if s.Gitignored {
		parts = append(parts, "[gitignored]")
	}
	return strings.Join(parts, " ")
}

func renderFooter(w io.Writer, c ui.Colorizer, f FooterSummary, audGlobalNotes []FooterNote) {
	fmt.Fprintln(w)
	if f.NoConfigDetected {
		fmt.Fprintln(w, c.Dim("no Claude config detected"))
		return
	}
	if f.Healthy {
		fmt.Fprintln(w, c.Green("✓ all detected config is managed by ainfra"))
		return
	}
	var parts []string
	if f.Adoptable > 0 {
		parts = append(parts, fmt.Sprintf("%d adoptable", f.Adoptable))
	}
	if f.Stale > 0 {
		parts = append(parts, fmt.Sprintf("%d stale", f.Stale))
	}
	if f.Drift > 0 {
		parts = append(parts, fmt.Sprintf("%d drift", f.Drift))
	}
	if len(parts) > 0 {
		fmt.Fprintln(w, strings.Join(parts, " · "))
	}
	if f.Suggested != "" {
		fmt.Fprintf(w, "run %s to start\n", c.Bold("`"+f.Suggested+"`"))
	}
}

func groupByLayer(rows []Row) map[Layer][]Row {
	out := map[Layer][]Row{}
	for _, r := range rows {
		out[r.Layer] = append(out[r.Layer], r)
	}
	return out
}

func groupByChannel(rows []Row) map[string][]Row {
	out := map[string][]Row{}
	for _, r := range rows {
		out[r.Channel] = append(out[r.Channel], r)
	}
	for k, v := range out {
		sort.SliceStable(v, func(i, j int) bool { return v[i].ID < v[j].ID })
		out[k] = v
	}
	return out
}

func groupNotesByLayer(notes []FooterNote) map[Layer][]FooterNote {
	out := map[Layer][]FooterNote{}
	for _, n := range notes {
		out[n.Layer] = append(out[n.Layer], n)
	}
	return out
}

func containsString(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}
