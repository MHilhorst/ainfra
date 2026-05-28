package resolve

import (
	"os"
	"testing"
)

// TestMain disables MCP introspection by default so existing pipeline tests
// don't try to start real npx subprocesses. Tests that exercise ToolsetHash
// reset IntrospectRunner explicitly. It also isolates XDG_CONFIG_HOME so a
// developer's real personal manifest at ~/.config/ainfra/personal.yaml does
// not leak into the layers under test.
func TestMain(m *testing.M) {
	IntrospectRunner = DisableIntrospection
	tmp, err := os.MkdirTemp("", "ainfra-test-xdg-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(tmp)
	os.Setenv("XDG_CONFIG_HOME", tmp)
	os.Exit(m.Run())
}
