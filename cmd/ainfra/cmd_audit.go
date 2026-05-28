package main

import (
	"flag"

	"github.com/MHilhorst/ainfra/internal/audit"
	"github.com/MHilhorst/ainfra/internal/cli"
)

// newAuditCommand is the read-only inventory of Claude Code config across
// the Global (~/.claude) and Project (.claude/) filesystem layers. It pairs
// `ainfra list` (which shows ainfra-managed entries only) with a broader,
// disk-first view that also surfaces unmanaged entries — the adoption hook.
func newAuditCommand() *cli.Command {
	var asJSON bool
	return &cli.Command{
		Name:      "audit",
		Summary:   "Inventory Claude config across global + project layers",
		UsageLine: "ainfra audit [--json]",
		Example:   "ainfra audit",
		SetFlags: func(fs *flag.FlagSet) {
			fs.BoolVar(&asJSON, "json", false, "emit JSON Lines instead of a table")
		},
		Run: func(ctx cli.Context) int {
			return audit.Run(ctx, audit.Options{JSON: asJSON})
		},
	}
}
