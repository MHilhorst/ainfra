package provider

import (
	"os"
	"os/exec"

	"github.com/MHilhorst/ainfra/internal/provider/fetch"
)

// Filesystem is the file I/O surface a provider may use. Production code uses
// OSFilesystem; tests use MemFilesystem so Observe/Apply are testable without
// touching the real disk.
type Filesystem interface {
	ReadFile(path string) ([]byte, error)
	WriteFile(path string, data []byte, perm os.FileMode) error
	Remove(path string) error
	Stat(path string) (os.FileInfo, error)
	MkdirAll(path string, perm os.FileMode) error
	// ReadDir returns the base names of entries in the given directory.
	// A missing directory returns an error satisfying os.IsNotExist.
	ReadDir(path string) ([]string, error)
	// RemoveAll removes path and any children it contains, like os.RemoveAll.
	// It returns nil if the path does not exist.
	RemoveAll(path string) error
}

// CommandRunner runs an external command and returns its combined output.
type CommandRunner interface {
	Run(name string, args ...string) ([]byte, error)
}

// Env is the injected environment a provider observes and applies against.
type Env struct {
	FS     Filesystem
	Runner CommandRunner
	Fetch  fetch.Fetcher
	Home   string // Claude Code config root (e.g. the user's home directory)
	Root   string // the repo root the manifest was resolved from
	DryRun bool
	// NoInstall, when set, makes the cliTools provider skip package installs
	// and the declare-and-check probe while the file-writing channels still
	// reconcile. Unlike DryRun it does not suppress file writes.
	NoInstall bool
}

// OSFilesystem is the real-disk Filesystem.
type OSFilesystem struct{}

func (OSFilesystem) ReadFile(p string) ([]byte, error)                 { return os.ReadFile(p) }
func (OSFilesystem) WriteFile(p string, d []byte, m os.FileMode) error { return os.WriteFile(p, d, m) }
func (OSFilesystem) Remove(p string) error                             { return os.Remove(p) }
func (OSFilesystem) Stat(p string) (os.FileInfo, error)                { return os.Stat(p) }
func (OSFilesystem) MkdirAll(p string, m os.FileMode) error            { return os.MkdirAll(p, m) }
func (OSFilesystem) RemoveAll(p string) error                          { return os.RemoveAll(p) }
func (OSFilesystem) ReadDir(p string) ([]string, error) {
	entries, err := os.ReadDir(p)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names, nil
}

// Ensure OSFilesystem satisfies Filesystem at compile time.
var _ Filesystem = OSFilesystem{}

// ExecRunner is the real CommandRunner.
type ExecRunner struct{}

// Run executes name with args and returns combined stdout+stderr.
func (ExecRunner) Run(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).CombinedOutput()
}
