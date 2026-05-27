package fetch

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"time"
)

// HTTPSFetcher resolves https:// and http:// sources. Tarball/zip responses
// are extracted; any other response is returned as a single-entry bundle
// keyed by the URL's basename.
type HTTPSFetcher struct {
	HTTPClient *http.Client
	Cache      *Cache
}

func (h HTTPSFetcher) client() *http.Client {
	if h.HTTPClient != nil {
		return h.HTTPClient
	}
	return &http.Client{Timeout: 30 * time.Second}
}

// Fetch implements Fetcher.
func (h HTTPSFetcher) Fetch(source, version string) (Bundle, error) {
	b, _, err := h.FetchResolved(source, version)
	return b, err
}

// FetchResolved downloads, optionally extracts, and content-hashes the
// response. Cache lookup happens after the SHA is known (we can't cache by
// URL alone because content may change).
func (h HTTPSFetcher) FetchResolved(source, version string) (Bundle, Resolved, error) {
	req, err := http.NewRequest("GET", source, nil)
	if err != nil {
		return nil, Resolved{}, err
	}
	resp, err := h.client().Do(req)
	if err != nil {
		if isNetworkError(err) {
			return nil, Resolved{}, fmt.Errorf("fetch: source not cached and network unreachable: %w", err)
		}
		return nil, Resolved{}, fmt.Errorf("fetch: GET %s: %w", source, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, Resolved{}, fmt.Errorf("fetch: GET %s: status %d", source, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxArtifactBytes))
	if err != nil {
		return nil, Resolved{}, err
	}

	sum := sha256.Sum256(data)
	hexSum := hex.EncodeToString(sum[:])
	effective := source
	if resp.Request != nil && resp.Request.URL != nil {
		effective = resp.Request.URL.String()
	}
	resolved := Resolved{
		Scheme:    "https",
		Address:   effective + "#sha256:" + hexSum,
		Integrity: "sha256:" + hexSum,
	}
	if b, ok := h.Cache.Get(resolved); ok {
		return b, resolved, nil
	}

	contentType := resp.Header.Get("Content-Type")
	bundle, err := h.decode(effective, contentType, data)
	if err != nil {
		return nil, resolved, err
	}
	if err := h.Cache.Put(resolved, bundle); err != nil {
		return bundle, resolved, fmt.Errorf("fetch: cache write: %w", err)
	}
	return bundle, resolved, nil
}

func (h HTTPSFetcher) decode(effectiveURL, contentType string, data []byte) (Bundle, error) {
	lowerURL := strings.ToLower(effectiveURL)
	ct := strings.ToLower(contentType)
	switch {
	case strings.HasSuffix(lowerURL, ".tar.gz"),
		strings.HasSuffix(lowerURL, ".tgz"),
		strings.Contains(ct, "application/gzip"),
		strings.Contains(ct, "application/x-gzip"),
		strings.Contains(ct, "application/x-tar"),
		strings.Contains(ct, "application/x-tgz"):
		return extractTarAuto(data, "")
	case strings.HasSuffix(lowerURL, ".zip"), strings.Contains(ct, "application/zip"):
		return extractZip(data, "")
	default:
		name := path.Base(effectiveURL)
		if i := strings.IndexAny(name, "?#"); i >= 0 {
			name = name[:i]
		}
		if name == "" || name == "." || name == "/" {
			name = "content"
		}
		return Bundle{name: data}, nil
	}
}
