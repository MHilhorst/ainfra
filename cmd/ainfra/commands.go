package main

import (
	"flag"
	"fmt"

	"github.com/MHilhorst/ainfra/internal/cli"
	"github.com/MHilhorst/ainfra/internal/ui"
	"github.com/MHilhorst/ainfra/internal/version"
)

// newVersionCommand prints the build version, optionally as JSON.
func newVersionCommand() *cli.Command {
	var asJSON bool
	return &cli.Command{
		Name:      "version",
		Summary:   "Print the ainfra version",
		UsageLine: "ainfra version [--json]",
		Example:   "ainfra version --json",
		SetFlags:  func(fs *flag.FlagSet) { fs.BoolVar(&asJSON, "json", false, "print as JSON") },
		Run: func(ctx cli.Context) int {
			if asJSON {
				fmt.Fprintf(ctx.Stdout, "{\"version\":%q}\n", version.Version)
			} else {
				fmt.Fprintf(ctx.Stdout, "ainfra %s\n", version.Version)
			}
			return 0
		},
	}
}

// newPendingCommand builds a command whose real behavior depends on the
// channel provider layer (the next build phase). It prints a clear message
// and exits 1, but still gets real --help via the registry.
func newPendingCommand(name, summary, describes string) *cli.Command {
	return &cli.Command{
		Name:      name,
		Summary:   summary,
		UsageLine: "ainfra " + name,
		Run: func(ctx cli.Context) int {
			c := ui.NewColorizer(ctx.Stderr, ctx.NoColor)
			fmt.Fprintln(ctx.Stderr, c.Bold("ainfra "+name), "is not available yet.")
			fmt.Fprintln(ctx.Stderr)
			fmt.Fprintln(ctx.Stderr, "  "+describes)
			fmt.Fprintln(ctx.Stderr, "  "+c.Dim("It depends on the channel provider layer — the next build phase."))
			return 1
		},
	}
}

func newPlanCommand() *cli.Command {
	return newPendingCommand("plan",
		"Show the diff between desired and observed state",
		"plan will resolve the manifest and show the +/~/- changes ainfra would make.")
}

func newApplyCommand() *cli.Command {
	return newPendingCommand("apply",
		"Reconcile the environment to match the manifest",
		"apply will show the plan, ask for confirmation, then reconcile each channel.")
}

func newCheckCommand() *cli.Command {
	return newPendingCommand("check",
		"Verify the environment matches the lockfile; report drift",
		"check will compare the observed environment against ainfra.lock and report drift.")
}
