// Package fsmerge provides helpers for writing managed regions into
// configuration files without disturbing content owned by other tools.
// It is intentionally standalone: it imports neither internal/provider nor
// internal/lockfile to avoid import cycles.
package fsmerge

import (
	"encoding/json"
	"errors"
	iofs "io/fs"
	"os"
	"path/filepath"
	"strings"
)

// FS is the file I/O surface fsmerge requires. Both provider.OSFilesystem and
// the in-memory fakes satisfy it structurally.
type FS interface {
	ReadFile(path string) ([]byte, error)
	WriteFile(path string, data []byte, perm os.FileMode) error
	MkdirAll(path string, perm os.FileMode) error
}

// MergeJSONKeys performs a three-way merge of a single JSON object key into a
// file at path. It reads the file (treating a missing file as {}), ensures the
// topKey object exists, removes every key listed in ownedKeys, sets every
// entry from desired, then writes the result back as indented JSON.
//
// Foreign keys — those present in the file but absent from ownedKeys — are
// preserved untouched.
func MergeJSONKeys(fs FS, path, topKey string, desired map[string]any, ownedKeys []string) error {
	raw, err := fs.ReadFile(path)
	if errors.Is(err, iofs.ErrNotExist) {
		raw = []byte("{}")
	} else if err != nil {
		return err
	}

	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		doc = map[string]any{}
	}

	top, ok := doc[topKey].(map[string]any)
	if !ok {
		top = map[string]any{}
	}

	owned := make(map[string]bool, len(ownedKeys))
	for _, k := range ownedKeys {
		owned[k] = true
	}

	for k := range owned {
		delete(top, k)
	}

	for k, v := range desired {
		top[k] = v
	}

	doc[topKey] = top

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}

	if err := fs.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return fs.WriteFile(path, out, 0o644)
}

// WriteOwnedFile creates parent directories as needed and writes content to
// path, replacing any existing file.
func WriteOwnedFile(fs FS, path string, content []byte) error {
	if err := fs.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return fs.WriteFile(path, content, 0o644)
}

// EnsureImportLine appends @importPath to the file at claudeMdPath if it is
// not already present. A missing file is treated as empty. The operation is
// idempotent.
func EnsureImportLine(fs FS, claudeMdPath, importPath string) error {
	raw, err := fs.ReadFile(claudeMdPath)
	if errors.Is(err, iofs.ErrNotExist) {
		raw = []byte{}
	} else if err != nil {
		return err
	}

	line := "@" + importPath
	content := string(raw)

	for _, l := range strings.Split(content, "\n") {
		if strings.TrimSpace(l) == line {
			return nil
		}
	}

	if len(content) > 0 && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	content += line + "\n"

	if err := fs.MkdirAll(filepath.Dir(claudeMdPath), 0o755); err != nil {
		return err
	}
	return fs.WriteFile(claudeMdPath, []byte(content), 0o644)
}
