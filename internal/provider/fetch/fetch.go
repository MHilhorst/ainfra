// Package fetch retrieves channel-entry bundles (skills, plugins) from their
// declared sources.
package fetch

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Bundle is a fetched set of files: relative path -> content.
type Bundle map[string][]byte

// Fetcher retrieves the bundle for a source reference at a pinned version.
type Fetcher interface {
	Fetch(source, version string) (Bundle, error)
}

// LocalFetcher fetches bundles from the local filesystem. Root is the base
// directory against which relative sources are resolved.
type LocalFetcher struct {
	Root string
}

// Fetch returns the contents of every regular file under the source directory,
// keyed by path relative to that directory. Remote-scheme sources (git+, npm:)
// return a clear unsupported error.
func (l LocalFetcher) Fetch(source, version string) (Bundle, error) {
	if strings.HasPrefix(source, "git+") || strings.HasPrefix(source, "npm:") {
		return nil, fmt.Errorf("fetch: remote source %q is not supported in this build; use a local path", source)
	}

	resolved := source
	if !filepath.IsAbs(source) {
		resolved = filepath.Join(l.Root, source)
	}

	bundle := make(Bundle)
	err := filepath.WalkDir(resolved, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(resolved, path)
		if err != nil {
			return err
		}
		bundle[rel] = data
		return nil
	})
	if err != nil {
		return nil, err
	}
	return bundle, nil
}

// FakeFetcher is a test double. If Err is non-nil, Fetch returns it. Otherwise
// Fetch returns the bundle stored under source in Bundles, or an error if the
// source is absent.
type FakeFetcher struct {
	Bundles map[string]Bundle
	Err     error
}

// Fetch implements Fetcher.
func (f FakeFetcher) Fetch(source, version string) (Bundle, error) {
	if f.Err != nil {
		return nil, f.Err
	}
	b, ok := f.Bundles[source]
	if !ok {
		return nil, fmt.Errorf("fetch: FakeFetcher has no bundle for source %q", source)
	}
	return b, nil
}
