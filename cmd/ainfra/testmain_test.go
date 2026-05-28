package main

import (
	"os"
	"testing"

	"github.com/MHilhorst/ainfra/internal/check"
	"github.com/MHilhorst/ainfra/internal/resolve"
)

// TestMain disables MCP introspection so command-level tests that invoke
// `ainfra lock` or `ainfra check` don't try to start real subprocesses for
// fixture MCP servers. It also isolates XDG_CONFIG_HOME so a developer's
// real personal manifest at ~/.config/ainfra/personal.yaml does not leak
// into the test layers.
func TestMain(m *testing.M) {
	resolve.IntrospectRunner = resolve.DisableIntrospection
	check.IntrospectRunner = resolve.DisableIntrospection
	tmp, err := os.MkdirTemp("", "ainfra-test-xdg-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(tmp)
	os.Setenv("XDG_CONFIG_HOME", tmp)
	os.Exit(m.Run())
}
