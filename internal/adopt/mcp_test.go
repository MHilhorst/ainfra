package adopt

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInferVersionFromArgs(t *testing.T) {
	cases := []struct {
		command string
		args    []string
		want    string
	}{
		{"npx", []string{"-y", "@modelcontextprotocol/server-filesystem@0.6.2"}, "0.6.2"},
		{"uvx", []string{"mcp-server-foo@1.2.3-rc1"}, "1.2.3-rc1"},
		{"npx", []string{"-y", "@modelcontextprotocol/server-x"}, ""},
		{"node", []string{"server@1.2.3"}, ""},
	}
	for _, tc := range cases {
		got := inferVersion(tc.command, tc.args)
		if got != tc.want {
			t.Errorf("inferVersion(%q,%v): got %q want %q", tc.command, tc.args, got, tc.want)
		}
	}
}

func TestReadMCPHttpDropsStdioFields(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(`{
		"mcpServers": {
			"a": {"type": "http", "url": "https://x", "command": "should-drop", "args": ["x"]}
		}
	}`), 0o644)
	servers, _, _, err := readMCP(dir)
	if err != nil {
		t.Fatal(err)
	}
	srv := servers["a"]
	if srv.Command != "" || len(srv.Args) != 0 || srv.Version != "" {
		t.Errorf("stdio fields not dropped for http: %+v", srv)
	}
}
