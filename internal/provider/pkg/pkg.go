// Package pkg provides package-manager adapters that install CLI tools.
package pkg

import (
	"fmt"

	"github.com/MHilhorst/ainfra/internal/provider"
)

// Adapter installs and probes a CLI tool through one package manager.
type Adapter interface {
	Name() string
	IsInstalled(env provider.Env, spec map[string]any) (bool, error)
	Install(env provider.Env, spec map[string]any) error
}

// brewSpec derives the package name and cask flag from a brew install spec.
// The spec must have either a "cask" key or a "formula" key.
func brewSpec(spec map[string]any) (name string, cask bool, err error) {
	if v, ok := spec["cask"]; ok {
		s, ok := v.(string)
		if !ok || s == "" {
			return "", false, fmt.Errorf("brew spec: cask value must be a non-empty string")
		}
		return s, true, nil
	}
	if v, ok := spec["formula"]; ok {
		s, ok := v.(string)
		if !ok || s == "" {
			return "", false, fmt.Errorf("brew spec: formula value must be a non-empty string")
		}
		return s, false, nil
	}
	return "", false, fmt.Errorf("brew spec: must have a formula or cask key")
}

// BrewAdapter installs CLI tools via Homebrew.
type BrewAdapter struct{}

// Name returns the adapter identifier.
func (BrewAdapter) Name() string { return "brew" }

// IsInstalled reports whether the formula or cask is installed by running brew list.
func (BrewAdapter) IsInstalled(env provider.Env, spec map[string]any) (bool, error) {
	name, cask, err := brewSpec(spec)
	if err != nil {
		return false, err
	}
	var runErr error
	if cask {
		_, runErr = env.Runner.Run("brew", "list", "--cask", "--versions", name)
	} else {
		_, runErr = env.Runner.Run("brew", "list", "--versions", name)
	}
	return runErr == nil, nil
}

// Install installs the formula or cask via brew install.
func (BrewAdapter) Install(env provider.Env, spec map[string]any) error {
	name, cask, err := brewSpec(spec)
	if err != nil {
		return err
	}
	if cask {
		_, err = env.Runner.Run("brew", "install", "--cask", name)
	} else {
		_, err = env.Runner.Run("brew", "install", name)
	}
	return err
}

// NpmAdapter installs CLI tools via npm -g.
type NpmAdapter struct{}

// Name returns the adapter identifier.
func (NpmAdapter) Name() string { return "npm" }

// npmSpec derives the package name and optional version from a npm install spec.
func npmSpec(spec map[string]any) (pkg string, version string, err error) {
	v, ok := spec["package"]
	if !ok {
		return "", "", fmt.Errorf("npm spec: must have a package key")
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return "", "", fmt.Errorf("npm spec: package value must be a non-empty string")
	}
	if ver, ok := spec["version"]; ok {
		if vs, ok := ver.(string); ok {
			version = vs
		}
	}
	return s, version, nil
}

// IsInstalled reports whether the package is installed globally.
func (NpmAdapter) IsInstalled(env provider.Env, spec map[string]any) (bool, error) {
	name, _, err := npmSpec(spec)
	if err != nil {
		return false, err
	}
	_, runErr := env.Runner.Run("npm", "ls", "-g", "--depth", "0", name)
	return runErr == nil, nil
}

// Install installs the package globally via npm install -g.
func (NpmAdapter) Install(env provider.Env, spec map[string]any) error {
	name, version, err := npmSpec(spec)
	if err != nil {
		return err
	}
	target := name
	if version != "" {
		target = name + "@" + version
	}
	_, err = env.Runner.Run("npm", "install", "-g", target)
	return err
}

// ComposerAdapter installs CLI tools via `composer global require`.
type ComposerAdapter struct{}

// Name returns the adapter identifier.
func (ComposerAdapter) Name() string { return "composer" }

// composerSpec derives the package name and optional version from a composer
// install spec.
func composerSpec(spec map[string]any) (pkg string, version string, err error) {
	v, ok := spec["package"]
	if !ok {
		return "", "", fmt.Errorf("composer spec: must have a package key")
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return "", "", fmt.Errorf("composer spec: package value must be a non-empty string")
	}
	if ver, ok := spec["version"]; ok {
		if vs, ok := ver.(string); ok {
			version = vs
		}
	}
	return s, version, nil
}

// IsInstalled reports whether the package is installed in composer's global
// environment.
func (ComposerAdapter) IsInstalled(env provider.Env, spec map[string]any) (bool, error) {
	name, _, err := composerSpec(spec)
	if err != nil {
		return false, err
	}
	_, runErr := env.Runner.Run("composer", "global", "show", name)
	return runErr == nil, nil
}

// Install installs the package globally via `composer global require`. composer
// joins a version constraint to the package with a colon (vendor/pkg:^1.0).
func (ComposerAdapter) Install(env provider.Env, spec map[string]any) error {
	name, version, err := composerSpec(spec)
	if err != nil {
		return err
	}
	target := name
	if version != "" {
		target = name + ":" + version
	}
	_, err = env.Runner.Run("composer", "global", "require", target)
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
	case "composer":
		return ComposerAdapter{}, true
	default:
		return nil, false
	}
}
