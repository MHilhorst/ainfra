package main

import (
	"encoding/json"
	"flag"
	"fmt"

	"github.com/MHilhorst/ainfra/internal/cli"
	"github.com/MHilhorst/ainfra/internal/manifest"
	"github.com/MHilhorst/ainfra/internal/schema"
	"github.com/MHilhorst/ainfra/internal/ui"
)

// newValidateCommand runs the manifest's static checks without resolving it
// or writing a lockfile. With --print-schema it prints the JSON Schema for
// ainfra.yaml instead — folds the (hidden) `ainfra schema` verb into a flag
// on a top-level verb so editors can be pointed at `ainfra validate
// --print-schema` once.
func newValidateCommand() *cli.Command {
	var printSchema bool
	return &cli.Command{
		Name:      "validate",
		Summary:   "Check the manifest for errors without resolving it",
		UsageLine: "ainfra validate [--print-schema]",
		Example:   "ainfra validate",
		SetFlags: func(fs *flag.FlagSet) {
			fs.BoolVar(&printSchema, "print-schema", false, "print the JSON Schema for ainfra.yaml and exit")
		},
		Run: func(ctx cli.Context) int {
			if printSchema {
				return runPrintSchema(ctx)
			}
			return runValidate(ctx)
		},
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

func runPrintSchema(ctx cli.Context) int {
	out, err := json.MarshalIndent(schema.Generate(), "", "  ")
	if err != nil {
		ui.RenderError(ctx.Stderr, ui.NewColorizer(ctx.Stderr, ctx.NoColor), err)
		return 1
	}
	fmt.Fprintln(ctx.Stdout, string(out))
	return 0
}
