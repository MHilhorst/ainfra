package cli

import (
	"bytes"
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
			name:       "flag after positionals is caught",
			raw:        []string{"command", "ship", "./x.md", "--global"},
			positional: []string{"command", "ship", "./x.md", "--global"},
			want:       "--global",
		},
		{
			name:       "flags before positionals parse normally",
			raw:        []string{"--global", "command", "ship", "./x.md"},
			positional: []string{"command", "ship", "./x.md"},
			want:       "",
		},
		{
			// "--" is the explicit "these are positionals" terminator, so a
			// leading dash after it is intentional, not a mistake.
			name:       "double dash disables the check",
			raw:        []string{"command", "ship", "--", "--weird-name"},
			positional: []string{"command", "ship", "--weird-name"},
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
			raw:        []string{"--yes"},
			positional: nil,
			want:       "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := strayFlag(c.raw, c.positional); got != c.want {
				t.Errorf("strayFlag = %q, want %q", got, c.want)
			}
		})
	}
}
