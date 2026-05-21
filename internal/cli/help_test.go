package cli

import (
	"bytes"
	"flag"
	"strings"
	"testing"
)

func TestPrintOverviewListsCommands(t *testing.T) {
	var out bytes.Buffer
	r := NewRegistry(&out, &bytes.Buffer{}, "0.0.0-test")
	r.Add(newTestCommand("init"))
	r.Add(newTestCommand("lock"))
	r.printOverview()
	s := out.String()
	for _, want := range []string{"Usage:", "init", "lock", "summary of init", "--chdir"} {
		if !strings.Contains(s, want) {
			t.Errorf("overview missing %q\n---\n%s", want, s)
		}
	}
}

func TestPrintCommandHelpShowsFlags(t *testing.T) {
	var out bytes.Buffer
	r := NewRegistry(&out, &bytes.Buffer{}, "0.0.0-test")
	cmd := &Command{
		Name: "init", Summary: "scaffold a manifest",
		UsageLine: "ainfra init [--force]",
		Example:   "ainfra init",
		SetFlags:  func(fs *flag.FlagSet) { fs.Bool("force", false, "overwrite an existing file") },
	}
	r.printCommandHelp(cmd)
	s := out.String()
	for _, want := range []string{"ainfra init [--force]", "--force", "overwrite an existing file", "Example:"} {
		if !strings.Contains(s, want) {
			t.Errorf("command help missing %q\n---\n%s", want, s)
		}
	}
}

func TestPrintUnknownSuggestsClosest(t *testing.T) {
	var errOut bytes.Buffer
	r := NewRegistry(&bytes.Buffer{}, &errOut, "0.0.0-test")
	r.Add(newTestCommand("lock"))
	r.printUnknown("lok")
	s := errOut.String()
	if !strings.Contains(s, `unknown command "lok"`) {
		t.Errorf("missing unknown-command line: %q", s)
	}
	if !strings.Contains(s, `Did you mean "lock"?`) {
		t.Errorf("missing suggestion: %q", s)
	}
}

func TestLevenshtein(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"lock", "lock", 0},
		{"lok", "lock", 1},
		{"", "abc", 3},
		{"plan", "apply", 4},
	}
	for _, c := range cases {
		if got := levenshtein(c.a, c.b); got != c.want {
			t.Errorf("levenshtein(%q,%q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}
