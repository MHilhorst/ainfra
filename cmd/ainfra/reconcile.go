package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/MHilhorst/ainfra/internal/agent"
	"github.com/MHilhorst/ainfra/internal/artifact"
	"github.com/MHilhorst/ainfra/internal/lockfile"
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

// artifactSource resolves a --from value to a verified local artifact
// directory. A local path is verified in place. An http(s) URL has its files
// downloaded into a temp directory, then verified. The returned dir has passed
// artifact.Verify; the caller must call cleanup when done.
func artifactSource(from string) (dir string, cleanup func(), err error) {
	noop := func() {}
	if !strings.HasPrefix(from, "http://") && !strings.HasPrefix(from, "https://") {
		if err := artifact.Verify(from); err != nil {
			return "", noop, err
		}
		return from, noop, nil
	}
	tmp, err := os.MkdirTemp("", "ainfra-artifact-")
	if err != nil {
		return "", noop, err
	}
	cleanup = func() { os.RemoveAll(tmp) }
	for _, name := range []string{artifact.ManifestName, artifact.DescriptorName, "ainfra.lock", "rendered.json"} {
		body, err := fetch.FetchURL(strings.TrimRight(from, "/") + "/" + name)
		if err != nil {
			cleanup()
			return "", noop, err
		}
		if err := os.WriteFile(filepath.Join(tmp, name), body, 0o644); err != nil {
			cleanup()
			return "", noop, err
		}
	}
	if err := artifact.Verify(tmp); err != nil {
		cleanup()
		return "", noop, err
	}
	return tmp, cleanup, nil
}

// loadArtifact reads a verified artifact directory into the pieces a
// subscriber reconcile needs: the declared agent's provider set, the rendered
// resources (which carry Payload), and the lockfile (for the applied ledger).
func loadArtifact(dir string) (providers []provider.Provider, rendered map[string][]provider.Resource, lock *lockfile.Lock, err error) {
	desc, err := artifact.ReadDescriptor(dir)
	if err != nil {
		return nil, nil, nil, err
	}
	providers, err = agentset.ForAgent(agent.ID(desc.Agent))
	if err != nil {
		return nil, nil, nil, err
	}
	raw, err := os.ReadFile(filepath.Join(dir, "rendered.json"))
	if err != nil {
		return nil, nil, nil, err
	}
	if err := json.Unmarshal(raw, &rendered); err != nil {
		return nil, nil, nil, err
	}
	lock, err = lockfile.Read(filepath.Join(dir, "ainfra.lock"))
	if err != nil {
		return nil, nil, nil, err
	}
	return providers, rendered, lock, nil
}

// subscriberEnv builds the provider.Env for a subscriber reconcile, rooted at
// the user's home directory.
func subscriberEnv() (provider.Env, string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return provider.Env{}, "", err
	}
	return provider.Env{
		FS:     provider.OSFilesystem{},
		Runner: provider.ExecRunner{},
		Fetch:  fetch.LocalFetcher{Root: home},
		Root:   home,
		Home:   home,
	}, home, nil
}
