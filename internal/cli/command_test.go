package cli

import (
	"bytes"
	"flag"
	"testing"
)

func newTestCommand(name string) *Command {
	return &Command{
		Name:      name,
		Summary:   "summary of " + name,
		UsageLine: "ainfra " + name,
		Run:       func(ctx Context) int { return 0 },
	}
}

func TestRegistryAddAndLookup(t *testing.T) {
	r := NewRegistry(&bytes.Buffer{}, &bytes.Buffer{}, "0.0.0-test")
	r.Add(newTestCommand("lock"))
	if r.lookup("lock") == nil {
		t.Error("lookup(lock) = nil after Add")
	}
	if r.lookup("absent") != nil {
		t.Error("lookup(absent) should be nil")
	}
}

func TestStrayFlag(t *testing.T) {
	// A FlagSet shaped like a real ainfra command.
	newFS := func() *flag.FlagSet {
		fs := flag.NewFlagSet("add", flag.ContinueOnError)
		fs.Bool("personal", false, "")
		fs.Bool("global", false, "")
		fs.Bool("no-install", false, "")
		fs.String("channel", "", "") // a value-taking flag, like list's
		return fs
	}

	cases := []struct {
		name       string
		raw        []string
		positional []string
		want       string
	}{
		{
			// The bug this exists for: --global after the positionals was
			// silently dropped, so the entry went to the team's ainfra.yaml
			// while the user believed it went to their global manifest.
			name:       "registered flag after positionals is caught",
			raw:        []string{"command", "ship", "./x.md", "--global"},
			positional: []string{"command", "ship", "./x.md", "--global"},
			want:       "--global",
		},
		{
			name:       "single-dash form is caught",
			raw:        []string{"command", "ship", "-global"},
			positional: []string{"command", "ship", "-global"},
			want:       "-global",
		},
		{
			name:       "--flag=value form is caught",
			raw:        []string{"command", "ship", "--global=true"},
			positional: []string{"command", "ship", "--global=true"},
			want:       "--global=true",
		},
		{
			name:       "flags before positionals parse normally",
			raw:        []string{"--global", "command", "ship", "./x.md"},
			positional: []string{"command", "ship", "./x.md"},
			want:       "",
		},
		{
			// A dash-prefixed token that is not a flag of this command is a
			// legitimate positional — a real filename. Rejecting it would break
			// invocations that work today.
			name:       "dash-prefixed non-flag positional is allowed",
			raw:        []string{"command", "ship", "-draft.md"},
			positional: []string{"command", "ship", "-draft.md"},
			want:       "",
		},
		{
			// Go's flag package would not parse ---global as a flag, so it is
			// not a dropped one either.
			name:       "triple-dash is not flag-shaped",
			raw:        []string{"command", "ship", "---global"},
			positional: []string{"command", "ship", "---global"},
			want:       "",
		},
		{
			// Any "--" means "take the rest literally". Deciding which "--"
			// Parse swallowed, and whether it was a terminator or a flag's
			// value, means reimplementing the parser — and a wrong guess
			// rejects a working command. Missing a drop is the safer error.
			name:       "a terminator anywhere disables the check",
			raw:        []string{"command", "ship", "--global", "--"},
			positional: []string{"command", "ship", "--global", "--"},
			want:       "",
		},
		{
			name:       "terminator consumed by Parse also disables it",
			raw:        []string{"--", "command", "--global"},
			positional: []string{"command", "--global"},
			want:       "",
		},
		{
			// Codex: `list --channel --channel -- --json` parses fine on main.
			// Whatever we do, it must not start erroring.
			name:       "flag-shaped value before a terminator is left alone",
			raw:        []string{"--channel", "--channel", "--", "--global"},
			positional: []string{"--global"},
			want:       "",
		},
		{
			name:       "a lone dash is a positional, not a flag",
			raw:        []string{"command", "ship", "-"},
			positional: []string{"command", "ship", "-"},
			want:       "",
		},
		{
			name:       "no positionals at all",
			raw:        []string{"--global"},
			positional: nil,
			want:       "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := strayFlag(newFS(), c.raw, c.positional); got != c.want {
				t.Errorf("strayFlag = %q, want %q", got, c.want)
			}
		})
	}
}
