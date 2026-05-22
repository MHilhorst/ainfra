// Package agentset assembles the channel provider set for a target agent. It
// is the seam that makes reconciliation agent-aware: the plan/apply/check
// commands resolve the agent and call ForAgent to get the providers to run.
package agentset

import (
	"fmt"

	"github.com/MHilhorst/ainfra/internal/agent"
	"github.com/MHilhorst/ainfra/internal/provider"
	"github.com/MHilhorst/ainfra/internal/provider/claudecode"
	"github.com/MHilhorst/ainfra/internal/provider/claudedesktop"
	"github.com/MHilhorst/ainfra/internal/provider/codex"
	"github.com/MHilhorst/ainfra/internal/provider/shared"
)

// sharedProviders are the agent-agnostic providers every agent set includes.
func sharedProviders() []provider.Provider {
	return []provider.Provider{shared.CLITools{}}
}

// ForAgent returns the channel providers that reconcile config for the given
// agent: the agent-specific providers plus the shared, agent-agnostic ones.
// An agent with no provider set is an error; manifest validation rejects an
// unknown agent earlier, so this is a defence-in-depth backstop, never a
// silent empty set.
func ForAgent(id agent.ID) ([]provider.Provider, error) {
	switch id {
	case agent.ClaudeCode:
		return append([]provider.Provider{
			claudecode.MCP{},
			claudecode.Hooks{},
			claudecode.Commands{},
			claudecode.Rules{},
			claudecode.Skills{},
			claudecode.Plugins{},
			claudecode.Services{},
			claudecode.Tools{},
		}, sharedProviders()...), nil
	case agent.ClaudeDesktop:
		return append([]provider.Provider{
			claudedesktop.MCP{},
		}, sharedProviders()...), nil
	case agent.Codex:
		return append([]provider.Provider{
			codex.MCP{},
			codex.Rules{},
		}, sharedProviders()...), nil
	default:
		return nil, fmt.Errorf("no provider set for agent %q", id)
	}
}
