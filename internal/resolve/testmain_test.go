package resolve

import (
	"os"
	"testing"
)

// TestMain disables MCP introspection by default so existing pipeline tests
// don't try to start real npx subprocesses. Tests that exercise ToolsetHash
// reset IntrospectRunner explicitly.
func TestMain(m *testing.M) {
	IntrospectRunner = DisableIntrospection
	os.Exit(m.Run())
}
