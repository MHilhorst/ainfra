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
	for id, e := range l.Entries.Marketplaces {
		out["marketplaces"] = append(out["marketplaces"], entryToResource(id, "marketplaces", e))
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

// PartitionByScope splits a rendered resources map into a repo-scope subset
// and a user-scope subset. An entry routes to user-scope when its Layer is
// "personal" — those entries materialize to ~/.claude/ on the user pass.
// Everything else routes to repo-scope. mcpServers is forced into repo-scope
// regardless of layer because Claude Code reads user-level MCP servers from
// ~/.claude.json (a different file with a different format than .mcp.json)
// which is out of scope for this slice; a warning surfaces in cmd_install.
func PartitionByScope(rendered map[string][]Resource) (repo, user map[string][]Resource) {
	repo = map[string][]Resource{}
	user = map[string][]Resource{}
	for channel, resources := range rendered {
		for _, r := range resources {
			if r.Layer == "personal" && channel != "mcpServers" {
				user[channel] = append(user[channel], r)
			} else {
				repo[channel] = append(repo[channel], r)
			}
		}
	}
	return repo, user
}

// HasUserScopeMCP reports whether any rendered mcpServers entry came from
// the personal layer — callers warn the user that MCP user-scope is not yet
// supported and the entry will fall back to repo-scope behavior.
func HasUserScopeMCP(rendered map[string][]Resource) bool {
	for _, r := range rendered["mcpServers"] {
		if r.Layer == "personal" {
			return true
		}
	}
	return false
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
	"marketplaces":       "marketplace",
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
		{"marketplaces", l.Entries.Marketplaces},
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
