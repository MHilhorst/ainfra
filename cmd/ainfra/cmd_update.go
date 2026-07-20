package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"sort"

	"github.com/MHilhorst/ainfra/internal/cli"
	"github.com/MHilhorst/ainfra/internal/provider"
	"github.com/MHilhorst/ainfra/internal/resolve"
	"github.com/MHilhorst/ainfra/internal/ui"
)

// newUpdateCommand wires `ainfra update [<channel> <id>]`.
//
// Today the verb re-resolves the manifest and reinstalls. The npm-registry
// probe that would auto-bump version: pins is deferred; until it lands the
// user bumps version: by hand and `update` materializes the change. The verb
// is shipped now so scripts and CI can wire to it without rework later.
func newUpdateCommand() *cli.Command {
	var noInstall bool
	return &cli.Command{
		Name:      "update",
		Summary:   "Re-resolve ainfra.yaml into a fresh lockfile and install (use after editing ainfra.yaml)",
		UsageLine: "ainfra update [--no-install] [<channel> <id>]",
		Example:   "ainfra update          # re-resolve all\n  ainfra update mcp github  # one entry",
		SetFlags: func(fs *flag.FlagSet) {
			fs.BoolVar(&noInstall, "no-install", false, "re-lock only; skip reconcile")
		},
		Run: func(ctx cli.Context) int {
			return runUpdate(ctx, noInstall)
		},
	}
}

// renderToolsetWarnings reports every MCP server the lock run could not probe.
// Their pinned tool lists are carried forward from the previous lock, so the
// lockfile stays intact — but staying silent about it once let a re-lock run
// with the DB tunnels down look identical to a clean one.
func renderToolsetWarnings(w io.Writer, c ui.Colorizer, result *resolve.RunLockResult) {
	if result == nil || len(result.ToolsetWarnings) == 0 {
		return
	}
	warnings := append([]resolve.ToolsetWarning(nil), result.ToolsetWarnings...)
	sort.Slice(warnings, func(i, j int) bool { return warnings[i].ServerID < warnings[j].ServerID })
	fmt.Fprintln(w, c.Yellow(fmt.Sprintf(
		"warning: could not probe %d MCP server(s); their pinned tool lists were kept from the previous lock:",
		len(warnings))))
	for _, wn := range warnings {
		fmt.Fprintf(w, "  %-38s %s\n", wn.ServerID, wn.Reason)
	}
	fmt.Fprintln(w, c.Dim("  A server is usually unreachable because its tunnel or VPN is down. Re-run once it is up to refresh the pins."))
}

func runUpdate(ctx cli.Context, noInstall bool) int {
	errColor := ui.NewColorizer(ctx.Stderr, ctx.NoColor)

	// Per-entry form takes <channel> <id>. Validate the args shape but the
	// behavior is identical today — both forms re-resolve the manifest.
	// Future work: when a registry probe lands, the per-entry form bumps just
	// that entry while bare bumps all.
	if len(ctx.Args) == 1 {
		ui.RenderError(ctx.Stderr, errColor, errors.New("usage: ainfra update [<channel> <id>]"))
		return 2
	}
	if len(ctx.Args) >= 2 {
		rawChannel := ctx.Args[0]
		if _, ok := channelAlias[rawChannel]; !ok {
			ui.RenderError(ctx.Stderr, errColor, fmt.Errorf("unknown channel %q (try one of: mcp, hook, command, skill, cliTool, plugin, marketplace, rule, tool)", rawChannel))
			return 1
		}
	}

	result, err := resolve.RunLockWithResult(ctx.Dir, provider.ExecRunner{})
	if err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}
	fmt.Fprintln(ctx.Stdout, "Re-resolved lockfile from ainfra.yaml.")
	renderToolsetWarnings(ctx.Stderr, errColor, result)

	if noInstall {
		c := ui.NewColorizer(ctx.Stdout, ctx.NoColor)
		ui.Next(ctx.Stdout, c, "run `ainfra install` to apply the updated lockfile.")
		return 0
	}
	return runApply(ctx, true /*yes*/, false /*dryRun*/, false /*noInstall*/, false /*strict*/, false /*prune*/, "")
}
