package provider

import (
	"os"
	"path/filepath"

	"github.com/MHilhorst/ainfra/internal/lockfile"
	"github.com/MHilhorst/ainfra/internal/xdg"
)

// appliedPath is the per-machine applied-state ledger location under a repo
// root. .ainfra/ is git-ignored, so the ledger is never committed.
func appliedPath(root string) string {
	return filepath.Join(root, ".ainfra", "applied.lock")
}

// ReadApplied loads the applied-state ledger — the lock ainfra last applied on
// this machine. A missing ledger is not an error: it returns an empty lock, so
// a first-ever apply treats every desired entry as a create.
func ReadApplied(root string) (*lockfile.Lock, error) {
	return lockfile.Read(appliedPath(root))
}

// WriteApplied snapshots l as the applied-state ledger after a successful apply.
func WriteApplied(root string, l *lockfile.Lock) error {
	dir := filepath.Join(root, ".ainfra")
	if err := ensureDir(dir); err != nil {
		return err
	}
	return lockfile.Write(appliedPath(root), l)
}

// ensureDir creates dir and all parent directories if they do not exist.
func ensureDir(dir string) error {
	return os.MkdirAll(dir, 0o755)
}

// ReadAppliedUser loads the user-scope applied-state ledger from the XDG
// config home. A missing ledger is not an error — a first-ever user-scope
// apply treats every desired entry as a create.
func ReadAppliedUser() (*lockfile.Lock, error) {
	path, err := xdg.AppliedLedgerPath()
	if err != nil {
		return nil, err
	}
	return lockfile.Read(path)
}

// WriteAppliedUser snapshots l as the user-scope applied-state ledger.
func WriteAppliedUser(l *lockfile.Lock) error {
	path, err := xdg.AppliedLedgerPath()
	if err != nil {
		return err
	}
	if err := ensureDir(filepath.Dir(path)); err != nil {
		return err
	}
	return lockfile.Write(path, l)
}
