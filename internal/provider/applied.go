package provider

import (
	"os"
	"path/filepath"

	"github.com/MHilhorst/ainfra/internal/lockfile"
	"github.com/MHilhorst/ainfra/internal/xdg"
)

// appliedPath is the per-machine applied-state ledger location under a repo
// root.
//
// Note: .ainfra/ is NOT git-ignored by ainfra. `ainfra init` writes only the
// `ainfra.personal.*` pattern (cmd_init.go::gitignoreEntry); repos that ignore
// .ainfra/ do so by hand. Do not rely on this directory being private — the
// prune offered ledger and its backups deliberately live under
// $XDG_CONFIG_HOME instead, because a shared copy of "what this user was shown"
// would arm deletions on a teammate's first run.
func appliedPath(root string) string {
	return filepath.Join(root, ".ainfra", "applied.lock")
}

func appliedPathForAgent(root, agentID string) string {
	if agentID == "" || agentID == "claude-code" {
		return appliedPath(root)
	}
	return filepath.Join(root, ".ainfra", "applied."+agentID+".lock")
}

// ReadApplied loads the applied-state ledger — the lock ainfra last applied on
// this machine. A missing ledger is not an error: it returns an empty lock, so
// a first-ever apply treats every desired entry as a create.
func ReadApplied(root string) (*lockfile.Lock, error) {
	return lockfile.Read(appliedPath(root))
}

// ReadAppliedForAgent loads the applied-state ledger for one target agent.
// Claude Code keeps using the historical applied.lock path; other agents use
// applied.<agent>.lock so side-by-side installs cannot plan each other away.
func ReadAppliedForAgent(root, agentID string) (*lockfile.Lock, error) {
	return lockfile.Read(appliedPathForAgent(root, agentID))
}

// WriteApplied snapshots l as the applied-state ledger after a successful apply.
func WriteApplied(root string, l *lockfile.Lock) error {
	dir := filepath.Join(root, ".ainfra")
	if err := ensureDir(dir); err != nil {
		return err
	}
	return lockfile.Write(appliedPath(root), l)
}

// WriteAppliedForAgent snapshots l as the agent-specific applied-state ledger
// after a successful apply.
func WriteAppliedForAgent(root, agentID string, l *lockfile.Lock) error {
	dir := filepath.Join(root, ".ainfra")
	if err := ensureDir(dir); err != nil {
		return err
	}
	return lockfile.Write(appliedPathForAgent(root, agentID), l)
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

// ReadAppliedUserForAgent loads the user-scope applied-state ledger for one
// target agent.
func ReadAppliedUserForAgent(agentID string) (*lockfile.Lock, error) {
	path, err := xdg.AppliedLedgerPathForAgent(agentID)
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

// WriteAppliedUserForAgent snapshots l as the user-scope applied-state ledger
// for one target agent.
func WriteAppliedUserForAgent(agentID string, l *lockfile.Lock) error {
	path, err := xdg.AppliedLedgerPathForAgent(agentID)
	if err != nil {
		return err
	}
	if err := ensureDir(filepath.Dir(path)); err != nil {
		return err
	}
	return lockfile.Write(path, l)
}
