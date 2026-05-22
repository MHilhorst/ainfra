package agentset_test

import (
	"testing"

	"github.com/MHilhorst/ainfra/internal/agent"
	"github.com/MHilhorst/ainfra/internal/provider/agentset"
)

func TestForAgentClaudeCodeReturnsEveryChannel(t *testing.T) {
	ps, err := agentset.ForAgent(agent.ClaudeCode)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string]bool{
		"mcpServers": true, "hooks": true, "commands": true, "rules": true,
		"skills": true, "plugins": true, "backgroundServices": true,
		"tools": true, "cliTools": true,
	}
	got := map[string]bool{}
	for _, p := range ps {
		ch := p.Channel()
		if got[ch] {
			t.Errorf("duplicate channel %q", ch)
		}
		got[ch] = true
	}
	if len(got) != len(want) {
		t.Fatalf("got %d distinct channels, want %d", len(got), len(want))
	}
	for ch := range want {
		if !got[ch] {
			t.Errorf("missing channel %q", ch)
		}
	}
}

func TestForAgentCodexReturnsItsChannels(t *testing.T) {
	ps, err := agentset.ForAgent(agent.Codex)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string]bool{"mcpServers": true, "rules": true, "cliTools": true}
	got := map[string]bool{}
	for _, p := range ps {
		got[p.Channel()] = true
	}
	if len(got) != len(want) {
		t.Fatalf("got %d distinct channels %v, want %d %v", len(got), got, len(want), want)
	}
	for ch := range want {
		if !got[ch] {
			t.Errorf("missing channel %q", ch)
		}
	}
}

func TestForAgentUnknownErrors(t *testing.T) {
	if _, err := agentset.ForAgent(agent.ID("emacs-doctor")); err == nil {
		t.Error("expected an error for an unknown agent, got nil")
	}
}

func TestForAgentClaudeDesktop(t *testing.T) {
	ps, err := agentset.ForAgent(agent.ClaudeDesktop)
	if err != nil {
		t.Fatalf("ForAgent(claude-desktop): %v", err)
	}
	var hasMCP bool
	for _, p := range ps {
		if p.Channel() == "mcpServers" {
			hasMCP = true
		}
	}
	if !hasMCP {
		t.Error("claude-desktop provider set must include the mcpServers provider")
	}
}
