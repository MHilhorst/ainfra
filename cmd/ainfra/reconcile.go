package main

import (
	"os"

	"github.com/MHilhorst/ainfra/internal/provider"
	"github.com/MHilhorst/ainfra/internal/provider/claudecode"
	"github.com/MHilhorst/ainfra/internal/provider/fetch"
	"github.com/MHilhorst/ainfra/internal/provider/shared"
)

// allProviders returns all nine channel providers in a stable order.
func allProviders() []provider.Provider {
	return []provider.Provider{
		claudecode.MCP{},
		claudecode.Hooks{},
		claudecode.Commands{},
		claudecode.Rules{},
		claudecode.Skills{},
		claudecode.Plugins{},
		shared.CLITools{},
		claudecode.Services{},
		claudecode.Tools{},
	}
}

// buildEnv constructs the provider.Env for a given repo root directory.
func buildEnv(dir string) provider.Env {
	home, _ := os.UserHomeDir()
	return provider.Env{
		FS:     provider.OSFilesystem{},
		Runner: provider.ExecRunner{},
		Fetch:  fetch.LocalFetcher{Root: dir},
		Root:   dir,
		Home:   home,
	}
}
