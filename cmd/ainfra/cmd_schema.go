package main

import (
	"encoding/json"
	"fmt"

	"github.com/MHilhorst/ainfra/internal/cli"
	"github.com/MHilhorst/ainfra/internal/schema"
	"github.com/MHilhorst/ainfra/internal/ui"
)

// newSchemaCommand prints the JSON Schema for ainfra.yaml. Redirect it to a
// file and point an editor's YAML language server at it for autocomplete and
// inline validation while authoring a manifest.
func newSchemaCommand() *cli.Command {
	return &cli.Command{
		Name:      "schema",
		Summary:   "Print the JSON Schema for ainfra.yaml",
		UsageLine: "ainfra schema",
		Example:   "ainfra schema > ainfra.schema.json",
		Hidden:    true,
		Run: func(ctx cli.Context) int {
			out, err := json.MarshalIndent(schema.Generate(), "", "  ")
			if err != nil {
				ui.RenderError(ctx.Stderr, ui.NewColorizer(ctx.Stderr, ctx.NoColor), err)
				return 1
			}
			fmt.Fprintln(ctx.Stdout, string(out))
			return 0
		},
	}
}
