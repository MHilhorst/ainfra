package secret

import "testing"

func TestPlaceholderVarIsDeterministicAndSanitized(t *testing.T) {
	got := PlaceholderVar("mcpServers", "linear-mcp", "token")
	want := "AINFRA_SECRET_MCPSERVERS_LINEAR_MCP_TOKEN"
	if got != want {
		t.Errorf("PlaceholderVar = %q, want %q", got, want)
	}
}

func TestPlaceholderWrapsVarInBraces(t *testing.T) {
	got := Placeholder("cliTools", "aws-cli", "ssoToken")
	want := "${AINFRA_SECRET_CLITOOLS_AWS_CLI_SSOTOKEN}"
	if got != want {
		t.Errorf("Placeholder = %q, want %q", got, want)
	}
}
