// Package pkg provides package-manager adapters that install CLI tools.
package pkg

import "github.com/MHilhorst/ainfra/internal/provider"

// Adapter installs and probes a CLI tool through one package manager.
type Adapter interface {
	Name() string
	IsInstalled(env provider.Env, tool string) (bool, error)
	Install(env provider.Env, tool string) error
}

// BrewAdapter installs CLI tools via Homebrew.
type BrewAdapter struct{}

// Name returns the adapter identifier.
func (BrewAdapter) Name() string { return "brew" }

// IsInstalled reports whether the formula is installed by running brew list.
func (BrewAdapter) IsInstalled(env provider.Env, tool string) (bool, error) {
	_, err := env.Runner.Run("brew", "list", "--versions", tool)
	return err == nil, nil
}

// Install installs the formula via brew install.
func (BrewAdapter) Install(env provider.Env, tool string) error {
	_, err := env.Runner.Run("brew", "install", tool)
	return err
}

// NpmAdapter installs CLI tools via npm -g.
type NpmAdapter struct{}

// Name returns the adapter identifier.
func (NpmAdapter) Name() string { return "npm" }

// IsInstalled reports whether the package is installed globally.
func (NpmAdapter) IsInstalled(env provider.Env, tool string) (bool, error) {
	_, err := env.Runner.Run("npm", "ls", "-g", "--depth", "0", tool)
	return err == nil, nil
}

// Install installs the package globally via npm install -g.
func (NpmAdapter) Install(env provider.Env, tool string) error {
	_, err := env.Runner.Run("npm", "install", "-g", tool)
	return err
}

// Select returns the Adapter for the given install method, or nil and false if
// the method is not recognised.
func Select(method string) (Adapter, bool) {
	switch method {
	case "brew":
		return BrewAdapter{}, true
	case "npm", "npm-g":
		return NpmAdapter{}, true
	default:
		return nil, false
	}
}
