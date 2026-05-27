package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/MHilhorst/ainfra/internal/cli"
	"github.com/MHilhorst/ainfra/internal/installer"
	"github.com/MHilhorst/ainfra/internal/manifest"
	"github.com/MHilhorst/ainfra/internal/ui"
)

// newInstallerCommand generates a one-time macOS installer that sets up a
// launchd scheduled job keeping the subscriber silently in sync.
func newInstallerCommand() *cli.Command {
	var out string
	return &cli.Command{
		Name:      "installer",
		Summary:   "Generate a one-time macOS installer for subscriber machines",
		UsageLine: "ainfra installer [--out <file>]",
		Example:   "ainfra installer --out ainfra-install.command",
		Hidden:    true,
		SetFlags: func(fs *flag.FlagSet) {
			fs.StringVar(&out, "out", "ainfra-install.command", "output .command file path")
		},
		Run: func(ctx cli.Context) int { return runInstaller(ctx, out) },
	}
}

func runInstaller(ctx cli.Context, out string) int {
	errColor := ui.NewColorizer(ctx.Stderr, ctx.NoColor)
	dir := ctx.Dir

	layers, err := manifest.LoadLayers(dir)
	if err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}
	repo := layers[manifest.LayerRepo]
	if repo == nil || repo.Publish == nil {
		ui.RenderError(ctx.Stderr, errColor, fmt.Errorf("no publish: block in ainfra.yaml — add one to generate an installer"))
		return 1
	}
	if err := manifest.ValidateAll(layers); err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}
	pub := repo.Publish

	params := installer.Params{
		Label: "com.ainfra.subscriber",
		// A launchd agent runs with a minimal PATH, so the plist must name an
		// absolute binary. The install script substitutes this placeholder
		// with the ainfra path it resolves on the subscriber's machine.
		BinPath:         installer.BinPathPlaceholder,
		ArtifactURL:     pub.ArtifactURL,
		IntervalMinutes: pub.Sync.IntervalMinutes,
		RunAtLogin:      pub.Sync.RunAtLogin,
	}

	plist := installer.LaunchdPlist(params)
	script := installer.InstallScript(params, plist)

	// Resolve a relative --out path against the working directory.
	if !filepath.IsAbs(out) {
		out = filepath.Join(dir, out)
	}

	if err := os.WriteFile(out, []byte(script), 0o755); err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}

	c := ui.NewColorizer(ctx.Stdout, ctx.NoColor)
	fmt.Fprintf(ctx.Stdout, "ainfra: wrote installer to %s\n", out)
	ui.Next(ctx.Stdout, c, "send this file to the subscriber; they double-click it once")
	return 0
}
