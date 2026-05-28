package ui

import (
	"fmt"
	"io"
	"sort"

	"github.com/MHilhorst/ainfra/internal/provider"
)

// RenderPlan writes a plan diff to w. For each non-noop change it prints a
// line with the change symbol (+/~/−), the channel.id key, and the detail.
// After iterating all channels it prints a summary line. If there are no
// non-noop changes anywhere it prints the no-changes message instead.
//
// Lines are aligned in two columns: the channel.id key on the left and the
// detail phrase on the right, sized to the widest key in this report so the
// detail column stays visually anchored.
func RenderPlan(w io.Writer, c Colorizer, plans map[string]provider.ChannelPlan) {
	channels := make([]string, 0, len(plans))
	for ch := range plans {
		channels = append(channels, ch)
	}
	sort.Strings(channels)

	type row struct {
		sym    string
		name   string
		detail string
	}
	var rows []row
	var adds, changes, destroys int

	for _, ch := range channels {
		plan := plans[ch]
		for _, change := range plan.Changes {
			switch change.Kind {
			case provider.ChangeCreate:
				adds++
			case provider.ChangeUpdate:
				changes++
			case provider.ChangeDelete:
				destroys++
			default:
				continue
			}
			var sym string
			switch change.Kind {
			case provider.ChangeCreate:
				sym = c.Green("+")
			case provider.ChangeUpdate:
				sym = c.Yellow("~")
			case provider.ChangeDelete:
				sym = c.Red("-")
			}
			rows = append(rows, row{sym: sym, name: ch + "." + change.ID, detail: change.Detail})
		}
	}

	if adds+changes+destroys == 0 {
		fmt.Fprintln(w, "No changes. Your environment already matches the lockfile.")
		return
	}

	// Align detail column to the longest name so the output reads as a
	// table rather than a ragged list.
	width := 0
	for _, r := range rows {
		if len(r.name) > width {
			width = len(r.name)
		}
	}
	for _, r := range rows {
		if r.detail == "" {
			fmt.Fprintf(w, "  %s %s\n", r.sym, r.name)
		} else {
			fmt.Fprintf(w, "  %s %-*s  %s\n", r.sym, width, r.name, c.Dim(r.detail))
		}
	}

	fmt.Fprintf(w, "\nPlan: %d to install, %d to update, %d to remove.\n", adds, changes, destroys)
}
