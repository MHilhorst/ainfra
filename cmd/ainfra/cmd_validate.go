package main

import (
	"fmt"

	"github.com/MHilhorst/ainfra/internal/cli"
	"github.com/MHilhorst/ainfra/internal/manifest"
	"github.com/MHilhorst/ainfra/internal/ui"
)

// newValidateCommand runs the manifest's static checks without resolving it
// or writing a lockfile.
func newValidateCommand() *cli.Command {
	return &cli.Command{
		Name:      "validate",
		Summary:   "Check the manifest for errors without resolving it",
		UsageLine: "ainfra validate",
		Example:   "ainfra validate",
		Run:       runValidate,
	}
}

func runValidate(ctx cli.Context) int {
	errColor := ui.NewColorizer(ctx.Stderr, ctx.NoColor)
	layers, err := manifest.LoadLayers(ctx.Dir)
	if err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}
	if err := manifest.ValidateAll(layers); err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}
	c := ui.NewColorizer(ctx.Stdout, ctx.NoColor)
	fmt.Fprintln(ctx.Stdout, c.Green("Configuration is valid."))
	return 0
}
