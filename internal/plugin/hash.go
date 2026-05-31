package plugin

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	iofs "io/fs"
	"os"
	"path/filepath"
	"sort"
)

// ContentHash returns a deterministic hash over the files under the given
// paths (relative to root). Order-independent; a missing path is skipped. A
// single-file path (e.g. ".mcp.json") hashes that file.
func ContentHash(root string, paths []string) (string, error) {
	type entry struct {
		rel string
		sum [32]byte
	}
	var entries []entry

	for _, p := range paths {
		abs := filepath.Join(root, p)
		err := filepath.WalkDir(abs, func(path string, d iofs.DirEntry, err error) error {
			if err != nil {
				if errors.Is(err, iofs.ErrNotExist) {
					return nil
				}
				return err
			}
			if d.IsDir() {
				return nil
			}
			data, rerr := os.ReadFile(path)
			if rerr != nil {
				return rerr
			}
			rel, rerr := filepath.Rel(root, path)
			if rerr != nil {
				return rerr
			}
			entries = append(entries, entry{rel: filepath.ToSlash(rel), sum: sha256.Sum256(data)})
			return nil
		})
		if err != nil {
			return "", err
		}
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].rel < entries[j].rel })

	h := sha256.New()
	for _, e := range entries {
		io.WriteString(h, e.rel)
		h.Write([]byte{0})
		h.Write(e.sum[:])
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
