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
	// A FlagSet shaped like `ainfra add`: the flags it really registers.
	newFS := func() *flag.FlagSet {
		fs := flag.NewFlagSet("add", flag.ContinueOnError)
		fs.Bool("personal", false, "")
		fs.Bool("global", false, "")
		fs.Bool("no-install", false, "")
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
			name:       "single-dash form is caught too",
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
			// Codex review of this change: a dash-prefixed token that is not a
			// flag of this command is a legitimate positional (a real filename).
			// Rejecting it would break invocations that work today.
			name:       "dash-prefixed non-flag positional is allowed",
			raw:        []string{"command", "ship", "-draft.md"},
			positional: []string{"command", "ship", "-draft.md"},
			want:       "",
		},
		{
			// "--" is the explicit "these are positionals" terminator.
			name:       "double dash disables the check",
			raw:        []string{"command", "ship", "--", "--global"},
			positional: []string{"command", "ship", "--global"},
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
