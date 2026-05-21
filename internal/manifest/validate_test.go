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
	if err == nil || !strings.Contains(err.Error(), "unknown template") {
		t.Fatalf("want unknown-template error, got %v", err)
	}
}

func TestValidateRejectsUnknownHookEvent(t *testing.T) {
	m := &Manifest{Version: 1, Hooks: map[string]Hook{
		"h": {Event: "OnEverything", Command: "echo x"},
	}}
	err := Validate(m)
	if err == nil || !strings.Contains(err.Error(), "event") {
		t.Fatalf("want hook-event error, got %v", err)
	}
}

func TestValidateRejectsHookWithoutCommand(t *testing.T) {
	m := &Manifest{Version: 1, Hooks: map[string]Hook{
		"h": {Event: "SessionStart"},
	}}
	err := Validate(m)
	if err == nil || !strings.Contains(err.Error(), "command") {
		t.Fatalf("want missing-command error, got %v", err)
	}
}

func TestValidateRejectsCommandWithoutSource(t *testing.T) {
	m := &Manifest{Version: 1, Commands: map[string]Command{
		"c": {Description: "no source"},
	}}
	err := Validate(m)
	if err == nil || !strings.Contains(err.Error(), "source") {
		t.Fatalf("want missing-source error, got %v", err)
	}
}

func TestValidateAcceptsValidHooksAndCommands(t *testing.T) {
	m := &Manifest{Version: 1,
		Hooks: map[string]Hook{
			"h": {Event: "PreToolUse", Matcher: "Bash", Command: "echo guard"},
		},
		Commands: map[string]Command{
			"c": {Source: "./commands/c.md", Description: "a command"},
		},
	}
	if err := Validate(m); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
