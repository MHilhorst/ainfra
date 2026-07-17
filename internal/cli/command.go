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
	"strings"

	"github.com/MHilhorst/ainfra/internal/diag"
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

	// SubParsesArgs reports, for a given leftover arg list, whether this
	// command parses the flags among them itself (e.g. `ainfra plugin release
	// --patch`). When it returns true the command is exempt from the
	// stray-flag check, because for that shape a flag after a positional is
	// intended rather than silently dropped.
	//
	// It takes the args rather than being a plain bool so a command can exempt
	// only the shapes it really sub-parses: `init` re-parses flags after `team
	// <path>`, but plain `ainfra init junk --force` drops --force like any
	// other command, and blanket-exempting init would hide that.
	//
	// Nil means never exempt. Do not return true for a shape the command does
	// NOT sub-parse: the flag really is dropped there, and this error is the
	// only thing telling the user their argument did nothing.
	SubParsesArgs func(args []string) bool
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
	subParses := cmd.SubParsesArgs != nil && cmd.SubParsesArgs(fs.Args())
	if stray := strayFlag(fs, cmdArgs, fs.Args()); stray != "" && !subParses {
		cz := ui.NewColorizer(r.stderr, *noColor)
		ui.RenderError(r.stderr, cz, &diag.Diagnostic{
			Summary: fmt.Sprintf("%s: %s comes after a positional argument, so it was not applied", cmd.Name, stray),
			Hint:    fmt.Sprintf("Flags must come before the positionals. Try:\n  %s", cmd.UsageLine),
		})
		return 2
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

// strayFlag returns the first leftover argument that names a flag this command
// actually registered, or "" when there is none.
//
// Go's flag package stops parsing at the first positional, so
// `ainfra add command ship ./x.md --global` silently leaves --global in Args
// and runs as if it were never passed: the entry lands in the team's
// ainfra.yaml instead of the user's global manifest, and the user believes
// they declared it. `--no-install` placed the same way is ignored and the
// install runs anyway. Both are silent wrong-thing-done outcomes, so the CLI
// refuses rather than guessing.
//
// It matches against the command's own FlagSet rather than "starts with a
// dash" on purpose. A dash-prefixed token that is not a flag of this command
// is just an unusual positional — `ainfra add command ship -draft.md` names a
// real file — and rejecting it would break invocations that work today. Only a
// token that would have done something had it been placed earlier is an error.
//
// Only tokens after an explicit "--" terminator are protected — that is what
// the terminator is for. A terminator later in the line does not retroactively
// excuse a dropped flag before it, so `add command ship x.md --global --` is
// still an error.
//
// When the terminator appears before any positional, flag.Parse consumes it and
// it is absent from positional; everything left is then protected by intent.
func strayFlag(fs *flag.FlagSet, raw, positional []string) string {
	if terminatorConsumed(fs, raw, positional) {
		return ""
	}
	for _, a := range positional {
		if a == "--" {
			return "" // everything from here on is a protected positional
		}
		name := strings.TrimLeft(a, "-")
		if name == a || name == "" {
			continue // not dash-prefixed, or a bare "-"
		}
		name, _, _ = strings.Cut(name, "=") // --flag=value
		if fs.Lookup(name) != nil {
			return a
		}
	}
	return ""
}

// terminatorConsumed reports whether flag.Parse swallowed a "--" terminator,
// which it does only when the terminator precedes every positional. In that
// case the caller asked for the remaining args to be taken literally.
//
// Parse never reorders, so the leftover positionals are always a suffix of raw
// and the token immediately before that suffix is whatever Parse swallowed
// last. That token is a terminator unless it was the VALUE of a preceding
// non-boolean flag: `ainfra install --agent -- bogus --dry-run` hands "--" to
// --agent, which protects nothing and must not excuse the dropped --dry-run.
//
// Testing for a leftover "--" instead would be wrong the other way: Parse
// strips exactly one, so a second literal terminator in `add -- command ship
// --global --` would read as "none consumed" and flag --global inside a tail
// the user explicitly marked literal.
func terminatorConsumed(fs *flag.FlagSet, raw, positional []string) bool {
	start := len(raw) - len(positional)
	if start <= 0 || raw[start-1] != "--" {
		return false
	}
	if start >= 2 && takesValue(fs, raw[start-2]) {
		return false // the "--" was that flag's value, not a terminator
	}
	return true
}

// takesValue reports whether tok names a registered non-boolean flag in the
// separate-value form (`--agent x`, not `--agent=x`), meaning Parse consumes
// the following token as its value.
func takesValue(fs *flag.FlagSet, tok string) bool {
	name := strings.TrimLeft(tok, "-")
	if name == tok || name == "" || strings.Contains(name, "=") {
		return false
	}
	f := fs.Lookup(name)
	if f == nil {
		return false
	}
	bf, ok := f.Value.(interface{ IsBoolFlag() bool })
	return !(ok && bf.IsBoolFlag()) // a bool flag never eats the next token
}
