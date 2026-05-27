package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/MHilhorst/ainfra/internal/adopt"
	"github.com/MHilhorst/ainfra/internal/cli"
	"github.com/MHilhorst/ainfra/internal/diag"
	"github.com/MHilhorst/ainfra/internal/manifest"
	"github.com/MHilhorst/ainfra/internal/ui"
)

// newAdoptCommand scans an existing repo's .mcp.json, .claude/, and CLAUDE.md
// and emits a draft ainfra.yaml. It is the brownfield onramp: a one-shot import
// for teams who already have a Claude Code setup committed to git.
func newAdoptCommand() *cli.Command {
	var force, merge bool
	return &cli.Command{
		Name:      "adopt",
		Summary:   "Generate ainfra.yaml from existing .mcp.json / .claude/ / CLAUDE.md",
		UsageLine: "ainfra adopt [--force | --merge]",
		Example:   "ainfra adopt",
		SetFlags: func(fs *flag.FlagSet) {
			fs.BoolVar(&force, "force", false, "overwrite an existing ainfra.yaml")
			fs.BoolVar(&merge, "merge", false, "add new entries to an existing ainfra.yaml without overwriting existing keys")
		},
		Run: func(ctx cli.Context) int { return runAdopt(ctx, force, merge) },
	}
}

func runAdopt(ctx cli.Context, force, merge bool) int {
	errColor := ui.NewColorizer(ctx.Stderr, ctx.NoColor)
	path := filepath.Join(ctx.Dir, "ainfra.yaml")

	if force && merge {
		ui.RenderError(ctx.Stderr, errColor, &diag.Diagnostic{
			Summary: "adopt: --force and --merge are mutually exclusive",
			Hint:    "Pick one: --force overwrites; --merge adds only new entries.",
		})
		return 1
	}

	existingPresent := false
	if _, err := os.Stat(path); err == nil {
		existingPresent = true
	}
	if existingPresent && !force && !merge {
		ui.RenderError(ctx.Stderr, errColor, &diag.Diagnostic{
			Summary: "adopt: ainfra.yaml exists",
			Hint:    "Use --merge to add new entries or --force to overwrite.",
		})
		return 1
	}

	scanned, warnings, err := adopt.Scan(ctx.Dir)
	if err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}

	final := scanned
	if merge && existingPresent {
		layers, err := manifest.LoadLayers(ctx.Dir)
		if err != nil {
			ui.RenderError(ctx.Stderr, errColor, err)
			return 1
		}
		existing := layers[manifest.LayerRepo]
		if existing == nil {
			ui.RenderError(ctx.Stderr, errColor, fmt.Errorf("adopt: --merge: could not load existing ainfra.yaml"))
			return 1
		}
		merged, mws := mergeAdopt(*existing, scanned)
		final = merged
		warnings = append(warnings, mws...)
	}

	out, err := adopt.Emit(final)
	if err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}

	for _, w := range warnings {
		fmt.Fprintln(ctx.Stderr, w.Message)
	}

	c := ui.NewColorizer(ctx.Stdout, ctx.NoColor)
	fmt.Fprintln(ctx.Stdout, "ainfra: wrote "+filepath.Base(path))
	ui.Next(ctx.Stdout, c, "review ainfra.yaml, then run 'ainfra validate', 'ainfra lock', and 'ainfra plan'.")
	return 0
}

// mergeAdopt overlays scanned onto existing: existing keys win, new keys from
// scanned land. Returns one warning per newly-added entry per channel.
func mergeAdopt(existing, scanned manifest.Manifest) (manifest.Manifest, []adopt.Warning) {
	out := existing
	var warnings []adopt.Warning

	out.MCPServers, warnings = addNewMCP(out.MCPServers, scanned.MCPServers, "mcpServers", warnings)
	out.Secrets, warnings = addNewSecrets(out.Secrets, scanned.Secrets, "secrets", warnings)
	out.Hooks, warnings = addNewHooks(out.Hooks, scanned.Hooks, "hooks", warnings)
	out.Commands, warnings = addNewCommands(out.Commands, scanned.Commands, "commands", warnings)
	out.Rules, warnings = addNewRules(out.Rules, scanned.Rules, "rules", warnings)

	return out, warnings
}

func addNewMCP(dst, src map[string]manifest.MCPServer, channel string, ws []adopt.Warning) (map[string]manifest.MCPServer, []adopt.Warning) {
	if dst == nil {
		dst = map[string]manifest.MCPServer{}
	}
	for k, v := range src {
		if _, ok := dst[k]; ok {
			continue
		}
		dst[k] = v
		ws = append(ws, adopt.Warning{Message: fmt.Sprintf("adopt: adding %s.%s (not present in existing manifest)", channel, k)})
	}
	if len(dst) == 0 {
		return nil, ws
	}
	return dst, ws
}

func addNewSecrets(dst, src map[string]manifest.Secret, channel string, ws []adopt.Warning) (map[string]manifest.Secret, []adopt.Warning) {
	if dst == nil {
		dst = map[string]manifest.Secret{}
	}
	for k, v := range src {
		if _, ok := dst[k]; ok {
			continue
		}
		dst[k] = v
		ws = append(ws, adopt.Warning{Message: fmt.Sprintf("adopt: adding %s.%s (not present in existing manifest)", channel, k)})
	}
	if len(dst) == 0 {
		return nil, ws
	}
	return dst, ws
}

func addNewHooks(dst, src map[string]manifest.Hook, channel string, ws []adopt.Warning) (map[string]manifest.Hook, []adopt.Warning) {
	if dst == nil {
		dst = map[string]manifest.Hook{}
	}
	for k, v := range src {
		if _, ok := dst[k]; ok {
			continue
		}
		dst[k] = v
		ws = append(ws, adopt.Warning{Message: fmt.Sprintf("adopt: adding %s.%s (not present in existing manifest)", channel, k)})
	}
	if len(dst) == 0 {
		return nil, ws
	}
	return dst, ws
}

func addNewCommands(dst, src map[string]manifest.Command, channel string, ws []adopt.Warning) (map[string]manifest.Command, []adopt.Warning) {
	if dst == nil {
		dst = map[string]manifest.Command{}
	}
	for k, v := range src {
		if _, ok := dst[k]; ok {
			continue
		}
		dst[k] = v
		ws = append(ws, adopt.Warning{Message: fmt.Sprintf("adopt: adding %s.%s (not present in existing manifest)", channel, k)})
	}
	if len(dst) == 0 {
		return nil, ws
	}
	return dst, ws
}

func addNewRules(dst, src map[string]manifest.Rule, channel string, ws []adopt.Warning) (map[string]manifest.Rule, []adopt.Warning) {
	if dst == nil {
		dst = map[string]manifest.Rule{}
	}
	for k, v := range src {
		if _, ok := dst[k]; ok {
			continue
		}
		dst[k] = v
		ws = append(ws, adopt.Warning{Message: fmt.Sprintf("adopt: adding %s.%s (not present in existing manifest)", channel, k)})
	}
	if len(dst) == 0 {
		return nil, ws
	}
	return dst, ws
}
