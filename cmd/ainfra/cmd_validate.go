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
// or writing a lockfile. Deprecated in favor of `install --dry-run`, which
// performs validation as a side effect of resolving; kept as a hidden alias
// because it has the unique property of running without a lockfile.
func newValidateCommand() *cli.Command {
	var printSchema bool
	return &cli.Command{
		Name:          "validate",
		Summary:       "Check the manifest for errors without resolving it",
		UsageLine:     "ainfra validate [--print-schema]",
		Example:       "ainfra validate",
		Hidden:        true,
		DeprecatedFor: "install --dry-run",
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
	// LoadLayers now returns successfully with no repo layer when there's
	// no ainfra.yaml (user-scope mode). validate's contract still expects a
	// repo manifest — there's nothing repo-shaped to check otherwise.
	if _, ok := layers[manifest.LayerRepo]; !ok {
		ui.RenderError(ctx.Stderr, errColor, fmt.Errorf("ainfra.yaml not found in %s", ctx.Dir))
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
