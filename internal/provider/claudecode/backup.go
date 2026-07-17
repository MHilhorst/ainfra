package claudecode

import (
	"path/filepath"

	"github.com/MHilhorst/ainfra/internal/provider"
)

// copyTree recursively copies src to dst using env.FS. The copy is recursive
// because a skill bundle may contain nested paths, and a flat copy would
// silently drop files from the backup of a directory that is about to be
// removed.
func copyTree(env provider.Env, src, dst string) error {
	names, err := env.FS.ReadDir(src)
	if err != nil {
		return err
	}
	if err := env.FS.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	for _, name := range names {
		s := filepath.Join(src, name)
		d := filepath.Join(dst, name)
		info, err := env.FS.Stat(s)
		if err != nil {
			return err
		}
		if info.IsDir() {
			if err := copyTree(env, s, d); err != nil {
				return err
			}
			continue
		}
		if err := copyFile(env, s, d); err != nil {
			return err
		}
	}
	return nil
}

// copyFile copies a single file, creating the destination directory.
func copyFile(env provider.Env, src, dst string) error {
	data, err := env.FS.ReadFile(src)
	if err != nil {
		return err
	}
	if err := env.FS.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return env.FS.WriteFile(dst, data, 0o644)
}
