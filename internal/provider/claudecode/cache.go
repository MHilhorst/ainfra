package claudecode

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	iofs "io/fs"
	"path/filepath"
	"sort"

	"github.com/MHilhorst/ainfra/internal/lockfile"
	"github.com/MHilhorst/ainfra/internal/provider"
)

// pluginCacheKeyDir is the per-plugin cache directory Claude Code maintains,
// e.g. ~/.claude/plugins/cache/<name>@<marketplace>. Each installed version of
// the plugin is a subdirectory of this path.
func pluginCacheKeyDir(env provider.Env, name, marketplace string) string {
	return filepath.Join(env.Home, ".claude", "plugins", "cache", name+"@"+marketplace)
}

// pluginCacheVersionDir returns the absolute path to the resolved version
// subdirectory inside the per-plugin cache. It picks the most recent live
// (non-orphan) version directory — the one with files in it. Orphan version
// directories left behind by Claude Code's 7-day GC window are skipped if
// possible by preferring the lexicographically largest entry, which for semver
// strings approximates "newest". When only one entry exists it is returned.
//
// Returns "" with a nil error when the cache key directory does not exist
// (plugin not installed yet, fresh checkout, dry-run, etc.).
func pluginCacheVersionDir(env provider.Env, name, marketplace string) (string, error) {
	root := pluginCacheKeyDir(env, name, marketplace)
	entries, err := env.FS.ReadDir(root)
	if errors.Is(err, iofs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if len(entries) == 0 {
		return "", nil
	}
	sort.Strings(entries)
	// Prefer the lexicographically largest entry; for semver tags this is the
	// newest version, and for SHAs it is deterministic across runs.
	return filepath.Join(root, entries[len(entries)-1]), nil
}

// readResolvedPluginVersion returns the version Claude Code resolved this
// plugin to, by reading its .claude-plugin/plugin.json inside the cache. An
// empty string is returned when the manifest is absent or has no version field
// (the SHA-versioned case); the caller treats that as "no enforceable version
// to compare against".
func readResolvedPluginVersion(env provider.Env, name, marketplace string) (string, error) {
	versionDir, err := pluginCacheVersionDir(env, name, marketplace)
	if err != nil || versionDir == "" {
		return "", err
	}
	manifestPath := filepath.Join(versionDir, ".claude-plugin", "plugin.json")
	raw, err := env.FS.ReadFile(manifestPath)
	if errors.Is(err, iofs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	var doc struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		// A malformed manifest is not our problem to surface here — fall through
		// with "no resolvable version" rather than failing apply.
		return "", nil
	}
	return doc.Version, nil
}

// hashPluginCacheDir computes a content hash over the resolved version
// directory of an installed plugin. It walks every file recursively, records
// (relative-path, sha256-of-bytes) pairs, sorts them for determinism, and
// returns lockfile.ContentHash over the sorted list.
//
// Returns ("", nil) when the cache directory does not exist (e.g. dry-run, or
// the install failed silently). The orchestrator's existing ledger backfill
// then keeps prior ContentHash in place.
func hashPluginCacheDir(env provider.Env, name, marketplace string) (string, error) {
	versionDir, err := pluginCacheVersionDir(env, name, marketplace)
	if err != nil || versionDir == "" {
		return "", err
	}
	entries, err := walkFiles(env.FS, versionDir)
	if err != nil {
		return "", err
	}
	if len(entries) == 0 {
		return "", nil
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].rel < entries[j].rel })
	pairs := make([][2]string, 0, len(entries))
	for _, e := range entries {
		pairs = append(pairs, [2]string{e.rel, e.sum})
	}
	return lockfile.ContentHash(pairs), nil
}

type walkedFile struct {
	rel string
	sum string
}

// walkFiles returns one walkedFile per regular file under root, with rel
// relative to root and sum the hex sha256 of the file bytes. It uses only the
// Filesystem interface so MemFilesystem-backed tests work.
func walkFiles(fs provider.Filesystem, root string) ([]walkedFile, error) {
	var out []walkedFile
	var visit func(dir, rel string) error
	visit = func(dir, rel string) error {
		entries, err := fs.ReadDir(dir)
		if errors.Is(err, iofs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		for _, name := range entries {
			child := filepath.Join(dir, name)
			info, err := fs.Stat(child)
			if err != nil {
				return err
			}
			childRel := name
			if rel != "" {
				childRel = filepath.Join(rel, name)
			}
			if info.IsDir() {
				if err := visit(child, childRel); err != nil {
					return err
				}
				continue
			}
			raw, err := fs.ReadFile(child)
			if err != nil {
				return err
			}
			sum := sha256.Sum256(raw)
			out = append(out, walkedFile{rel: childRel, sum: hex.EncodeToString(sum[:])})
		}
		return nil
	}
	if err := visit(root, ""); err != nil {
		return nil, err
	}
	return out, nil
}
