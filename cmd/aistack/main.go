// Command aistack is the config-as-code CLI for Claude Code team environments.
//
// This is the foundation scaffold. The subcommands below are declared so the
// CLI surface is real and buildable from day one; their behaviour is filled in
// task-by-task per docs/superpowers/plans/2026-05-21-aistack-cli.md.
package main

import (
	"fmt"
	"os"

	"github.com/MHilhorst/aistack/internal/version"
)

const usage = `aistack — config-as-code for Claude Code team environments

Usage:
  aistack <command> [flags]

Commands:
  init      Scaffold an ai-stack.yaml in the current repo
  plan      Show the diff between desired and observed state
  apply     Reconcile the environment to match the manifest
  check     Verify the environment matches the lockfile; report drift
  lock      Resolve the manifest and write ai-stack.lock
  version   Print the aistack version

Run "aistack <command> --help" for command-specific detail.
`

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		fmt.Print(usage)
		return 0
	}
	switch args[0] {
	case "version", "--version", "-v":
		fmt.Printf("aistack %s\n", version.Version)
		return 0
	case "help", "--help", "-h":
		fmt.Print(usage)
		return 0
	case "init", "plan", "apply", "check", "lock":
		fmt.Fprintf(os.Stderr, "aistack: %q is not implemented yet (see docs/superpowers/plans/)\n", args[0])
		return 1
	default:
		fmt.Fprintf(os.Stderr, "aistack: unknown command %q\n\n%s", args[0], usage)
		return 2
	}
}
