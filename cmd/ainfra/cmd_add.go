package main

import (
	"errors"
	"flag"
	"fmt"
	"path/filepath"

	"github.com/MHilhorst/ainfra/internal/cli"
	"github.com/MHilhorst/ainfra/internal/manifest/writer"
	"github.com/MHilhorst/ainfra/internal/provider"
	"github.com/MHilhorst/ainfra/internal/resolve"
	"github.com/MHilhorst/ainfra/internal/ui"
)

// channelAlias maps the short form a user types (`mcp`) to the canonical
// channel key as it appears in ainfra.yaml (`mcpServers`). The short forms
// match the package-manager-ish vocabulary users already have; the canonical
// keys preserve the existing manifest schema.
var channelAlias = map[string]string{
	"mcp":          "mcpServers",
	"mcpServers":   "mcpServers",
	"hook":         "hooks",
	"hooks":        "hooks",
	"command":      "commands",
	"commands":     "commands",
	"skill":        "skills",
	"skills":       "skills",
	"cliTool":      "cliTools",
	"cliTools":     "cliTools",
	"plugin":       "plugins",
	"plugins":      "plugins",
	"marketplace":  "marketplaces",
	"marketplaces": "marketplaces",
	"rule":         "rules",
	"rules":        "rules",
	"tool":         "tools",
	"tools":        "tools",
}

// channelDefaults returns the YAML body for a new entry in the given canonical
// channel. The first positional after the channel is the id; the second, if
// given, is the source / spec string for channels that carry one.
func channelDefaults(channel, source string) (string, error) {
	switch channel {
	case "mcpServers":
		// Sensible default: stdio MCP server launched via npx. Users edit
		// after `add` to refine. This mirrors what `ainfra init` scaffolds.
		body := `transport: stdio
command: npx
args: ["-y", "REPLACE_ME"]
version: "0.1.0"`
		return body, nil
	case "commands":
		if source == "" {
			return "", errors.New("commands require a source path (e.g. ./commands/<id>.md)")
		}
		return fmt.Sprintf("source: %s\ndescription: \"\"\n", source), nil
	case "skills":
		if source == "" {
			return "", errors.New("skills require a source (e.g. github:owner/repo/path)")
		}
		return fmt.Sprintf("source: %q\nversion: \"0.1.0\"\n", source), nil
	case "hooks":
		return `event: PostToolUse
matcher: "Edit|Write"
command: "echo REPLACE_ME"
timeout: 5000`, nil
	case "rules":
		if source == "" {
			return "", errors.New("rules require a source path")
		}
		return fmt.Sprintf("source: %s\n", source), nil
	default:
		// Generic fallback: empty mapping. The user will edit.
		return "{}", nil
	}
}

// newAddCommand wires `ainfra add <channel> <id> [source]`.
func newAddCommand() *cli.Command {
	var personal, noInstall bool
	return &cli.Command{
		Name:      "add",
		Summary:   "Add an entry to ainfra.yaml and reconcile (npm-install-style)",
		UsageLine: "ainfra add <channel> <id> [source] [--personal] [--no-install]",
		Example:   "ainfra add mcp github\n  ainfra add command audit ./commands/audit.md\n  ainfra add --personal mcp local-fs",
		SetFlags: func(fs *flag.FlagSet) {
			fs.BoolVar(&personal, "personal", false, "write to ainfra.personal.yaml instead of ainfra.yaml")
			fs.BoolVar(&noInstall, "no-install", false, "write the manifest entry and re-lock, but skip reconcile")
		},
		Run: func(ctx cli.Context) int {
			return runAdd(ctx, personal, noInstall)
		},
	}
}

func runAdd(ctx cli.Context, personal, noInstall bool) int {
	errColor := ui.NewColorizer(ctx.Stderr, ctx.NoColor)
	if len(ctx.Args) < 2 {
		ui.RenderError(ctx.Stderr, errColor, errors.New("usage: ainfra add <channel> <id> [source]"))
		return 2
	}
	rawChannel := ctx.Args[0]
	id := ctx.Args[1]
	source := ""
	if len(ctx.Args) >= 3 {
		source = ctx.Args[2]
	}

	canonical, ok := channelAlias[rawChannel]
	if !ok {
		ui.RenderError(ctx.Stderr, errColor, fmt.Errorf("unknown channel %q; valid: mcp, hook, command, skill, cliTool, plugin, marketplace, rule, tool", rawChannel))
		return 1
	}

	manifestFile := "ainfra.yaml"
	if personal {
		manifestFile = "ainfra.personal.yaml"
	}
	manifestPath := filepath.Join(ctx.Dir, manifestFile)
	if !fileExists(manifestPath) {
		if personal {
			ui.RenderError(ctx.Stderr, errColor, fmt.Errorf("%s not found — run `ainfra init --personal` first", manifestFile))
		} else {
			ui.RenderError(ctx.Stderr, errColor, fmt.Errorf("ainfra.yaml not found — run `ainfra init` first"))
		}
		return 1
	}

	body, err := channelDefaults(canonical, source)
	if err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}

	if err := writer.AddEntry(manifestPath, canonical, id, body); err != nil {
		if errors.Is(err, writer.ErrEntryExists) {
			ui.RenderError(ctx.Stderr, errColor, fmt.Errorf("%s.%s already exists — use `ainfra update %s %s` to bump or remove first", canonical, id, rawChannel, id))
			return 1
		}
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}

	fmt.Fprintf(ctx.Stdout, "Added %s.%s to %s\n", canonical, id, manifestFile)

	// Re-lock so the new entry lands in ainfra.lock.
	if err := resolve.RunLock(ctx.Dir, provider.ExecRunner{}); err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}

	if noInstall {
		fmt.Fprintln(ctx.Stdout, "Skipping install (--no-install).")
		return 0
	}

	// Run the standard install path. --yes is implicit on add (the user just
	// asked for this change to land); --strict is irrelevant; --dry-run is not
	// what the user wants here.
	return runApply(ctx, true /*yes*/, false /*dryRun*/, false /*noInstall*/, false /*strict*/)
}
