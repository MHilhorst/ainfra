package main

import (
	"fmt"
	"path/filepath"

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
		Summary:   "Resolve the manifest and write ainfra.lock",
		UsageLine: "ainfra lock",
		Example:   "ainfra lock",
		Run:       runLock,
	}
}

func runLock(ctx cli.Context) int {
	if err := resolve.RunLock(ctx.Dir, provider.ExecRunner{}); err != nil {
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
	fmt.Fprintln(ctx.Stdout, "ainfra: resolved "+lockSummary(committed, personal))
	fmt.Fprintln(ctx.Stdout, "        wrote ainfra.lock and ainfra.personal.lock")
	ui.Next(ctx.Stdout, c, "run 'ainfra plan' to preview changes.")
	if layers, err := manifest.LoadLayers(ctx.Dir); err == nil {
		for _, w := range cliToolInstallWarnings(layers) {
			fmt.Fprintln(ctx.Stderr, c.Yellow("warning: "+w))
		}
	}
	return 0
}

// lockSummary describes the entry counts across both lock files, listing only
// the channels that have entries.
func lockSummary(committed, personal *lockfile.Lock) string {
	count := func(pick func(*lockfile.Lock) map[string]lockfile.Entry) int {
		return len(pick(committed)) + len(pick(personal))
	}
	type channel struct {
		label string
		n     int
	}
	channels := []channel{
		{"MCP servers", count(func(l *lockfile.Lock) map[string]lockfile.Entry { return l.Entries.MCPServers })},
		{"background services", count(func(l *lockfile.Lock) map[string]lockfile.Entry { return l.Entries.BackgroundServices })},
		{"hooks", count(func(l *lockfile.Lock) map[string]lockfile.Entry { return l.Entries.Hooks })},
		{"commands", count(func(l *lockfile.Lock) map[string]lockfile.Entry { return l.Entries.Commands })},
		{"CLI tools", count(func(l *lockfile.Lock) map[string]lockfile.Entry { return l.Entries.CLITools })},
	}
	parts := []string{}
	for _, ch := range channels {
		if ch.n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", ch.n, ch.label))
		}
	}
	if len(parts) == 0 {
		return "an empty manifest (no entries)"
	}
	out := parts[0]
	for _, p := range parts[1:] {
		out += ", " + p
	}
	return out
}
