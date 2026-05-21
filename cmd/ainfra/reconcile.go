package main

import (
	"os"

	"github.com/MHilhorst/ainfra/internal/provider"
	"github.com/MHilhorst/ainfra/internal/provider/channels"
	"github.com/MHilhorst/ainfra/internal/provider/fetch"
	"github.com/MHilhorst/ainfra/internal/provider/shared"
)

// allProviders returns all nine channel providers in a stable order.
func allProviders() []provider.Provider {
	return []provider.Provider{
		channels.MCP{},
		channels.Hooks{},
		channels.Commands{},
		channels.Rules{},
		channels.Skills{},
		channels.Plugins{},
		shared.CLITools{},
		channels.Services{},
		channels.Tools{},
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
