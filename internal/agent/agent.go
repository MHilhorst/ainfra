// Package agent is the registry of AI coding agents ainfra can target and the
// channels each one supports. It is the seam that makes ainfra agnostic to the
// agent: resolution stays target-neutral, and this registry decides which
// channels a chosen agent can render. See
// docs/superpowers/specs/2026-05-21-multi-agent-renderers-design.md.
package agent

// ID identifies a target AI coding agent.
type ID string

const (
	ClaudeCode    ID = "claude-code"
	Codex         ID = "codex"
	ClaudeDesktop ID = "claude-desktop"
)

// Default is the agent ainfra targets when no manifest layer names one.
const Default = ClaudeCode

// Channel names — the wire keys ainfra.yaml uses for each configurable channel.
const (
	ChannelMCPServers    = "mcpServers"
	ChannelSkills        = "skills"
	ChannelMarketplaces  = "marketplaces"
	ChannelPlugins       = "plugins"
	ChannelRules         = "rules"
	ChannelTools         = "tools"
	ChannelCLITools      = "cliTools"
	ChannelHooks         = "hooks"
	ChannelCommands      = "commands"
)

// capabilities records, per agent, which channels that agent can render. An
// agent missing from this map is unknown; a channel missing from an agent's
// set is one that agent cannot render.
var capabilities = map[ID]map[string]bool{
	ClaudeCode: {
		ChannelMCPServers: true, ChannelSkills: true, ChannelMarketplaces: true,
		ChannelPlugins: true, ChannelRules: true, ChannelTools: true,
		ChannelCLITools: true, ChannelHooks: true, ChannelCommands: true,
	},
	Codex: {
		ChannelMCPServers: true, ChannelRules: true, ChannelCLITools: true,
	},
	ClaudeDesktop: {
		ChannelMCPServers: true,
	},
}

// Known reports whether id names an agent ainfra can target.
func Known(id string) bool {
	_, ok := capabilities[ID(id)]
	return ok
}

// Supports reports whether agent id can render the named channel.
func Supports(id ID, channel string) bool {
	return capabilities[id][channel]
}
