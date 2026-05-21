package manifest

import (
	"strings"
	"testing"
)

func TestValidateRejectsFloatingMCPVersion(t *testing.T) {
	m := &Manifest{Version: 1, MCPServers: map[string]MCPServer{
		"s": {Command: "npx", Args: []string{"-y", "pkg@latest"}},
	}}
	err := Validate(m)
	if err == nil || !strings.Contains(err.Error(), "pin an exact version") {
		t.Fatalf("want pinned-version error, got %v", err)
	}
}

func TestValidateAcceptsPinnedMCPVersion(t *testing.T) {
	m := &Manifest{Version: 1, MCPServers: map[string]MCPServer{
		"s": {Command: "npx", Args: []string{"-y", "pkg"}, Version: "1.2.3"},
	}}
	if err := Validate(m); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateRejectsUnknownTemplate(t *testing.T) {
	m := &Manifest{Version: 1, MCPServers: map[string]MCPServer{
		"s": {Template: "missing"},
	}}
	err := Validate(m)
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("want unknown-template error, got %v", err)
	}
}
