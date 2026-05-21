package main

import (
	"os"

	"github.com/MHilhorst/ainfra/internal/agent"
	"github.com/MHilhorst/ainfra/internal/manifest"
	"github.com/MHilhorst/ainfra/internal/provider"
	"github.com/MHilhorst/ainfra/internal/provider/agentset"
	"github.com/MHilhorst/ainfra/internal/provider/fetch"
)

// providersForDir resolves the target agent from the manifest layers at dir
// and returns the channel provider set that reconciles config for that agent.
func providersForDir(dir string) ([]provider.Provider, error) {
	layers, err := manifest.LoadLayers(dir)
	if err != nil {
		return nil, err
	}
	id, _, _ := manifest.ResolveAgent(layers)
	return agentset.ForAgent(agent.ID(id))
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
