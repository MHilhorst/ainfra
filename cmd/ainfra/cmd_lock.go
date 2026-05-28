package main

import (
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"github.com/MHilhorst/ainfra/internal/cli"
	"github.com/MHilhorst/ainfra/internal/lockfile"
	"github.com/MHilhorst/ainfra/internal/manifest"
	"github.com/MHilhorst/ainfra/internal/provider"
	"github.com/MHilhorst/ainfra/internal/resolve"
	"github.com/MHilhorst/ainfra/internal/ui"
)

// newLockCommand resolves the manifest and writes ainfra.lock and
// ainfra.personal.lock.
func newLockCommand() *cli.Command {
	return &cli.Command{
		Name:      "lock",
		Summary:   "Resolve ainfra.yaml into ainfra.lock (pins exact versions; commit this file)",
		UsageLine: "ainfra lock",
		Example:   "ainfra lock",
		Hidden:    true,
		Run:       runLock,
	}
}

func runLock(ctx cli.Context) int {
	result, err := resolve.RunLockWithResult(ctx.Dir, provider.ExecRunner{})
	if err != nil {
		ui.RenderError(ctx.Stderr, ui.NewColorizer(ctx.Stderr, ctx.NoColor), err)
		return 1
	}
	c := ui.NewColorizer(ctx.Stdout, ctx.NoColor)
	committed, err := lockfile.Read(filepath.Join(ctx.Dir, "ainfra.lock"))
	if err != nil {
		committed = &lockfile.Lock{}
	}
	personal, err := lockfile.Read(filepath.Join(ctx.Dir, "ainfra.personal.lock"))
	if err != nil {
		personal = &lockfile.Lock{}
	}
	fmt.Fprintln(ctx.Stdout, "Resolved ainfra.yaml: "+lockSummary(committed, personal)+".")
	fmt.Fprintln(ctx.Stdout, "Wrote ainfra.lock (commit this) and ainfra.personal.lock (git-ignored).")
	renderMCPServerStatus(ctx.Stdout, committed, personal, result)
	ui.Next(ctx.Stdout, c, "run `ainfra install --dry-run` to preview changes, or `ainfra install` to apply.")
	if layers, err := manifest.LoadLayers(ctx.Dir); err == nil {
		for _, w := range cliToolInstallWarnings(layers) {
			fmt.Fprintln(ctx.Stderr, c.Yellow("warning: "+w))
		}
	}
	return 0
}

// renderMCPServerStatus prints a per-server toolset-verification report for
// every MCP server in the merged lockfile: verified servers list the
// short-form hash; unverified servers list the warning reason.
func renderMCPServerStatus(w io.Writer, committed, personal *lockfile.Lock, result *resolve.RunLockResult) {
	type row struct {
		id   string
		hash string
	}
	collect := func(l *lockfile.Lock) []row {
		var rows []row
		for id, e := range l.Entries.MCPServers {
			rows = append(rows, row{id: id, hash: e.ToolsetHash})
		}
		return rows
	}
	rows := append(collect(committed), collect(personal)...)
	if len(rows) == 0 {
		return
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].id < rows[j].id })
	warnings := map[string]resolve.ToolsetWarning{}
	if result != nil {
		for _, w := range result.ToolsetWarnings {
			warnings[w.ServerID] = w
		}
	}
	fmt.Fprintln(w, "MCP servers (tool list pinned so upstream changes surface as drift):")
	for _, r := range rows {
		if r.hash != "" {
			fmt.Fprintf(w, "  %-30s tool list pinned\n", r.id)
			continue
		}
		reason := "could not probe the server — tool list not pinned"
		if wn, ok := warnings[r.id]; ok {
			reason = "could not probe — " + wn.Reason
		}
		fmt.Fprintf(w, "  %-30s %s\n", r.id, reason)
	}
}

// shortHash truncates a "sha256:<hex>" string to a 12-char digest with an
// ellipsis suffix for display.
func shortHash(h string) string {
	if i := strings.IndexByte(h, ':'); i >= 0 {
		h = h[i+1:]
	}
	if len(h) > 12 {
		return h[:12] + "..."
	}
	return h
}

// lockSummary describes the entry counts across both lock files, listing only
// the channels that have entries.
func lockSummary(committed, personal *lockfile.Lock) string {
	count := func(pick func(*lockfile.Lock) map[string]lockfile.Entry) int {
		return len(pick(committed)) + len(pick(personal))
	}
	channels := []struct {
		singular string
		plural   string
		n        int
	}{
		{"MCP server", "MCP servers", count(func(l *lockfile.Lock) map[string]lockfile.Entry { return l.Entries.MCPServers })},
		{"background service", "background services", count(func(l *lockfile.Lock) map[string]lockfile.Entry { return l.Entries.BackgroundServices })},
		{"hook", "hooks", count(func(l *lockfile.Lock) map[string]lockfile.Entry { return l.Entries.Hooks })},
		{"command", "commands", count(func(l *lockfile.Lock) map[string]lockfile.Entry { return l.Entries.Commands })},
		{"CLI tool", "CLI tools", count(func(l *lockfile.Lock) map[string]lockfile.Entry { return l.Entries.CLITools })},
	}
	parts := []string{}
	for _, ch := range channels {
		if ch.n == 0 {
			continue
		}
		label := ch.plural
		if ch.n == 1 {
			label = ch.singular
		}
		parts = append(parts, fmt.Sprintf("%d %s", ch.n, label))
	}
	if len(parts) == 0 {
		return "0 entries (ainfra.yaml is empty — add some with `ainfra add`)"
	}
	out := parts[0]
	for _, p := range parts[1:] {
		out += ", " + p
	}
	return out
}
