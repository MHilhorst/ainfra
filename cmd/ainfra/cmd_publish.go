package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/MHilhorst/ainfra/internal/artifact"
	"github.com/MHilhorst/ainfra/internal/cli"
	"github.com/MHilhorst/ainfra/internal/manifest"
	"github.com/MHilhorst/ainfra/internal/resolve"
	"github.com/MHilhorst/ainfra/internal/ui"
)

// newPublishCommand packages the resolved lockfile into a subscriber artifact.
func newPublishCommand() *cli.Command {
	var out string
	return &cli.Command{
		Name:      "publish",
		Summary:   "Package the resolved lockfile into a subscriber artifact",
		UsageLine: "ainfra publish [--out <dir>]",
		Example:   "ainfra publish --out ./dist",
		SetFlags:  func(fs *flag.FlagSet) { fs.StringVar(&out, "out", "ainfra-artifact", "artifact output directory") },
		Run:       func(ctx cli.Context) int { return runPublish(ctx, out) },
	}
}

func runPublish(ctx cli.Context, out string) int {
	errColor := ui.NewColorizer(ctx.Stderr, ctx.NoColor)
	dir := ctx.Dir

	layers, err := manifest.LoadLayers(dir)
	if err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}
	repo := layers[manifest.LayerRepo]
	if repo == nil || repo.Publish == nil {
		ui.RenderError(ctx.Stderr, errColor, fmt.Errorf("no publish: block in ainfra.yaml — add one to publish an artifact"))
		return 1
	}
	pub := repo.Publish

	lockPath := filepath.Join(dir, "ainfra.lock")
	lockBytes, err := os.ReadFile(lockPath)
	if err != nil {
		ui.RenderError(ctx.Stderr, errColor, fmt.Errorf("ainfra.lock not found — run `ainfra lock` first"))
		return 1
	}

	// Resolve a relative --out path against the working directory.
	if !filepath.IsAbs(out) {
		out = filepath.Join(dir, out)
	}

	rendered, err := resolve.RenderResources(dir)
	if err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}
	renderedBytes, err := json.MarshalIndent(rendered, "", "  ")
	if err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}

	desc := artifact.Descriptor{
		SchemaVersion: 1,
		ArtifactURL:   pub.ArtifactURL,
		Agent:         pub.Agent,
		Sync: artifact.Sync{
			IntervalMinutes: pub.Sync.IntervalMinutes,
			RunAtLogin:      pub.Sync.RunAtLogin,
		},
	}
	files := map[string][]byte{
		"ainfra.lock":   lockBytes,
		"rendered.json": renderedBytes,
	}
	if err := artifact.Write(out, desc, files); err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}

	c := ui.NewColorizer(ctx.Stdout, ctx.NoColor)
	fmt.Fprintf(ctx.Stdout, "ainfra: wrote artifact to %s\n", out)
	ui.Next(ctx.Stdout, c, "upload the artifact directory to "+pub.ArtifactURL)
	return 0
}
