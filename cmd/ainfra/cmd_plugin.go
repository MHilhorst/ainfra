package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/MHilhorst/ainfra/internal/cli"
	"github.com/MHilhorst/ainfra/internal/lockfile"
	"github.com/MHilhorst/ainfra/internal/manifest"
	"github.com/MHilhorst/ainfra/internal/plugin"
	"github.com/MHilhorst/ainfra/internal/ui"
)

// newPluginCommand wires `ainfra plugin build|release`.
func newPluginCommand() *cli.Command {
	return &cli.Command{
		Name:      "plugin",
		Summary:   "Build and release this repo's own Claude Code plugin",
		UsageLine: "ainfra plugin <build|release> [--patch|--minor|--major]",
		Example:   "ainfra plugin release --patch",
		Run: func(ctx cli.Context) int {
			return runPlugin(ctx)
		},
	}
}

func runPlugin(ctx cli.Context) int {
	errColor := ui.NewColorizer(ctx.Stderr, ctx.NoColor)
	if len(ctx.Args) < 1 {
		ui.RenderError(ctx.Stderr, errColor, errors.New("usage: ainfra plugin <build|release>"))
		return 2
	}
	action := ctx.Args[0]

	// Parse subcommand flags from args after the action word.
	sub := flag.NewFlagSet("plugin "+action, flag.ContinueOnError)
	sub.SetOutput(ctx.Stderr)
	var patch, minor, major bool
	sub.BoolVar(&patch, "patch", false, "bump the patch version on release")
	sub.BoolVar(&minor, "minor", false, "bump the minor version on release")
	sub.BoolVar(&major, "major", false, "bump the major version on release")
	if err := sub.Parse(ctx.Args[1:]); err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 2
	}
	level := ""
	switch {
	case major:
		level = "major"
	case minor:
		level = "minor"
	case patch:
		level = "patch"
	}

	layers, err := manifest.LoadLayers(ctx.Dir)
	if err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}
	repo := layers[manifest.LayerRepo]
	if repo == nil || repo.Plugin == nil {
		ui.RenderError(ctx.Stderr, errColor, errors.New("no plugin: block in ainfra.yaml"))
		return 1
	}
	pb := *repo.Plugin

	switch action {
	case "build":
		version := currentPluginVersion(ctx.Dir, pb.Name)
		if err := writePluginFiles(ctx.Dir, pb, version); err != nil {
			ui.RenderError(ctx.Stderr, errColor, err)
			return 1
		}
		fmt.Fprintf(ctx.Stdout, "Built plugin %s at version %s.\n", pb.Name, version)
		return 0

	case "release":
		return runPluginRelease(ctx, pb, level, errColor)

	default:
		ui.RenderError(ctx.Stderr, errColor, fmt.Errorf("unknown plugin action %q (want build or release)", action))
		return 2
	}
}

func runPluginRelease(ctx cli.Context, pb manifest.PluginBuild, level string, errColor ui.Colorizer) int {
	if warn, err := claudeValidatePlugin(ctx.Dir); err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	} else if warn != "" {
		fmt.Fprintln(ctx.Stderr, warn)
	}

	hash, err := plugin.ReleaseHash(ctx.Dir, pb)
	if err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}

	lockPath := filepath.Join(ctx.Dir, "ainfra.lock")
	lock, err := lockfile.Read(lockPath)
	if err != nil {
		ui.RenderError(ctx.Stderr, errColor, fmt.Errorf("ainfra.lock not found — run `ainfra lock` first"))
		return 1
	}
	base := lock.Plugin
	if base == nil {
		base = &lockfile.PluginBaseline{Name: pb.Name, Version: "0.0.0", ContentHash: ""}
	}

	decision, err := plugin.Decide(hash, base.ContentHash, base.Version, level)
	if err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}
	if decision.Action == plugin.ActionNoop {
		fmt.Fprintf(ctx.Stdout, "Nothing changed since v%s.\n", base.Version)
		return 0
	}

	if err := writePluginFiles(ctx.Dir, pb, decision.NewVersion); err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}
	lock.Plugin = &lockfile.PluginBaseline{Name: pb.Name, Version: decision.NewVersion, ContentHash: hash}
	if err := lockfile.Write(lockPath, lock); err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}
	fmt.Fprintf(ctx.Stdout, "Released %s %s -> %s.\n", pb.Name, decision.OldVersion, decision.NewVersion)
	return 0
}

// writePluginFiles renders plugin.json and merges the marketplace self-entry.
func writePluginFiles(dir string, pb manifest.PluginBuild, version string) error {
	pjPath := filepath.Join(dir, ".claude-plugin", "plugin.json")
	if err := os.MkdirAll(filepath.Dir(pjPath), 0o755); err != nil {
		return err
	}
	pj, err := plugin.RenderPluginJSON(pb, version)
	if err != nil {
		return err
	}
	if err := os.WriteFile(pjPath, pj, 0o644); err != nil {
		return err
	}

	mkPath := filepath.Join(dir, ".claude-plugin", "marketplace.json")
	existing, err := os.ReadFile(mkPath)
	if err != nil {
		return fmt.Errorf("plugin: read marketplace.json: %w", err)
	}
	merged, err := plugin.MergeMarketplaceEntry(existing, pb)
	if err != nil {
		return err
	}
	return os.WriteFile(mkPath, merged, 0o644)
}

// currentPluginVersion reads the version from an existing plugin.json, falling
// back to "0.0.0" when none exists.
func currentPluginVersion(dir, name string) string {
	raw, err := os.ReadFile(filepath.Join(dir, ".claude-plugin", "plugin.json"))
	if err != nil {
		return "0.0.0"
	}
	var doc struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil || doc.Version == "" {
		return "0.0.0"
	}
	return doc.Version
}

// claudeValidatePlugin runs `claude plugin validate <dir>` to validate the
// plugin/marketplace manifest at dir. A missing claude binary returns a warning
// (not an error) so offline maintainers and tests are not blocked; a present
// binary that fails returns an error.
func claudeValidatePlugin(dir string) (warn string, err error) {
	path, lookErr := exec.LookPath("claude")
	if lookErr != nil {
		return "warning: claude CLI not found; skipped `claude plugin validate`.", nil
	}
	cmd := exec.Command(path, "plugin", "validate", dir)
	out, runErr := cmd.CombinedOutput()
	if runErr != nil {
		return "", fmt.Errorf("claude plugin validate failed: %s", string(out))
	}
	return "", nil
}
