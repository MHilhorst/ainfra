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
func RenderPlan(w io.Writer, c Colorizer, plans map[string]provider.ChannelPlan) {
	channels := make([]string, 0, len(plans))
	for ch := range plans {
		channels = append(channels, ch)
	}
	sort.Strings(channels)

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
			name := ch + "." + change.ID
			var sym string
			switch change.Kind {
			case provider.ChangeCreate:
				sym = c.Green("+")
			case provider.ChangeUpdate:
				sym = c.Yellow("~")
			case provider.ChangeDelete:
				sym = c.Red("-")
			}
			if change.Detail == "" {
				fmt.Fprintf(w, "  %s %s\n", sym, name)
			} else {
				fmt.Fprintf(w, "  %s %s  %s\n", sym, name, c.Dim(change.Detail))
			}
		}
	}

	if adds+changes+destroys == 0 {
		fmt.Fprintln(w, "No changes. Your environment already matches the lockfile.")
		return
	}

	fmt.Fprintf(w, "%d to add, %d to update, %d to remove.\n", adds, changes, destroys)
}
