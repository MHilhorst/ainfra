package provider

import (
	"github.com/MHilhorst/ainfra/internal/graph"
	"github.com/MHilhorst/ainfra/internal/lockfile"
)

// ResourcesByChannel converts a lockfile.Lock into a map of channel name to
// []Resource slices, carrying ID, Layer, ContentHash, and Requires per entry.
func ResourcesByChannel(l *lockfile.Lock) map[string][]Resource {
	out := map[string][]Resource{}

	for id, e := range l.Entries.MCPServers {
		out["mcpServers"] = append(out["mcpServers"], entryToResource(id, "mcpServers", e))
	}
	for id, e := range l.Entries.BackgroundServices {
		out["backgroundServices"] = append(out["backgroundServices"], entryToResource(id, "backgroundServices", e))
	}
	for id, e := range l.Entries.Hooks {
		out["hooks"] = append(out["hooks"], entryToResource(id, "hooks", e))
	}
	for id, e := range l.Entries.Commands {
		out["commands"] = append(out["commands"], entryToResource(id, "commands", e))
	}
	for id, e := range l.Entries.CLITools {
		out["cliTools"] = append(out["cliTools"], entryToResource(id, "cliTools", e))
	}
	for id, e := range l.Entries.Skills {
		out["skills"] = append(out["skills"], entryToResource(id, "skills", e))
	}
	for id, e := range l.Entries.Plugins {
		out["plugins"] = append(out["plugins"], entryToResource(id, "plugins", e))
	}
	for id, e := range l.Entries.Rules {
		out["rules"] = append(out["rules"], entryToResource(id, "rules", e))
	}
	for id, e := range l.Entries.Tools {
		out["tools"] = append(out["tools"], entryToResource(id, "tools", e))
	}

	return out
}

func entryToResource(id, channel string, e lockfile.Entry) Resource {
	return Resource{
		ID:          id,
		Channel:     channel,
		Layer:       e.Layer,
		ContentHash: e.ContentHash,
		Requires:    e.Requires,
	}
}

// channelPrefix maps a channel name to its node-ref prefix in the dependency graph.
var channelPrefix = map[string]string{
	"mcpServers":         "mcp",
	"backgroundServices": "svc",
	"hooks":              "hook",
	"commands":           "cmd",
	"cliTools":           "cli",
	"skills":             "skill",
	"plugins":            "plugin",
	"rules":              "rule",
	"tools":              "tools",
}

// ApplyOrder returns the topological order of all lock entries so that a
// required entry precedes the entry that needs it. Node refs use the same
// prefix scheme as the resolve pipeline (e.g. "mcp:db", "svc:tunnel").
// graph.TopoSort returns dependencies-before-dependents, which is the required
// order, so no reversal is needed.
func ApplyOrder(l *lockfile.Lock) ([]string, error) {
	g := graph.New()

	type channelEntries struct {
		channel string
		entries map[string]lockfile.Entry
	}
	channels := []channelEntries{
		{"mcpServers", l.Entries.MCPServers},
		{"backgroundServices", l.Entries.BackgroundServices},
		{"hooks", l.Entries.Hooks},
		{"commands", l.Entries.Commands},
		{"cliTools", l.Entries.CLITools},
		{"skills", l.Entries.Skills},
		{"plugins", l.Entries.Plugins},
		{"rules", l.Entries.Rules},
		{"tools", l.Entries.Tools},
	}

	for _, ch := range channels {
		prefix := channelPrefix[ch.channel]
		for id := range ch.entries {
			node := prefix + ":" + id
			g.AddNode(node)
		}
	}

	for _, ch := range channels {
		prefix := channelPrefix[ch.channel]
		for id, e := range ch.entries {
			node := prefix + ":" + id
			for _, ref := range e.Requires {
				g.AddNode(ref)
				g.AddEdge(node, ref)
			}
		}
	}

	return g.TopoSort()
}
