package agent

import "testing"

func TestKnownRecognizesRegisteredAgents(t *testing.T) {
	for _, id := range []string{"claude-code", "codex"} {
		if !Known(id) {
			t.Errorf("Known(%q) = false, want true", id)
		}
	}
	if Known("emacs-doctor") {
		t.Error(`Known("emacs-doctor") = true, want false`)
	}
}

func TestClaudeCodeSupportsEveryChannel(t *testing.T) {
	for _, ch := range []string{
		ChannelMCPServers, ChannelSkills, ChannelPlugins, ChannelRules,
		ChannelTools, ChannelCLITools, ChannelHooks, ChannelCommands,
	} {
		if !Supports(ClaudeCode, ch) {
			t.Errorf("Supports(ClaudeCode, %q) = false, want true", ch)
		}
	}
}

func TestCodexSupportsOnlyPortableChannels(t *testing.T) {
	supported := map[string]bool{
		ChannelMCPServers: true, ChannelRules: true, ChannelCLITools: true,
	}
	for _, ch := range []string{
		ChannelMCPServers, ChannelSkills, ChannelPlugins, ChannelRules,
		ChannelTools, ChannelCLITools, ChannelHooks, ChannelCommands,
	} {
		if got := Supports(Codex, ch); got != supported[ch] {
			t.Errorf("Supports(Codex, %q) = %v, want %v", ch, got, supported[ch])
		}
	}
}

func TestDefaultIsClaudeCode(t *testing.T) {
	if Default != ClaudeCode {
		t.Errorf("Default = %q, want %q", Default, ClaudeCode)
	}
}
