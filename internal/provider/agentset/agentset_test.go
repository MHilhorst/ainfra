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

func TestForAgentCodexNotYetAvailable(t *testing.T) {
	if _, err := agentset.ForAgent(agent.Codex); err == nil {
		t.Error("expected an error for the codex set (built in plan 2b), got nil")
	}
}

func TestForAgentUnknownErrors(t *testing.T) {
	if _, err := agentset.ForAgent(agent.ID("emacs-doctor")); err == nil {
		t.Error("expected an error for an unknown agent, got nil")
	}
}
