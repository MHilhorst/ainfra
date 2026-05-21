package main

import (
	"flag"
	"fmt"
	"path/filepath"

	"github.com/MHilhorst/ainfra/internal/cli"
	"github.com/MHilhorst/ainfra/internal/lockfile"
	"github.com/MHilhorst/ainfra/internal/provider"
	"github.com/MHilhorst/ainfra/internal/resolve"
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

// mergeLocks returns a new lock that is the union of committed and personal
// entries. Personal entries take precedence over committed entries when both
// define the same key in the same channel.
func mergeLocks(committed, personal *lockfile.Lock) *lockfile.Lock {
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
		Version: committed.Version,
		Entries: lockfile.Entries{
			MCPServers:         merge(committed.Entries.MCPServers, personal.Entries.MCPServers),
			BackgroundServices: merge(committed.Entries.BackgroundServices, personal.Entries.BackgroundServices),
			Hooks:              merge(committed.Entries.Hooks, personal.Entries.Hooks),
			Commands:           merge(committed.Entries.Commands, personal.Entries.Commands),
			CLITools:           merge(committed.Entries.CLITools, personal.Entries.CLITools),
			Skills:             merge(committed.Entries.Skills, personal.Entries.Skills),
			Plugins:            merge(committed.Entries.Plugins, personal.Entries.Plugins),
			Rules:              merge(committed.Entries.Rules, personal.Entries.Rules),
			Tools:              merge(committed.Entries.Tools, personal.Entries.Tools),
		},
	}
}

func fileExists(path string) bool {
	fs := provider.OSFilesystem{}
	_, err := fs.ReadFile(path)
	return err == nil
}

// warnIfStale prints a warning when the manifest has changed since the last
// lock run, indicating the lock may be out of date.
func warnIfStale(ctx cli.Context, dir string, committed *lockfile.Lock) {
	if committed.ManifestHash == "" {
		return
	}
	current, err := resolve.CurrentManifestHash(dir)
	if err != nil {
		return
	}
	if current != committed.ManifestHash {
		c := ui.NewColorizer(ctx.Stderr, ctx.NoColor)
		fmt.Fprintln(ctx.Stderr, c.Yellow("warning: manifest has changed since last lock run — run `ainfra lock` to update"))
	}
}

func newPlanCommand() *cli.Command {
	return &cli.Command{
		Name:      "plan",
		Summary:   "Show the diff between desired and observed state",
		UsageLine: "ainfra plan",
		Example:   "ainfra plan",
		Run:       runPlan,
	}
}

func runPlan(ctx cli.Context) int {
	dir := ctx.Dir
	errColor := ui.NewColorizer(ctx.Stderr, ctx.NoColor)

	lockPath := filepath.Join(dir, "ainfra.lock")
	if !fileExists(lockPath) {
		ui.RenderError(ctx.Stderr, errColor, fmt.Errorf("ainfra.lock not found — run `ainfra lock` first"))
		return 1
	}

	committed, err := lockfile.Read(lockPath)
	if err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}
	personal, err := lockfile.Read(filepath.Join(dir, "ainfra.personal.lock"))
	if err != nil {
		personal = &lockfile.Lock{}
	}
	merged := mergeLocks(committed, personal)
	warnIfStale(ctx, dir, committed)

	orch := provider.NewOrchestrator(dir, buildEnv(dir), allProviders())
	plans, err := orch.PlanAll(merged)
	if err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}

	c := ui.NewColorizer(ctx.Stdout, ctx.NoColor)
	ui.RenderPlan(ctx.Stdout, c, plans)
	return 0
}

// newPendingCommand builds a command whose real behavior is not yet
// implemented. It prints a clear message and exits 1.
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
