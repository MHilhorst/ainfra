package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/MHilhorst/ainfra/internal/cli"
	"github.com/MHilhorst/ainfra/internal/diag"
	"github.com/MHilhorst/ainfra/internal/ui"
)

// starterManifest is the ainfra.yaml a fresh `ainfra init` writes.
const starterManifest = `version: 1

# ainfra manifest — your team's Claude Code setup as config-as-code.
# Schema: spec/manifest-schema.md   Guide: docs/quickstart.md

# CLI tools the other channels depend on.
cliTools: {}

# MCP servers to land in each developer's .mcp.json.
mcpServers: {}

# Hooks, commands, skills, plugins, and CLAUDE.md rules go here too —
# see spec/manifest-schema.md for the full schema.
`

// starterPersonal is the ainfra.personal.yaml that `ainfra init --personal`
// writes. The personal layer is git-ignored and never affects teammates.
const starterPersonal = `version: 1

# Your personal ainfra layer — overrides and additions just for you.
# This file is git-ignored; it never affects teammates.

mcpServers: {}
`

// newInitCommand scaffolds an ainfra.yaml (or ainfra.personal.yaml).
func newInitCommand() *cli.Command {
	var personal, force bool
	return &cli.Command{
		Name:      "init",
		Summary:   "Scaffold an ainfra.yaml in the current repo",
		UsageLine: "ainfra init [--personal] [--force]",
		Example:   "ainfra init",
		SetFlags: func(fs *flag.FlagSet) {
			fs.BoolVar(&personal, "personal", false, "scaffold ainfra.personal.yaml instead")
			fs.BoolVar(&force, "force", false, "overwrite an existing file")
		},
		Run: func(ctx cli.Context) int { return runInit(ctx, personal, force) },
	}
}

func runInit(ctx cli.Context, personal, force bool) int {
	name, content := "ainfra.yaml", starterManifest
	if personal {
		name, content = "ainfra.personal.yaml", starterPersonal
	}
	path := filepath.Join(ctx.Dir, name)
	errColor := ui.NewColorizer(ctx.Stderr, ctx.NoColor)

	if !force {
		if _, err := os.Stat(path); err == nil {
			ui.RenderError(ctx.Stderr, errColor, &diag.Diagnostic{
				Summary: name + " already exists",
				Hint:    "Pass --force to overwrite it.",
			})
			return 1
		}
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}
	if err := ensureGitignore(ctx.Dir); err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}

	c := ui.NewColorizer(ctx.Stdout, ctx.NoColor)
	fmt.Fprintln(ctx.Stdout, "ainfra: created "+name)
	ui.Next(ctx.Stdout, c, "edit "+name+", then run 'ainfra lock'.")
	return 0
}

// gitignoreEntry is the pattern init keeps in .gitignore so a developer's
// personal layer is never committed.
const gitignoreEntry = "ainfra.personal.*"

// ensureGitignore appends gitignoreEntry to .gitignore (creating the file if
// absent) unless it is already present. It is idempotent.
func ensureGitignore(dir string) error {
	path := filepath.Join(dir, ".gitignore")
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == gitignoreEntry {
			return nil
		}
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	prefix := ""
	if len(data) > 0 && !strings.HasSuffix(string(data), "\n") {
		prefix = "\n"
	}
	_, err = f.WriteString(prefix + gitignoreEntry + "\n")
	return err
}
