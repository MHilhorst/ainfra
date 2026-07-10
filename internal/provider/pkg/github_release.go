package pkg

import (
	"fmt"
	"path/filepath"

	"github.com/MHilhorst/ainfra/internal/provider"
	"github.com/MHilhorst/ainfra/internal/provider/fetch"
)

// githubReleaseFetcher is the interface for fetching GitHub release assets.
type githubReleaseFetcher interface {
	ResolveAssetURL(owner, repo, tag, assetName string) (string, error)
	Download(url string) ([]byte, error)
}

// GitHubReleaseAdapter installs CLI tools from GitHub release assets.
type GitHubReleaseAdapter struct {
	Fetcher githubReleaseFetcher
}

// Name returns the adapter identifier.
func (GitHubReleaseAdapter) Name() string { return "github-release" }

// fetcher returns the effective fetcher (or a default production one if nil).
func (a GitHubReleaseAdapter) fetcher() githubReleaseFetcher {
	if a.Fetcher != nil {
		return a.Fetcher
	}
	return fetch.GitHubReleaseFetcher{}
}

// githubReleaseSpec parses the spec and extracts owner, repo, assetPattern,
// tag (optional, defaults to "latest"), and binary (optional, defaults to repo).
func githubReleaseSpec(spec map[string]any) (owner, repo, assetPattern, tag, binary string, err error) {
	// owner (required)
	if v, ok := spec["owner"]; ok {
		if s, ok := v.(string); ok && s != "" {
			owner = s
		}
	}
	if owner == "" {
		return "", "", "", "", "", fmt.Errorf("github-release spec: must have owner key")
	}

	// repo (required)
	if v, ok := spec["repo"]; ok {
		if s, ok := v.(string); ok && s != "" {
			repo = s
		}
	}
	if repo == "" {
		return "", "", "", "", "", fmt.Errorf("github-release spec: must have repo key")
	}

	// assetPattern (required)
	if v, ok := spec["assetPattern"]; ok {
		if s, ok := v.(string); ok && s != "" {
			assetPattern = s
		}
	}
	if assetPattern == "" {
		return "", "", "", "", "", fmt.Errorf("github-release spec: must have assetPattern key")
	}

	// tag (optional, defaults to "latest")
	tag = "latest"
	if v, ok := spec["tag"]; ok {
		if s, ok := v.(string); ok && s != "" {
			tag = s
		}
	}

	// binary (optional, defaults to repo name)
	binary = repo
	if v, ok := spec["binary"]; ok {
		if s, ok := v.(string); ok && s != "" {
			binary = s
		}
	}

	return owner, repo, assetPattern, tag, binary, nil
}

// binPath returns the path where the binary will be installed.
func binPath(env provider.Env, binary string) string {
	return filepath.Join(env.Home, ".local", "bin", binary)
}

// IsInstalled checks whether the binary exists.
func (a GitHubReleaseAdapter) IsInstalled(env provider.Env, spec map[string]any) (bool, error) {
	_, _, _, _, binary, err := githubReleaseSpec(spec)
	if err != nil {
		return false, err
	}

	path := binPath(env, binary)
	_, err = env.FS.Stat(path)
	return err == nil, nil
}

// Install downloads the release asset and places it in ~/.local/bin with executable perms.
func (a GitHubReleaseAdapter) Install(env provider.Env, spec map[string]any) error {
	owner, repo, assetPattern, tag, binary, err := githubReleaseSpec(spec)
	if err != nil {
		return err
	}

	// Substitute {os} and {arch} in the pattern
	assetName := fetch.SubstituteAssetPattern(assetPattern)

	// Resolve the asset download URL
	url, err := a.fetcher().ResolveAssetURL(owner, repo, tag, assetName)
	if err != nil {
		return fmt.Errorf("install via github-release failed: %w", err)
	}

	// Download the asset
	data, err := a.fetcher().Download(url)
	if err != nil {
		return fmt.Errorf("install via github-release failed: %w", err)
	}

	// Prepare the installation path
	path := binPath(env, binary)
	dir := filepath.Dir(path)

	// Create the directory if needed
	if err := env.FS.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("install via github-release failed: mkdir %s: %w", dir, err)
	}

	// Write the binary with executable permissions
	if err := env.FS.WriteFile(path, data, 0o755); err != nil {
		return fmt.Errorf("install via github-release failed: write %s: %w", path, err)
	}

	return nil
}
