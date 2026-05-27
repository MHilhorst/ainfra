package fetch

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// GitHubFetcher resolves github: and git+https://github.com sources to a
// pinned commit SHA via the GitHub API, then downloads and extracts the
// tarball.
type GitHubFetcher struct {
	// HTTPClient is the http client used for both metadata and tarball
	// downloads. Tests inject a transport here. Default is a 30s-timeout
	// client.
	HTTPClient *http.Client
	// Token, if non-empty, is sent as Authorization: Bearer <Token>.
	// Defaults to $GITHUB_TOKEN.
	Token string
	// Cache stores resolved bundles content-addressed by commit SHA.
	Cache *Cache
	// APIBase overrides the GitHub API base URL (default
	// "https://api.github.com"). Used by tests.
	APIBase string
}

func (g GitHubFetcher) client() *http.Client {
	if g.HTTPClient != nil {
		return g.HTTPClient
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (g GitHubFetcher) apiBase() string {
	if g.APIBase != "" {
		return strings.TrimRight(g.APIBase, "/")
	}
	return "https://api.github.com"
}

func (g GitHubFetcher) token() string {
	if g.Token != "" {
		return g.Token
	}
	return os.Getenv("GITHUB_TOKEN")
}

// githubRef holds the parsed pieces of a github: source.
type githubRef struct {
	Org     string
	Repo    string
	SubPath string
	Ref     string
}

// parseGitHubSource handles both forms:
//
//	github:org/repo[/sub/path]@ref
//	git+https://github.com/org/repo[/sub/path][?ref=X|#ref]
//
// If version is non-empty and the source has no embedded ref, version is used.
func parseGitHubSource(source, version string) (githubRef, error) {
	var rest, ref string

	switch {
	case strings.HasPrefix(source, "github:"):
		body := strings.TrimPrefix(source, "github:")
		if i := strings.LastIndex(body, "@"); i >= 0 {
			rest = body[:i]
			ref = body[i+1:]
		} else {
			rest = body
		}
	case strings.HasPrefix(source, "git+https://github.com/") || strings.HasPrefix(source, "git+http://github.com/"):
		raw := strings.TrimPrefix(strings.TrimPrefix(source, "git+https://"), "git+http://")
		// raw is now "github.com/org/repo[/sub]?ref=X"
		u, err := url.Parse("https://" + raw)
		if err != nil {
			return githubRef{}, fmt.Errorf("fetch: invalid github URL %q: %w", source, err)
		}
		rest = strings.TrimPrefix(u.Path, "/")
		if r := u.Query().Get("ref"); r != "" {
			ref = r
		}
		if ref == "" && u.Fragment != "" {
			ref = u.Fragment
		}
	default:
		return githubRef{}, fmt.Errorf("fetch: not a github source: %q", source)
	}

	if ref == "" {
		ref = version
	}
	if ref == "" {
		return githubRef{}, fmt.Errorf("fetch: github source %q has no ref (use @ref or pass version)", source)
	}

	parts := strings.SplitN(rest, "/", 3)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return githubRef{}, fmt.Errorf("fetch: github source %q must be org/repo[/sub]", source)
	}
	out := githubRef{Org: parts[0], Repo: parts[1], Ref: ref}
	if len(parts) == 3 {
		out.SubPath = strings.TrimRight(parts[2], "/")
	}
	// repo may have a trailing ".git" suffix when coming from git+https
	out.Repo = strings.TrimSuffix(out.Repo, ".git")
	return out, nil
}

type githubCommit struct {
	SHA string `json:"sha"`
}

// Fetch implements Fetcher.
func (g GitHubFetcher) Fetch(source, version string) (Bundle, error) {
	bundle, _, err := g.FetchResolved(source, version)
	return bundle, err
}

// FetchResolved returns the bundle and the pinning metadata (commit SHA,
// tarball URL) for the lockfile.
func (g GitHubFetcher) FetchResolved(source, version string) (Bundle, Resolved, error) {
	ref, err := parseGitHubSource(source, version)
	if err != nil {
		return nil, Resolved{}, err
	}

	sha, err := g.resolveSHA(ref)
	if err != nil {
		return nil, Resolved{}, err
	}
	tarballURL := fmt.Sprintf("%s/repos/%s/%s/tarball/%s", g.apiBase(), ref.Org, ref.Repo, sha)
	resolved := Resolved{
		Scheme:     "github",
		Address:    fmt.Sprintf("github.com/%s/%s@%s#%s", ref.Org, ref.Repo, sha, ref.SubPath),
		CommitSHA:  sha,
		TarballURL: tarballURL,
	}

	if b, ok := g.Cache.Get(resolved); ok {
		return b, resolved, nil
	}

	data, err := g.download(tarballURL)
	if err != nil {
		if isNetworkError(err) {
			return nil, resolved, fmt.Errorf("fetch: source not cached and network unreachable: %w", err)
		}
		return nil, resolved, err
	}
	bundle, err := extractTarAuto(data, ref.SubPath)
	if err != nil {
		return nil, resolved, err
	}
	if err := g.Cache.Put(resolved, bundle); err != nil {
		return bundle, resolved, fmt.Errorf("fetch: cache write: %w", err)
	}
	return bundle, resolved, nil
}

func (g GitHubFetcher) resolveSHA(ref githubRef) (string, error) {
	api := fmt.Sprintf("%s/repos/%s/%s/commits/%s", g.apiBase(), ref.Org, ref.Repo, ref.Ref)
	req, err := http.NewRequest("GET", api, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if tok := g.token(); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := g.client().Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch: github commit lookup: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return "", fmt.Errorf("fetch: github %s/%s@%s: not found", ref.Org, ref.Repo, ref.Ref)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("fetch: github %s/%s commit lookup: status %d", ref.Org, ref.Repo, resp.StatusCode)
	}
	var body githubCommit
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return "", fmt.Errorf("fetch: github commit decode: %w", err)
	}
	if body.SHA == "" {
		return "", fmt.Errorf("fetch: github %s/%s@%s: empty sha", ref.Org, ref.Repo, ref.Ref)
	}
	return body.SHA, nil
}

func (g GitHubFetcher) download(url string) ([]byte, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if tok := g.token(); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := g.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch: GET %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("fetch: GET %s: status %d", url, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxArtifactBytes))
}

// isNetworkError is a coarse check: any non-HTTP-status error in our download
// path is treated as network-unreachable for the offline error message.
func isNetworkError(err error) bool {
	if err == nil {
		return false
	}
	var ne interface{ Timeout() bool }
	if errors.As(err, &ne) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "dial") ||
		strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "network is unreachable") ||
		strings.Contains(msg, "transport error")
}
