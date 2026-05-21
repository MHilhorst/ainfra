// Package cli is ainfra's hand-rolled command framework: a registry of
// commands, per-command flag parsing, dispatch, help, and a did-you-mean
// suggestion. It depends only on the standard library and internal/ui.
package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/MHilhorst/ainfra/internal/ui"
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

// Dispatch parses args (the process args after the program name), selects and
// runs a command, and returns the process exit code.
//
// Global flags (--chdir, --no-color) may appear before the command name;
// --no-color is also accepted after it. --help/-h and --version/-v are
// recognized as leading shortcuts.
func (r *Registry) Dispatch(args []string) int {
	// Leading --help/--version shortcuts, before any command name.
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			break
		}
		switch a {
		case "-h", "--help":
			r.printOverview()
			return 0
		case "-v", "--version":
			fmt.Fprintf(r.stdout, "ainfra %s\n", r.version)
			return 0
		}
	}

	// Global flags that precede the command name.
	global := flag.NewFlagSet("ainfra", flag.ContinueOnError)
	global.SetOutput(io.Discard)
	noColor := global.Bool("no-color", false, "disable colored output")
	chdir := global.String("chdir", "", "run as if started in this directory")
	if err := global.Parse(args); err != nil {
		ui.RenderError(r.stderr, ui.NewColorizer(r.stderr, false), err)
		return 1
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

	// Per-command flag set. --help and --no-color are accepted on every
	// command; --no-color here merges with the global one.
	fs := flag.NewFlagSet(cmd.Name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if cmd.SetFlags != nil {
		cmd.SetFlags(fs)
	}
	helpWanted := fs.Bool("help", false, "show help for this command")
	localNoColor := fs.Bool("no-color", false, "disable colored output")
	if err := fs.Parse(cmdArgs); err != nil {
		cz := ui.NewColorizer(r.stderr, *noColor)
		ui.RenderError(r.stderr, cz, fmt.Errorf("%s: %v", cmd.Name, err))
		return 1
	}
	if *helpWanted {
		r.printCommandHelp(cmd)
		return 0
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
		Args:    fs.Args(),
		Stdout:  r.stdout,
		Stderr:  r.stderr,
		NoColor: *noColor || *localNoColor,
		Dir:     dir,
	})
}
