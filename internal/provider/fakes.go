package provider

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// MemFilesystem is an in-memory Filesystem for tests.
type MemFilesystem struct {
	Files map[string][]byte
	Dirs  map[string]bool
}

// NewMemFilesystem returns an empty in-memory filesystem.
func NewMemFilesystem() *MemFilesystem {
	return &MemFilesystem{Files: map[string][]byte{}, Dirs: map[string]bool{}}
}

func (m *MemFilesystem) ReadFile(p string) ([]byte, error) {
	d, ok := m.Files[p]
	if !ok {
		return nil, &os.PathError{Op: "open", Path: p, Err: os.ErrNotExist}
	}
	return append([]byte(nil), d...), nil
}

func (m *MemFilesystem) WriteFile(p string, d []byte, _ os.FileMode) error {
	m.Files[p] = append([]byte(nil), d...)
	return nil
}

func (m *MemFilesystem) Remove(p string) error {
	if _, ok := m.Files[p]; !ok {
		return &os.PathError{Op: "remove", Path: p, Err: os.ErrNotExist}
	}
	delete(m.Files, p)
	return nil
}

func (m *MemFilesystem) RemoveAll(p string) error {
	prefix := p + "/"
	for path := range m.Files {
		if path == p || strings.HasPrefix(path, prefix) {
			delete(m.Files, path)
		}
	}
	for path := range m.Dirs {
		if path == p || strings.HasPrefix(path, prefix) {
			delete(m.Dirs, path)
		}
	}
	return nil
}

func (m *MemFilesystem) MkdirAll(p string, _ os.FileMode) error {
	m.Dirs[p] = true
	return nil
}

// ReadDir returns the base names of entries (files and directories) whose path
// has dir as their immediate parent directory. A directory that has no entries
// and is not recorded in Dirs returns an os.ErrNotExist error, matching
// os.ReadDir semantics.
func (m *MemFilesystem) ReadDir(dir string) ([]string, error) {
	seen := map[string]bool{}
	for p := range m.Files {
		if filepath.Dir(p) == dir {
			seen[filepath.Base(p)] = true
		}
	}
	for p := range m.Dirs {
		if filepath.Dir(p) == dir {
			seen[filepath.Base(p)] = true
		}
	}
	if len(seen) == 0 && !m.Dirs[dir] {
		return nil, &os.PathError{Op: "open", Path: dir, Err: os.ErrNotExist}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

// memFileInfo is the minimal os.FileInfo MemFilesystem.Stat returns.
type memFileInfo struct {
	name string
	size int64
	dir  bool
}

func (i memFileInfo) Name() string       { return i.name }
func (i memFileInfo) Size() int64        { return i.size }
func (i memFileInfo) Mode() os.FileMode  { return 0o644 }
func (i memFileInfo) ModTime() time.Time { return time.Time{} }
func (i memFileInfo) IsDir() bool        { return i.dir }
func (i memFileInfo) Sys() any           { return nil }

func (m *MemFilesystem) Stat(p string) (os.FileInfo, error) {
	if d, ok := m.Files[p]; ok {
		return memFileInfo{name: p, size: int64(len(d))}, nil
	}
	if m.Dirs[p] {
		return memFileInfo{name: p, dir: true}, nil
	}
	return nil, &os.PathError{Op: "stat", Path: p, Err: os.ErrNotExist}
}

var _ fs.FileInfo = memFileInfo{}

// FakeResult is one scripted command outcome.
type FakeResult struct {
	Output []byte
	Err    error
}

// FakeRunner is a scripted CommandRunner that records every call.
type FakeRunner struct {
	Script map[string]FakeResult
	Calls  []string
}

// NewFakeRunner returns an empty scripted runner.
func NewFakeRunner() *FakeRunner {
	return &FakeRunner{Script: map[string]FakeResult{}}
}

// Run records the call and returns the scripted result, or errors if the
// command was not scripted.
func (r *FakeRunner) Run(name string, args ...string) ([]byte, error) {
	key := strings.TrimSpace(name + " " + strings.Join(args, " "))
	r.Calls = append(r.Calls, key)
	res, ok := r.Script[key]
	if !ok {
		return nil, fmt.Errorf("fake runner: unscripted command %q", key)
	}
	return res.Output, res.Err
}

// SortedCalls returns the recorded calls sorted, for stable assertions.
func (r *FakeRunner) SortedCalls() []string {
	out := append([]string(nil), r.Calls...)
	sort.Strings(out)
	return out
}
