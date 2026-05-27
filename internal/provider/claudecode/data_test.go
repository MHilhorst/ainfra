package claudecode_test

import (
	"testing"

	"github.com/MHilhorst/ainfra/internal/provider"
	"github.com/MHilhorst/ainfra/internal/provider/claudecode"
)

func TestPluginDataID(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		// From the plugins reference: "formatter@my-marketplace" -> "formatter-my-marketplace".
		{"formatter@my-marketplace", "formatter-my-marketplace"},
		// All allowed characters pass through unchanged.
		{"abc_DEF-123", "abc_DEF-123"},
		// Spaces, dots, slashes, and other punctuation collapse to '-'.
		{"a b.c/d", "a-b-c-d"},
		// Empty input is empty output.
		{"", ""},
		// Unicode bytes are replaced byte-by-byte.
		{"café", "caf--"},
	}
	for _, c := range cases {
		if got := claudecode.PluginDataID(c.in); got != c.want {
			t.Errorf("PluginDataID(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestPluginDataDir(t *testing.T) {
	env := provider.Env{Home: "/home/user"}
	got := claudecode.PluginDataDir(env, "formatter@my-marketplace")
	want := "/home/user/.claude/plugins/data/formatter-my-marketplace"
	if got != want {
		t.Errorf("PluginDataDir = %q, want %q", got, want)
	}
}
