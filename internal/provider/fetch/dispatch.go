package fetch

import (
	"fmt"
	"strings"
)

// MultiSchemeFetcher dispatches Fetch calls to the appropriate scheme-specific
// fetcher. Local paths (no scheme, or "local:") go to Local; "github:" and
// "git+https://github.com/..." go to GitHub; "npm:" goes to NPM; "http(s)://"
// goes to HTTPS.
type MultiSchemeFetcher struct {
	Local  LocalFetcher
	GitHub GitHubFetcher
	NPM    NPMFetcher
	HTTPS  HTTPSFetcher
}

// NewMultiSchemeFetcher wires a default dispatcher rooted at localRoot. The
// cache is shared across the remote fetchers.
func NewMultiSchemeFetcher(localRoot string, cache *Cache) MultiSchemeFetcher {
	return MultiSchemeFetcher{
		Local:  LocalFetcher{Root: localRoot},
		GitHub: GitHubFetcher{Cache: cache},
		NPM:    NPMFetcher{Cache: cache},
		HTTPS:  HTTPSFetcher{Cache: cache},
	}
}

// SchemeOf returns a short label for the scheme of source.
func SchemeOf(source string) string {
	switch {
	case strings.HasPrefix(source, "github:"),
		strings.HasPrefix(source, "git+https://github.com/"),
		strings.HasPrefix(source, "git+http://github.com/"):
		return "github"
	case strings.HasPrefix(source, "npm:"):
		return "npm"
	case strings.HasPrefix(source, "https://"), strings.HasPrefix(source, "http://"):
		return "https"
	case strings.HasPrefix(source, "local:"):
		return "local"
	default:
		return "local"
	}
}

// Fetch implements Fetcher by dispatching on the source scheme.
func (m MultiSchemeFetcher) Fetch(source, version string) (Bundle, error) {
	b, _, err := m.FetchResolved(source, version)
	return b, err
}

// FetchResolved dispatches and returns the pinning metadata. Local sources
// have an empty Resolved.
func (m MultiSchemeFetcher) FetchResolved(source, version string) (Bundle, Resolved, error) {
	switch SchemeOf(source) {
	case "github":
		return m.GitHub.FetchResolved(source, version)
	case "npm":
		return m.NPM.FetchResolved(source, version)
	case "https":
		return m.HTTPS.FetchResolved(source, version)
	case "local":
		src := strings.TrimPrefix(source, "local:")
		b, err := m.Local.Fetch(src, version)
		return b, Resolved{Scheme: "local", Address: src}, err
	default:
		return nil, Resolved{}, fmt.Errorf("fetch: unknown scheme for source %q", source)
	}
}
