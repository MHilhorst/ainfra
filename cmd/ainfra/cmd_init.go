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

# ainfra manifest — your team's AI coding agent setup as config-as-code.
# Schema: spec/manifest-schema.md   Guide: docs/quickstart.md

# Which AI coding agent ainfra renders for: claude-code (default) or codex.
agent: claude-code

# CLI tools the other channels depend on.
cliTools: {}

# MCP servers to land in each developer's agent config.
mcpServers: {}

# Hooks, commands, skills, plugins, and rules go here too —
# see spec/manifest-schema.md for the full schema.
`

// starterPersonal is the ainfra.personal.yaml that `ainfra init --personal`
// writes. The personal layer is git-ignored and never affects teammates.
const starterPersonal = `version: 1

# Your personal ainfra layer — overrides and additions just for you.
# This file is git-ignored; it never affects teammates.

mcpServers: {}
`

// usingAinfraSkillBlock is appended to the starter manifest when
// `ainfra init --with-skill` is passed. It pulls in the using-ainfra skill
// shipped from this repo so any AI agent that lands in the consumer's project
// learns the plan/apply/lock/check workflow.
const usingAinfraSkillBlock = `
# Teach AI agents how to use ainfra in this repo.
# Source: https://github.com/MHilhorst/ainfra/tree/main/skills/using-ainfra
skills:
  using-ainfra:
    source: "github:MHilhorst/ainfra/skills/using-ainfra"
    version: "0.1.0"
`

// newInitCommand scaffolds an ainfra.yaml (or ainfra.personal.yaml).
func newInitCommand() *cli.Command {
	var personal, force, withSkill bool
	return &cli.Command{
		Name:      "init",
		Summary:   "Scaffold an ainfra.yaml in the current repo",
		UsageLine: "ainfra init [--personal] [--with-skill] [--force]",
		Example:   "ainfra init --with-skill",
		SetFlags: func(fs *flag.FlagSet) {
			fs.BoolVar(&personal, "personal", false, "scaffold ainfra.personal.yaml instead")
			fs.BoolVar(&withSkill, "with-skill", false, "include the using-ainfra skill so AI agents know how to use ainfra")
			fs.BoolVar(&force, "force", false, "overwrite an existing file")
		},
		Run: func(ctx cli.Context) int { return runInit(ctx, personal, withSkill, force) },
	}
}

func runInit(ctx cli.Context, personal, withSkill, force bool) int {
	name, content := "ainfra.yaml", starterManifest
	if personal {
		name, content = "ainfra.personal.yaml", starterPersonal
	} else if withSkill {
		content = starterManifest + usingAinfraSkillBlock
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
	if withSkill && !personal {
		fmt.Fprintln(ctx.Stdout, "ainfra: included the using-ainfra skill — AI agents in this repo will learn the plan/apply/lock/check workflow.")
	}
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
