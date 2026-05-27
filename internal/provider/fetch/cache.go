package fetch

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Resolved describes how a remote source was pinned at fetch time. It feeds
// the lockfile so subsequent resolves can reproduce the same bundle.
type Resolved struct {
	// Scheme is the source scheme: github, npm, https, local.
	Scheme string `json:"scheme,omitempty"`
	// Address is a canonical, content-addressable identifier for the source
	// (e.g. tarball URL plus commit SHA). It is part of the cache key.
	Address string `json:"address,omitempty"`
	// CommitSHA is set for github sources.
	CommitSHA string `json:"commit_sha,omitempty"`
	// TarballURL is set for github and npm sources.
	TarballURL string `json:"tarball_url,omitempty"`
	// Integrity is the npm dist.integrity value or sha256:<hex> for https.
	Integrity string `json:"integrity,omitempty"`
}

// CacheRoot returns the directory used for the content-addressed source cache.
// Honors $XDG_CACHE_HOME, falling back to $HOME/.cache.
func CacheRoot() (string, error) {
	if xdg := os.Getenv("XDG_CACHE_HOME"); xdg != "" {
		return filepath.Join(xdg, "ainfra", "sources"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("fetch: locate home directory: %w", err)
	}
	return filepath.Join(home, ".cache", "ainfra", "sources"), nil
}

// Cache is a content-addressed store for fetched bundles. The key is the
// sha256 of (scheme + "\x00" + address). Each entry is a directory containing
// a "contents" subtree and a "manifest.json" listing the files.
type Cache struct {
	Root string
}

// NewCache returns a Cache rooted at the conventional location.
func NewCache() (*Cache, error) {
	root, err := CacheRoot()
	if err != nil {
		return nil, err
	}
	return &Cache{Root: root}, nil
}

// Key derives the content-addressed cache key for a resolved source.
func (c *Cache) Key(r Resolved) string {
	h := sha256.New()
	h.Write([]byte(r.Scheme))
	h.Write([]byte{0})
	h.Write([]byte(r.Address))
	return hex.EncodeToString(h.Sum(nil))
}

type manifestEntry struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type manifest struct {
	Scheme   string          `json:"scheme,omitempty"`
	Address  string          `json:"address,omitempty"`
	Entries  []manifestEntry `json:"entries"`
	Resolved Resolved        `json:"resolved"`
}

// Get returns the cached Bundle for r, or (nil, false) if absent or corrupt.
func (c *Cache) Get(r Resolved) (Bundle, bool) {
	if c == nil || c.Root == "" {
		return nil, false
	}
	dir := filepath.Join(c.Root, c.Key(r))
	mfPath := filepath.Join(dir, "manifest.json")
	mfBytes, err := os.ReadFile(mfPath)
	if err != nil {
		return nil, false
	}
	var mf manifest
	if err := json.Unmarshal(mfBytes, &mf); err != nil {
		return nil, false
	}
	bundle := make(Bundle, len(mf.Entries))
	contents := filepath.Join(dir, "contents")
	for _, e := range mf.Entries {
		clean := filepath.Clean(e.Path)
		if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
			return nil, false
		}
		data, err := os.ReadFile(filepath.Join(contents, clean))
		if err != nil {
			return nil, false
		}
		got := sha256.Sum256(data)
		if hex.EncodeToString(got[:]) != e.SHA256 {
			return nil, false
		}
		bundle[clean] = data
	}
	return bundle, true
}

// Put writes the Bundle to the cache, keyed by r. It is best-effort: a write
// failure is returned but the caller can still use the in-memory bundle.
func (c *Cache) Put(r Resolved, b Bundle) error {
	if c == nil || c.Root == "" {
		return nil
	}
	dir := filepath.Join(c.Root, c.Key(r))
	contents := filepath.Join(dir, "contents")
	if err := os.MkdirAll(contents, 0o755); err != nil {
		return fmt.Errorf("fetch: cache mkdir: %w", err)
	}

	paths := make([]string, 0, len(b))
	for p := range b {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	mf := manifest{Scheme: r.Scheme, Address: r.Address, Resolved: r}
	for _, p := range paths {
		clean := filepath.Clean(p)
		if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
			return fmt.Errorf("fetch: cache refuses path %q (escapes cache root)", p)
		}
		dst := filepath.Join(contents, clean)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dst, b[p], 0o644); err != nil {
			return err
		}
		sum := sha256.Sum256(b[p])
		mf.Entries = append(mf.Entries, manifestEntry{Path: clean, SHA256: hex.EncodeToString(sum[:])})
	}

	mfBytes, err := json.MarshalIndent(mf, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "manifest.json"), mfBytes, 0o644)
}

// ensureDirReadable is a small helper for tests that want to verify the cache
// layout. It returns the list of file paths under root.
func ensureDirReadable(root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			rel, _ := filepath.Rel(root, p)
			out = append(out, rel)
		}
		return nil
	})
	return out, err
}
