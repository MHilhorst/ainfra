package main

import (
	"os"
	"testing"

	"github.com/MHilhorst/ainfra/internal/resolve"
)

// TestMain disables MCP introspection so command-level tests that invoke
// `ainfra lock` don't try to start real subprocesses for fixture MCP servers.
func TestMain(m *testing.M) {
	resolve.IntrospectRunner = resolve.DisableIntrospection
	os.Exit(m.Run())
}
