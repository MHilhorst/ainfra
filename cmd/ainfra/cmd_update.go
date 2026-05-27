package main

import (
	"errors"
	"flag"
	"fmt"

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
		Summary:   "Bump locked versions and reconcile (or re-resolve if no entry named)",
		UsageLine: "ainfra update [<channel> <id>] [--no-install]",
		Example:   "ainfra update          # re-resolve all\n  ainfra update mcp github  # one entry",
		SetFlags: func(fs *flag.FlagSet) {
			fs.BoolVar(&noInstall, "no-install", false, "re-lock only; skip reconcile")
		},
		Run: func(ctx cli.Context) int {
			return runUpdate(ctx, noInstall)
		},
	}
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
			ui.RenderError(ctx.Stderr, errColor, fmt.Errorf("unknown channel %q", rawChannel))
			return 1
		}
	}

	if err := resolve.RunLock(ctx.Dir, provider.ExecRunner{}); err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}
	fmt.Fprintln(ctx.Stdout, "Re-resolved lockfile.")

	if noInstall {
		return 0
	}
	return runApply(ctx, true /*yes*/, false /*dryRun*/, false /*noInstall*/, false /*strict*/)
}
