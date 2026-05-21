package cli

import (
	"flag"
	"fmt"

	"github.com/MHilhorst/ainfra/internal/ui"
)

// printOverview writes the no-command overview: tagline, usage, the command
// table, and the global flags.
func (r *Registry) printOverview() {
	c := ui.NewColorizer(r.stdout, false)
	fmt.Fprintln(r.stdout, "ainfra — config-as-code for Claude Code team environments")
	fmt.Fprintln(r.stdout)
	fmt.Fprintln(r.stdout, c.Bold("Usage:"))
	fmt.Fprintln(r.stdout, "  ainfra <command> [flags]")
	fmt.Fprintln(r.stdout)
	fmt.Fprintln(r.stdout, c.Bold("Commands:"))
	for _, cmd := range r.commands {
		fmt.Fprintf(r.stdout, "  %-10s %s\n", cmd.Name, cmd.Summary)
	}
	fmt.Fprintln(r.stdout)
	fmt.Fprintln(r.stdout, c.Bold("Global flags:"))
	fmt.Fprintln(r.stdout, "  --chdir <dir>   Run as if started in <dir>")
	fmt.Fprintln(r.stdout, "  --no-color      Disable colored output")
	fmt.Fprintln(r.stdout)
	fmt.Fprintln(r.stdout, `Run "ainfra <command> --help" for command-specific detail.`)
}

// printCommandHelp writes per-command help: summary, usage, flags, example.
func (r *Registry) printCommandHelp(cmd *Command) {
	c := ui.NewColorizer(r.stdout, false)
	fmt.Fprintf(r.stdout, "ainfra %s — %s\n", cmd.Name, cmd.Summary)
	fmt.Fprintln(r.stdout)
	fmt.Fprintln(r.stdout, c.Bold("Usage:"))
	fmt.Fprintln(r.stdout, "  "+cmd.UsageLine)
	if cmd.SetFlags != nil {
		fs := flag.NewFlagSet(cmd.Name, flag.ContinueOnError)
		cmd.SetFlags(fs)
		shown := false
		fs.VisitAll(func(f *flag.Flag) {
			if !shown {
				fmt.Fprintln(r.stdout)
				fmt.Fprintln(r.stdout, c.Bold("Flags:"))
				shown = true
			}
			fmt.Fprintf(r.stdout, "  --%-12s %s\n", f.Name, f.Usage)
		})
	}
	if cmd.Example != "" {
		fmt.Fprintln(r.stdout)
		fmt.Fprintln(r.stdout, c.Bold("Example:"))
		fmt.Fprintln(r.stdout, "  "+cmd.Example)
	}
}

// printUnknown writes an unknown-command error to stderr, with a did-you-mean
// suggestion when a registered command is within edit distance 2.
func (r *Registry) printUnknown(name string) {
	c := ui.NewColorizer(r.stderr, false)
	fmt.Fprintf(r.stderr, "%s unknown command %q\n", c.Red("ainfra:"), name)
	if s := r.closest(name); s != "" {
		fmt.Fprintln(r.stderr)
		fmt.Fprintf(r.stderr, "Did you mean %q?\n", s)
	}
	fmt.Fprintln(r.stderr)
	fmt.Fprintln(r.stderr, `Run "ainfra --help" to see all commands.`)
}

// closest returns the registered command name nearest to name by edit
// distance, or "" if none is within distance 2.
func (r *Registry) closest(name string) string {
	best, bestDist := "", 3
	for _, cmd := range r.commands {
		if d := levenshtein(name, cmd.Name); d < bestDist {
			best, bestDist = cmd.Name, d
		}
	}
	return best
}

// levenshtein returns the edit distance between a and b.
func levenshtein(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		curr := make([]int, len(b)+1)
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min3(prev[j]+1, curr[j-1]+1, prev[j-1]+cost)
		}
		prev = curr
	}
	return prev[len(b)]
}

func min3(a, b, c int) int {
	m := a
	if b < m {
		m = b
	}
	if c < m {
		m = c
	}
	return m
}

// runHelp implements the `ainfra help [command]` form. Defined here; wired by
// Dispatch in command.go (Task 7).
func (r *Registry) runHelp(args []string) int {
	if len(args) == 0 {
		r.printOverview()
		return 0
	}
	cmd := r.lookup(args[0])
	if cmd == nil {
		r.printUnknown(args[0])
		return 2
	}
	r.printCommandHelp(cmd)
	return 0
}
