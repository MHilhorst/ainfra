// Package cli is ainfra's hand-rolled command framework: a registry of
// commands, per-command flag parsing, dispatch, help, and a did-you-mean
// suggestion. It depends only on the standard library and internal/ui.
package cli

import (
	"flag"
	"io"
)

// Context is what a command's Run receives.
type Context struct {
	Args    []string  // positional args left after the command's flags
	Stdout  io.Writer // where normal output goes
	Stderr  io.Writer // where errors go
	NoColor bool      // resolved --no-color (from either flag position)
	Dir     string    // working directory, with --chdir applied
}

// Command is one ainfra subcommand.
type Command struct {
	Name      string                 // the word typed after "ainfra"
	Summary   string                 // one line, shown in the overview
	UsageLine string                 // e.g. "ainfra init [--personal] [--force]"
	Example   string                 // optional, shown in per-command help
	SetFlags  func(fs *flag.FlagSet) // registers command-specific flags (optional)
	Run       func(ctx Context) int  // returns the process exit code
}

// Registry holds the registered commands and dispatches to them.
type Registry struct {
	commands []*Command
	stdout   io.Writer
	stderr   io.Writer
	version  string
}

// NewRegistry returns a Registry writing to the given streams.
func NewRegistry(stdout, stderr io.Writer, version string) *Registry {
	return &Registry{stdout: stdout, stderr: stderr, version: version}
}

// Add registers a command. Registration order is the order shown in the
// overview.
func (r *Registry) Add(c *Command) { r.commands = append(r.commands, c) }

// lookup returns the command with the given name, or nil.
func (r *Registry) lookup(name string) *Command {
	for _, c := range r.commands {
		if c.Name == name {
			return c
		}
	}
	return nil
}
