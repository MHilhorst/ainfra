// Package xdg resolves user-scope paths for ainfra following the XDG Base
// Directory specification. Centralizing the resolution here keeps the
// personal-manifest loader (internal/manifest) and the user-scope applied
// ledger (internal/provider) in agreement about where files live.
package xdg

import (
	"os"
	"path/filepath"
)

// ConfigHome returns the directory ainfra uses for user-scope config files
// (the personal manifest, the user-scope applied ledger). Honors
// XDG_CONFIG_HOME if set; otherwise falls back to ~/.config/ainfra/.
func ConfigHome() (string, error) {
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return filepath.Join(v, "ainfra"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "ainfra"), nil
}

// PersonalManifestPath returns the path of the user-scope personal manifest.
func PersonalManifestPath() (string, error) {
	dir, err := ConfigHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "personal.yaml"), nil
}

// AppliedLedgerPath returns the path of the user-scope applied ledger.
// Sits alongside the personal manifest for discoverability — one user-scope
// directory holds both the "what you want" (personal.yaml) and the
// "what's installed" (applied.lock).
func AppliedLedgerPath() (string, error) {
	dir, err := ConfigHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "applied.lock"), nil
}
