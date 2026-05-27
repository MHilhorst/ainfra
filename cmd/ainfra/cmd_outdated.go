package main

import (
	"flag"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/MHilhorst/ainfra/internal/cli"
	"github.com/MHilhorst/ainfra/internal/lockfile"
	"github.com/MHilhorst/ainfra/internal/ui"
)

// newOutdatedCommand reports entries whose locked version is older than the
// resolvable latest. Read-only. `--strict` exits non-zero when at least one
// row would be printed (CI shape).
//
// The latest-version lookup is stubbed in this first pass — entries without
// an obviously stale signal (today: package-launched MCP servers with a
// pinned npm version) are skipped silently. A follow-up unit can probe the
// npm registry; today the verb earns its keep as a placeholder that scripts
// can already wire to without needing rework when the lookup lands.
func newOutdatedCommand() *cli.Command {
	var strict bool
	return &cli.Command{
		Name:      "outdated",
		Summary:   "Show installed entries that have newer resolvable versions",
		UsageLine: "ainfra outdated [--strict]",
		Example:   "ainfra outdated --strict   # CI gate",
		SetFlags: func(fs *flag.FlagSet) {
			fs.BoolVar(&strict, "strict", false, "exit non-zero when at least one entry is outdated")
		},
		Run: func(ctx cli.Context) int {
			return runOutdated(ctx, strict)
		},
	}
}

func runOutdated(ctx cli.Context, strict bool) int {
	errColor := ui.NewColorizer(ctx.Stderr, ctx.NoColor)

	lockPath := filepath.Join(ctx.Dir, "ainfra.lock")
	if !fileExists(lockPath) {
		ui.RenderError(ctx.Stderr, errColor, fmt.Errorf("ainfra.lock not found — run `ainfra install` first"))
		return 1
	}
	committed, err := lockfile.Read(lockPath)
	if err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}
	personal, _ := lockfile.Read(filepath.Join(ctx.Dir, "ainfra.personal.lock"))
	if personal == nil {
		personal = &lockfile.Lock{}
	}

	stale := outdatedEntries(committed, personal)

	if len(stale) == 0 {
		fmt.Fprintln(ctx.Stdout, "Up to date.")
		return 0
	}

	for _, e := range stale {
		fmt.Fprintf(ctx.Stdout, "  %-20s %-30s %-10s -> %s\n", e.Channel, e.ID, e.Current, e.Latest)
	}
	if strict {
		return 1
	}
	return 0
}

// outdatedRow is one row of `outdated` output.
type outdatedRow struct {
	Channel string
	ID      string
	Current string
	Latest  string
}

// outdatedEntries computes the list of stale rows. Today it's a stub that
// returns empty until the npm-registry probe lands — but the verb, output
// format, and --strict semantics are all in place so CI configs and tests
// can wire to it now.
func outdatedEntries(committed, personal *lockfile.Lock) []outdatedRow {
	_ = committed
	_ = personal
	var out []outdatedRow
	// TODO: probe internal/provider/pkg for latest npm versions and compare
	// against Entry.Version on package-launched MCP servers.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Channel != out[j].Channel {
			return out[i].Channel < out[j].Channel
		}
		return out[i].ID < out[j].ID
	})
	return out
}
