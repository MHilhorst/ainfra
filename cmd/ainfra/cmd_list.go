package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/MHilhorst/ainfra/internal/cli"
	"github.com/MHilhorst/ainfra/internal/lockfile"
	"github.com/MHilhorst/ainfra/internal/ui"
)

// listEntry is one row of `ainfra list` output, JSON-stable.
type listEntry struct {
	Channel string `json:"channel"`
	ID      string `json:"id"`
	Version string `json:"version,omitempty"`
	Layer   string `json:"layer"`
}

// newListCommand reads the merged lockfile and prints one row per installed
// entry. `--channel` filters; `--json` emits JSON Lines for scripting.
func newListCommand() *cli.Command {
	var channel string
	var asJSON bool
	return &cli.Command{
		Name:      "list",
		Summary:   "List installed entries (mcpServers, hooks, commands, ...)",
		UsageLine: "ainfra list [--channel <name>] [--json]",
		Example:   "ainfra list --channel mcpServers",
		SetFlags: func(fs *flag.FlagSet) {
			fs.StringVar(&channel, "channel", "", "filter to one channel (e.g. mcpServers, hooks)")
			fs.BoolVar(&asJSON, "json", false, "emit JSON Lines instead of a table")
		},
		Run: func(ctx cli.Context) int {
			return runList(ctx, channel, asJSON)
		},
	}
}

func runList(ctx cli.Context, channelFilter string, asJSON bool) int {
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
	personal, err := lockfile.Read(filepath.Join(ctx.Dir, "ainfra.personal.lock"))
	if err != nil {
		personal = &lockfile.Lock{}
	}

	entries := collectListEntries(committed, personal, channelFilter)

	if asJSON {
		enc := json.NewEncoder(ctx.Stdout)
		for _, e := range entries {
			if err := enc.Encode(e); err != nil {
				return 1
			}
		}
		return 0
	}

	if len(entries) == 0 {
		fmt.Fprintln(ctx.Stdout, "No entries.")
		return 0
	}

	for _, e := range entries {
		v := e.Version
		if v == "" {
			v = "—"
		}
		fmt.Fprintf(ctx.Stdout, "  %-20s %-30s %-10s %s\n", e.Channel, e.ID, v, e.Layer)
	}
	return 0
}

// collectListEntries walks both lockfiles, applies the channel filter, and
// returns a stable-sorted slice.
func collectListEntries(committed, personal *lockfile.Lock, channelFilter string) []listEntry {
	var out []listEntry
	walk := func(lock *lockfile.Lock, layerOverride string) {
		channels := map[string]map[string]lockfile.Entry{
			"mcpServers":         lock.Entries.MCPServers,
			"backgroundServices": lock.Entries.BackgroundServices,
			"hooks":              lock.Entries.Hooks,
			"commands":           lock.Entries.Commands,
			"cliTools":           lock.Entries.CLITools,
			"skills":             lock.Entries.Skills,
			"marketplaces":       lock.Entries.Marketplaces,
			"plugins":            lock.Entries.Plugins,
			"rules":              lock.Entries.Rules,
			"tools":              lock.Entries.Tools,
		}
		for ch, m := range channels {
			if channelFilter != "" && ch != channelFilter {
				continue
			}
			for id, entry := range m {
				layer := entry.Layer
				if layerOverride != "" {
					layer = layerOverride
				}
				out = append(out, listEntry{Channel: ch, ID: id, Version: entry.Version, Layer: layer})
			}
		}
	}
	walk(committed, "")
	walk(personal, "personal")

	sort.Slice(out, func(i, j int) bool {
		if out[i].Channel != out[j].Channel {
			return out[i].Channel < out[j].Channel
		}
		return out[i].ID < out[j].ID
	})
	return out
}
