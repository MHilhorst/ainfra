package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/MHilhorst/ainfra/internal/adopt"
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

// newInitCommand scaffolds an ainfra.yaml in one of several flavors.
//
//   ainfra init                  — empty repo manifest
//   ainfra init --adopt          — import an existing ./.claude/ setup (was `ainfra adopt`)
//   ainfra init --personal       — empty ainfra.personal.yaml
//   ainfra init team <path>      — scaffold a team config repo at <path>, scanning
//                                  ~/.claude/ by default; --empty for a skeleton
//
// `--adopt` is bootstrap-only — once a manifest exists, run `ainfra install` to
// reconcile drift rather than re-adopting. `--force` is the only escape hatch
// and rewrites the manifest from scratch.
func newInitCommand() *cli.Command {
	var personal, force, withSkill, doAdopt, empty bool
	return &cli.Command{
		Name:    "init",
		Summary: "Scaffold an ainfra.yaml in the current repo, or a team config repo",
		UsageLine: "ainfra init [--personal | --adopt] [--with-skill] [--force]\n" +
			"       ainfra init team <path> [--empty] [--with-skill] [--force]",
		Example: "ainfra init team ../claude-config",
		SetFlags: func(fs *flag.FlagSet) {
			fs.BoolVar(&personal, "personal", false, "scaffold ainfra.personal.yaml instead")
			fs.BoolVar(&doAdopt, "adopt", false, "import the current repo's ./.claude/ setup into ainfra.yaml")
			fs.BoolVar(&withSkill, "with-skill", false, "include the using-ainfra skill so AI agents know how to use ainfra")
			fs.BoolVar(&force, "force", false, "overwrite existing files (team: allow non-empty target)")
			fs.BoolVar(&empty, "empty", false, "team: scaffold an empty manifest instead of scanning ~/.claude/")
		},
		Run: func(ctx cli.Context) int {
			if len(ctx.Args) > 0 && ctx.Args[0] == "team" {
				return runInitTeam(ctx, ctx.Args[1:], empty, withSkill, force)
			}
			return runInit(ctx, personal, withSkill, force, doAdopt)
		},
	}
}

func runInit(ctx cli.Context, personal, withSkill, force, doAdopt bool) int {
	errColor := ui.NewColorizer(ctx.Stderr, ctx.NoColor)

	if doAdopt && personal {
		ui.RenderError(ctx.Stderr, errColor, &diag.Diagnostic{
			Summary: "init: --adopt and --personal are mutually exclusive",
			Hint:    "--adopt imports an existing repo setup; --personal scaffolds a fresh personal layer.",
		})
		return 1
	}
	if doAdopt {
		return runAdopt(ctx, force)
	}

	name, content := "ainfra.yaml", starterManifest
	if personal {
		name, content = "ainfra.personal.yaml", starterPersonal
	} else if withSkill {
		content = starterManifest + usingAinfraSkillBlock
	}
	path := filepath.Join(ctx.Dir, name)

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
	fmt.Fprintln(ctx.Stdout, "Created "+name+".")
	if withSkill && !personal {
		fmt.Fprintln(ctx.Stdout, "Included the using-ainfra skill so AI agents in this repo learn how to use ainfra.")
	}
	ui.Next(ctx.Stdout, c, "edit "+name+" to declare what you want (mcpServers, hooks, commands, ...), then run `ainfra lock`.")
	return 0
}

// starterTeamManifest is the ainfra.yaml a fresh `ainfra init team` writes when
// --from-home is not passed. The shape mirrors the repo starter but the
// comments frame this repo as the team's shared layer that downstream repos
// reference via `extends:`.
const starterTeamManifest = `version: 1

# Team ainfra manifest — the shared AI coding agent setup your repos extend from.
# Schema: https://github.com/MHilhorst/ainfra/blob/main/spec/manifest-schema.md
#
# Other repos consume this manifest by adding:
#   extends:
#     - git+https://github.com/<org>/<this-repo>.git
# to their own ainfra.yaml.

agent: claude-code

cliTools: {}
mcpServers: {}
# hooks, commands, skills, plugins, rules: add as your team adopts them.
`

// teamReadme is the README scaffold dropped next to the team ainfra.yaml. Short
// on purpose — points readers at ainfra docs rather than restating them.
const teamReadme = `# Team Claude Config

Shared ainfra manifest for the team's AI coding agent setup.

## What this is

A central place for MCP servers, skills, plugins, hooks, slash commands, rules,
and CLI tools every repo in the org should get. Downstream repos reference this
manifest from their own ` + "`ainfra.yaml`" + ` via:

` + "```yaml" + `
extends:
  - git+https://github.com/<org>/<this-repo>.git
` + "```" + `

## Workflow

1. Edit ` + "`ainfra.yaml`" + ` to add or change shared tooling.
2. ` + "`ainfra validate`" + ` then ` + "`ainfra lock`" + ` and commit both files.
3. Tag a release; downstream repos pin to the tag in their ` + "`extends:`" + ` list.

See https://github.com/MHilhorst/ainfra for the full docs.
`

// runInitTeam scaffolds a fresh team claude-config repo at args[0]. The target
// must be empty or non-existent unless --force is set. By default the manifest
// is populated by scanning ~/.claude/ — the whole point of this command is
// "share my polished local setup." Pass --empty for a skeleton manifest.
// Always runs `git init` in the target so the operator can commit straight away.
//
// We re-parse flags from args here so that operators can pass flags after the
// positional path (`ainfra init team <path> --empty`). Stock Go flag parsing
// stops at the first non-flag token, so the top-level command's flag set only
// sees flags that came before "team".
func runInitTeam(ctx cli.Context, args []string, empty, withSkill, force bool) int {
	errColor := ui.NewColorizer(ctx.Stderr, ctx.NoColor)
	sub := flag.NewFlagSet("init team", flag.ContinueOnError)
	sub.SetOutput(ctx.Stderr)
	sub.BoolVar(&empty, "empty", empty, "scaffold an empty manifest instead of scanning ~/.claude/")
	sub.BoolVar(&withSkill, "with-skill", withSkill, "include the using-ainfra skill")
	sub.BoolVar(&force, "force", force, "allow scaffolding into a non-empty dir / overwrite existing files")
	// Walk args: positional tokens get appended to a clean slice; flag tokens
	// pass through sub.Parse via a second pass. This is how we accept flags
	// either before or after the path.
	var positional []string
	var flagTokens []string
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			flagTokens = append(flagTokens, a)
		} else {
			positional = append(positional, a)
		}
	}
	if err := sub.Parse(flagTokens); err != nil {
		return 1
	}

	if len(positional) == 0 || strings.TrimSpace(positional[0]) == "" {
		ui.RenderError(ctx.Stderr, errColor, &diag.Diagnostic{
			Summary: "init team: missing <path>",
			Hint:    "Usage: ainfra init team <path> [--from-home].",
		})
		return 1
	}
	target := positional[0]
	if !filepath.IsAbs(target) {
		target = filepath.Join(ctx.Dir, target)
	}

	// Target must be a fresh directory: non-existent, or empty. --force lifts
	// the empty requirement but still refuses to clobber an existing manifest.
	if entries, err := os.ReadDir(target); err == nil {
		if len(entries) > 0 && !force {
			ui.RenderError(ctx.Stderr, errColor, &diag.Diagnostic{
				Summary: "init team: " + target + " is not empty",
				Hint:    "Pick a fresh path, or pass --force to scaffold into a non-empty dir.",
			})
			return 1
		}
	} else if !os.IsNotExist(err) {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}

	manifestPath := filepath.Join(target, "ainfra.yaml")
	if _, err := os.Stat(manifestPath); err == nil && !force {
		ui.RenderError(ctx.Stderr, errColor, &diag.Diagnostic{
			Summary: "init team: " + manifestPath + " already exists",
			Hint:    "Pass --force to overwrite.",
		})
		return 1
	}

	var (
		manifestBytes []byte
		warnings      []adopt.Warning
	)
	if empty {
		content := starterTeamManifest
		if withSkill {
			content += usingAinfraSkillBlock
		}
		manifestBytes = []byte(content)
	} else {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			ui.RenderError(ctx.Stderr, errColor, &diag.Diagnostic{
				Summary: "init team: cannot resolve home directory",
				Hint:    "Set HOME and re-run, or pass --empty for a skeleton manifest.",
			})
			return 1
		}
		scanned, ws, err := adopt.ScanLayout(adopt.UserLayout(home))
		if err != nil {
			ui.RenderError(ctx.Stderr, errColor, err)
			return 1
		}
		out, err := adopt.Emit(scanned)
		if err != nil {
			ui.RenderError(ctx.Stderr, errColor, err)
			return 1
		}
		manifestBytes = out
		warnings = ws
	}

	if err := os.WriteFile(manifestPath, manifestBytes, 0o644); err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}
	readmePath := filepath.Join(target, "README.md")
	if _, err := os.Stat(readmePath); os.IsNotExist(err) {
		if err := os.WriteFile(readmePath, []byte(teamReadme), 0o644); err != nil {
			ui.RenderError(ctx.Stderr, errColor, err)
			return 1
		}
	}

	// `git init` so the operator can commit immediately. Silent failure: if git
	// is missing or the dir is already inside a worktree, we still printed the
	// files — the user can sort the VCS bit out themselves.
	gitInitialized := false
	if _, err := os.Stat(filepath.Join(target, ".git")); os.IsNotExist(err) {
		cmd := exec.Command("git", "init", "--quiet")
		cmd.Dir = target
		if err := cmd.Run(); err == nil {
			gitInitialized = true
		}
	}

	c := ui.NewColorizer(ctx.Stdout, ctx.NoColor)
	errC := ui.NewColorizer(ctx.Stderr, ctx.NoColor)
	fmt.Fprintln(ctx.Stdout, "ainfra: scaffolded team config at "+target)
	fmt.Fprintln(ctx.Stdout, "  wrote ainfra.yaml")
	fmt.Fprintln(ctx.Stdout, "  wrote README.md")
	if gitInitialized {
		fmt.Fprintln(ctx.Stdout, "  ran git init")
	}
	strippedCount := printAdoptWarnings(ctx.Stderr, errC, warnings)

	switch {
	case !empty && strippedCount > 0:
		ui.Next(ctx.Stdout, c, fmt.Sprintf("open ainfra.yaml, replace %d TODO secret ref(s) under 'secrets:', then run 'ainfra validate' from %s.", strippedCount, target))
	case !empty:
		ui.Next(ctx.Stdout, c, "review ainfra.yaml in "+target+", then run 'ainfra validate' and 'ainfra lock'.")
	default:
		ui.Next(ctx.Stdout, c, "edit "+filepath.Join(target, "ainfra.yaml")+", then run 'ainfra validate' and 'ainfra lock'.")
	}
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
