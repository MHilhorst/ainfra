// Package cli is ainfra's hand-rolled command framework: a registry of
// commands, per-command flag parsing, dispatch, help, and a did-you-mean
// suggestion. It depends only on the standard library and internal/ui.
package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/MHilhorst/ainfra/internal/ui"
)

// Context is what a command's Run receives.
type Context struct {
	Args     []string  // positional args left after the command's flags
	Stdin    io.Reader // where confirmation prompts and interactive input come from
	Stdout   io.Writer // where normal output goes
	Stderr   io.Writer // where errors go
	NoColor  bool      // resolved --no-color (from either flag position)
	Dir      string    // working directory, with --chdir applied
	Identity string    // resolved --identity (or AINFRA_IDENTITY); empty means "default"
}

// Command is one ainfra subcommand.
type Command struct {
	Name      string                 // the word typed after "ainfra"
	Summary   string                 // one line, shown in the overview
	UsageLine string                 // e.g. "ainfra init [--personal] [--force]"
	Example   string                 // optional, shown in per-command help
	SetFlags  func(fs *flag.FlagSet) // registers command-specific flags (optional)
	Run       func(ctx Context) int  // returns the process exit code

	// Hidden commands work normally but are omitted from `ainfra --help`.
	// Use for niche / advanced verbs that we keep working but don't want to
	// front-page (subscriber-mode helpers, etc.).
	Hidden bool
}

// Registry holds the registered commands and dispatches to them.
type Registry struct {
	commands []*Command
	stdin    io.Reader
	stdout   io.Writer
	stderr   io.Writer
	version  string
}

// NewRegistry returns a Registry writing to the given streams.
func NewRegistry(stdout, stderr io.Writer, version string) *Registry {
	return &Registry{stdin: os.Stdin, stdout: stdout, stderr: stderr, version: version}
}

// SetStdin sets the reader commands receive for interactive prompts.
func (r *Registry) SetStdin(stdin io.Reader) { r.stdin = stdin }

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

// Dispatch parses args (the process args after the program name), selects and
// runs a command, and returns the process exit code.
//
// Global flags (--chdir, --no-color, --version/-v) may appear in any order
// before the command name. --help/-h before a command prints the overview;
// after a command it prints that command's help.
func (r *Registry) Dispatch(args []string) int {
	// Global flags that precede the command name.
	global := flag.NewFlagSet("ainfra", flag.ContinueOnError)
	global.SetOutput(io.Discard)
	noColor := global.Bool("no-color", false, "disable colored output")
	chdir := global.String("chdir", "", "run as if started in this directory")
	identity := global.String("identity", "", "caller identity for scope filtering (overrides AINFRA_IDENTITY)")
	showVersion := global.Bool("version", false, "print the ainfra version")
	showV := global.Bool("v", false, "print the ainfra version")
	if err := global.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			r.printOverview()
			return 0
		}
		ui.RenderError(r.stderr, ui.NewColorizer(r.stderr, false), err)
		return 1
	}
	if *showVersion || *showV {
		fmt.Fprintf(r.stdout, "ainfra %s\n", r.version)
		return 0
	}
	rest := global.Args()
	if len(rest) == 0 {
		r.printOverview()
		return 0
	}

	cmdName, cmdArgs := rest[0], rest[1:]
	if cmdName == "help" {
		return r.runHelp(cmdArgs)
	}

	cmd := r.lookup(cmdName)
	if cmd == nil {
		r.printUnknown(cmdName)
		return 2
	}

	// Per-command flag set. --no-color is accepted after the command too;
	// --help/-h print this command's help via flag.ErrHelp.
	fs := flag.NewFlagSet(cmd.Name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if cmd.SetFlags != nil {
		cmd.SetFlags(fs)
	}
	localNoColor := fs.Bool("no-color", false, "disable colored output")
	if err := fs.Parse(cmdArgs); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			r.printCommandHelp(cmd)
			return 0
		}
		cz := ui.NewColorizer(r.stderr, *noColor)
		ui.RenderError(r.stderr, cz, fmt.Errorf("%s: %v", cmd.Name, err))
		return 1
	}

	dir := *chdir
	if dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			ui.RenderError(r.stderr, ui.NewColorizer(r.stderr, *noColor || *localNoColor), err)
			return 1
		}
		dir = wd
	}

	return cmd.Run(Context{
		Args:     fs.Args(),
		Stdin:    r.stdin,
		Stdout:   r.stdout,
		Stderr:   r.stderr,
		NoColor:  *noColor || *localNoColor,
		Dir:      dir,
		Identity: *identity,
	})
}

