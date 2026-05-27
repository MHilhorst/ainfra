package main

import (
	"errors"
	"flag"
	"fmt"
	"path/filepath"

	"github.com/MHilhorst/ainfra/internal/cli"
	"github.com/MHilhorst/ainfra/internal/manifest/writer"
	"github.com/MHilhorst/ainfra/internal/provider"
	"github.com/MHilhorst/ainfra/internal/resolve"
	"github.com/MHilhorst/ainfra/internal/ui"
)

// newRemoveCommand wires `ainfra remove <channel> <id>`.
func newRemoveCommand() *cli.Command {
	var personal, noInstall bool
	return &cli.Command{
		Name:      "remove",
		Summary:   "Remove an entry from ainfra.yaml and reconcile",
		UsageLine: "ainfra remove <channel> <id> [--personal] [--no-install]",
		Example:   "ainfra remove mcp github\n  ainfra remove --personal mcp local-fs",
		SetFlags: func(fs *flag.FlagSet) {
			fs.BoolVar(&personal, "personal", false, "remove from ainfra.personal.yaml instead of ainfra.yaml")
			fs.BoolVar(&noInstall, "no-install", false, "remove the manifest entry and re-lock, but skip reconcile")
		},
		Run: func(ctx cli.Context) int {
			return runRemove(ctx, personal, noInstall)
		},
	}
}

func runRemove(ctx cli.Context, personal, noInstall bool) int {
	errColor := ui.NewColorizer(ctx.Stderr, ctx.NoColor)
	if len(ctx.Args) < 2 {
		ui.RenderError(ctx.Stderr, errColor, errors.New("usage: ainfra remove <channel> <id>"))
		return 2
	}
	rawChannel := ctx.Args[0]
	id := ctx.Args[1]

	canonical, ok := channelAlias[rawChannel]
	if !ok {
		ui.RenderError(ctx.Stderr, errColor, fmt.Errorf("unknown channel %q", rawChannel))
		return 1
	}

	manifestFile := "ainfra.yaml"
	if personal {
		manifestFile = "ainfra.personal.yaml"
	}
	manifestPath := filepath.Join(ctx.Dir, manifestFile)
	if !fileExists(manifestPath) {
		ui.RenderError(ctx.Stderr, errColor, fmt.Errorf("%s not found", manifestFile))
		return 1
	}

	if err := writer.RemoveEntry(manifestPath, canonical, id); err != nil {
		if errors.Is(err, writer.ErrEntryNotFound) {
			ui.RenderError(ctx.Stderr, errColor, fmt.Errorf("%s.%s not found in %s", canonical, id, manifestFile))
			return 1
		}
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}

	fmt.Fprintf(ctx.Stdout, "Removed %s.%s from %s\n", canonical, id, manifestFile)

	if err := resolve.RunLock(ctx.Dir, provider.ExecRunner{}); err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}

	if noInstall {
		fmt.Fprintln(ctx.Stdout, "Skipping install (--no-install).")
		return 0
	}

	return runApply(ctx, true /*yes*/, false /*dryRun*/, false /*noInstall*/, false /*strict*/)
}
