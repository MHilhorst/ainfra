package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/MHilhorst/ainfra/internal/cli"
	"github.com/MHilhorst/ainfra/internal/lockfile"
	"github.com/MHilhorst/ainfra/internal/manifest"
	"github.com/MHilhorst/ainfra/internal/ui"
)

// listEntry is one row of `ainfra list` output, JSON-stable.
type listEntry struct {
	Channel     string `json:"channel"`
	ID          string `json:"id"`
	Version     string `json:"version,omitempty"`
	Layer       string `json:"layer"`
	ToolsetHash string `json:"toolsetHash"`
	// ShadowedBy names the layer of the entry that overrides this one when
	// multiple layers declare the same (channel, id). Empty for active rows.
	ShadowedBy string `json:"shadowedBy,omitempty"`
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
	// Annotate cross-layer collisions: when the same (channel, id) is
	// declared in more than one manifest layer, the lockfile only carries
	// the winning entry. Surface the loser as a shadowed row so the
	// override is visible at a glance.
	if shadowed := collectShadowedFromManifest(ctx.Dir, entries, channelFilter); len(shadowed) > 0 {
		entries = append(entries, shadowed...)
		sort.Slice(entries, func(i, j int) bool {
			if entries[i].Channel != entries[j].Channel {
				return entries[i].Channel < entries[j].Channel
			}
			return entries[i].ID < entries[j].ID
		})
	}

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
		toolset := ""
		if e.Channel == "mcpServers" {
			if e.ToolsetHash != "" {
				toolset = shortToolsetHash(e.ToolsetHash)
			} else {
				toolset = "unverified"
			}
		}
		suffix := toolset
		if e.ShadowedBy != "" {
			tag := fmt.Sprintf("(shadowed by %s)", e.ShadowedBy)
			if suffix != "" {
				suffix = suffix + " " + tag
			} else {
				suffix = tag
			}
		}
		fmt.Fprintf(ctx.Stdout, "  %-20s %-30s %-10s %-10s %s\n", e.Channel, e.ID, v, e.Layer, suffix)
	}
	return 0
}

// shortToolsetHash trims "sha256:" and truncates to 12 hex characters, suffixed
// with "..." for display in the list output.
func shortToolsetHash(h string) string {
	if i := strings.IndexByte(h, ':'); i >= 0 {
		h = h[i+1:]
	}
	if len(h) > 12 {
		return h[:12] + "..."
	}
	return h
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
				out = append(out, listEntry{Channel: ch, ID: id, Version: entry.Version, Layer: layer, ToolsetHash: entry.ToolsetHash})
			}
		}
	}
	walk(committed, "")
	walk(personal, "personal")

	annotateShadowed(out)

	sort.Slice(out, func(i, j int) bool {
		if out[i].Channel != out[j].Channel {
			return out[i].Channel < out[j].Channel
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// collectShadowedFromManifest loads manifest layers and, for each entry
// already in `existing`, returns synthetic listEntry rows for any other
// layer that also declares the same (channel, id). Only commands, hooks,
// skills, rules, and mcpServers are checked — the channels where layered
// overrides actually happen in practice.
func collectShadowedFromManifest(dir string, existing []listEntry, channelFilter string) []listEntry {
	layers, err := manifest.LoadLayers(dir)
	if err != nil || len(layers) < 2 {
		return nil
	}
	priority := map[string]int{"team": 0, "repo": 1, "personal": 2}
	type ids struct{ commands, hooks, skills, rules, mcps map[string]bool }
	byLayer := map[manifest.Layer]ids{}
	for layer, m := range layers {
		i := ids{
			commands: map[string]bool{},
			hooks:    map[string]bool{},
			skills:   map[string]bool{},
			rules:    map[string]bool{},
			mcps:     map[string]bool{},
		}
		for k := range m.Commands {
			i.commands[k] = true
		}
		for k := range m.Hooks {
			i.hooks[k] = true
		}
		for k := range m.Skills {
			i.skills[k] = true
		}
		for k := range m.Rules {
			i.rules[k] = true
		}
		for k := range m.MCPServers {
			i.mcps[k] = true
		}
		byLayer[layer] = i
	}
	pickChannel := func(channel string, i ids) map[string]bool {
		switch channel {
		case "commands":
			return i.commands
		case "hooks":
			return i.hooks
		case "skills":
			return i.skills
		case "rules":
			return i.rules
		case "mcpServers":
			return i.mcps
		}
		return nil
	}

	var out []listEntry
	for idx := range existing {
		e := &existing[idx]
		// Discover every layer that declares this (channel, id), then pick
		// the highest-priority one as the "active" layer. Lockfile entries
		// record the last-written layer; correct the active row to the
		// real install winner and emit shadow rows for the others.
		var declared []manifest.Layer
		for layer, i := range byLayer {
			ids := pickChannel(e.Channel, i)
			if ids != nil && ids[e.ID] {
				declared = append(declared, layer)
			}
		}
		if len(declared) < 2 {
			continue
		}
		sort.Slice(declared, func(i, j int) bool {
			return priority[string(declared[i])] < priority[string(declared[j])]
		})
		winner := declared[0]
		e.Layer = string(winner)
		for _, layer := range declared[1:] {
			if channelFilter != "" && e.Channel != channelFilter {
				continue
			}
			out = append(out, listEntry{
				Channel:    e.Channel,
				ID:         e.ID,
				Layer:      string(layer),
				ShadowedBy: string(winner),
			})
		}
	}
	return out
}

// annotateShadowed walks the collected entries, groups by (channel, id), and
// marks every row that shares a key with a higher-precedence row. Precedence
// order: team < repo < personal — earlier layers win during apply, so the
// later ones are the shadowed losers.
func annotateShadowed(entries []listEntry) {
	priority := map[string]int{"team": 0, "repo": 1, "personal": 2}
	type winner struct {
		idx   int
		layer string
		rank  int
	}
	winners := map[string]winner{}
	for i, e := range entries {
		key := e.Channel + "/" + e.ID
		rank, ok := priority[e.Layer]
		if !ok {
			rank = 99
		}
		w, seen := winners[key]
		if !seen || rank < w.rank {
			winners[key] = winner{idx: i, layer: e.Layer, rank: rank}
		}
	}
	for i := range entries {
		key := entries[i].Channel + "/" + entries[i].ID
		w := winners[key]
		if w.idx != i {
			entries[i].ShadowedBy = w.layer
		}
	}
}
