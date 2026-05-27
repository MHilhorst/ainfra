package claudecode

import (
	"path/filepath"

	"github.com/MHilhorst/ainfra/internal/provider"
)

// PluginDataID returns the sanitized directory name Claude Code uses for a
// plugin's ${CLAUDE_PLUGIN_DATA} root. Per the plugins reference, characters
// outside [A-Za-z0-9_-] are replaced by '-'. The input is the full plugin
// identifier including marketplace, e.g. "formatter@my-marketplace".
//
// Example: "formatter@my-marketplace" -> "formatter-my-marketplace".
func PluginDataID(key string) string {
	out := make([]byte, 0, len(key))
	for i := 0; i < len(key); i++ {
		c := key[i]
		switch {
		case c >= 'a' && c <= 'z',
			c >= 'A' && c <= 'Z',
			c >= '0' && c <= '9',
			c == '_', c == '-':
			out = append(out, c)
		default:
			out = append(out, '-')
		}
	}
	return string(out)
}

// PluginDataDir returns Claude Code's persistent data directory for the given
// "name@marketplace" plugin key under env.Home. It is the path
// ${CLAUDE_PLUGIN_DATA} expands to at runtime.
func PluginDataDir(env provider.Env, key string) string {
	return filepath.Join(env.Home, ".claude", "plugins", "data", PluginDataID(key))
}
