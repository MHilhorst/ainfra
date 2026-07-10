package fetch

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"
)

// GitHubReleaseFetcher downloads release assets from GitHub Releases.
// It resolves a release tag (or "latest") and matches an asset by name pattern.
type GitHubReleaseFetcher struct {
	HTTPClient *http.Client
	Token      string
	APIBase    string
}

func (g GitHubReleaseFetcher) client() *http.Client {
	if g.HTTPClient != nil {
		return g.HTTPClient
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (g GitHubReleaseFetcher) apiBase() string {
	if g.APIBase != "" {
		return strings.TrimRight(g.APIBase, "/")
	}
	return "https://api.github.com"
}

func (g GitHubReleaseFetcher) token() string {
	if g.Token != "" {
		return g.Token
	}
	return os.Getenv("GITHUB_TOKEN")
}

type releaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type release struct {
	Assets []releaseAsset `json:"assets"`
}

// ResolveAssetURL finds the release and returns the download URL for the
// asset matching assetName. tag can be "" or "latest" to fetch the latest
// release; otherwise it fetches /releases/tags/{tag}.
func (g GitHubReleaseFetcher) ResolveAssetURL(owner, repo, tag, assetName string) (string, error) {
	if owner == "" || repo == "" || assetName == "" {
		return "", fmt.Errorf("fetch: github-release requires owner, repo, and assetName")
	}

	var endpoint string
	if tag == "" || tag == "latest" {
		endpoint = fmt.Sprintf("%s/repos/%s/%s/releases/latest", g.apiBase(), owner, repo)
	} else {
		endpoint = fmt.Sprintf("%s/repos/%s/%s/releases/tags/%s", g.apiBase(), owner, repo, tag)
	}

	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if tok := g.token(); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}

	resp, err := g.client().Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch: github release lookup: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		return "", fmt.Errorf("fetch: github %s/%s@%s: not found", owner, repo, tag)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("fetch: github %s/%s release lookup: status %d", owner, repo, resp.StatusCode)
	}

	var rel release
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&rel); err != nil {
		return "", fmt.Errorf("fetch: github release decode: %w", err)
	}

	// Find asset by exact name match
	for _, asset := range rel.Assets {
		if asset.Name == assetName {
			return asset.BrowserDownloadURL, nil
		}
	}

	// Asset not found; list available names for debugging
	available := make([]string, len(rel.Assets))
	for i, asset := range rel.Assets {
		available[i] = asset.Name
	}
	return "", fmt.Errorf("fetch: asset %q not found in %s/%s@%s; available: %v",
		assetName, owner, repo, tag, available)
}

// Download fetches the asset from the given URL and returns its contents,
// capped at maxArtifactBytes.
func (g GitHubReleaseFetcher) Download(url string) ([]byte, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	if tok := g.token(); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}

	resp, err := g.client().Do(req)
	if err != nil {
		if isNetworkError(err) {
			return nil, fmt.Errorf("fetch: network unreachable: %w", err)
		}
		return nil, fmt.Errorf("fetch: GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("fetch: GET %s: status %d", url, resp.StatusCode)
	}

	return io.ReadAll(io.LimitReader(resp.Body, maxArtifactBytes))
}

// SubstituteAssetPattern replaces {os} with GOOS and {arch} with GOARCH.
func SubstituteAssetPattern(pattern string) string {
	pattern = strings.ReplaceAll(pattern, "{os}", runtime.GOOS)
	pattern = strings.ReplaceAll(pattern, "{arch}", runtime.GOARCH)
	return pattern
}
