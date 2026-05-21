package secret

import "strings"

// PlaceholderVar returns the environment variable name ainfra renders into
// native config for a secret. channel is the owning channel ("mcpServers",
// "cliTools"), owner is the entry id, name is the secret's logical name. The
// derivation is deterministic so content hashes stay stable across runs.
func PlaceholderVar(channel, owner, name string) string {
	return "AINFRA_SECRET_" + sanitize(channel) + "_" + sanitize(owner) + "_" + sanitize(name)
}

// Placeholder returns the ${VAR} token rendered into config files.
func Placeholder(channel, owner, name string) string {
	return "${" + PlaceholderVar(channel, owner, name) + "}"
}

// sanitize uppercases s and replaces every non-alphanumeric rune with "_".
func sanitize(s string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(s) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	return b.String()
}
