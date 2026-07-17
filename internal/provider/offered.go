package provider

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/MHilhorst/ainfra/internal/xdg"
)

// offeredLedgerVersion is the on-disk schema version of the offered ledger.
const offeredLedgerVersion = 1

// OfferedEntry records when an untracked resource was first reported to the
// user.
type OfferedEntry struct {
	FirstOfferedAt string `json:"firstOfferedAt"`
}

// OfferedLedger is the per-scope record of untracked resources the user has
// already been shown by a --prune run.
//
// It is what makes --prune safe. Untracked does not mean unwanted: it usually
// means the user never got around to declaring the thing. So prune reports an
// untracked resource on one run and only removes it on a later one, once the
// user has had a chance to declare it. This ledger is the memory of "you were
// told", and it is deliberately not bypassable by a flag: the enforced gap
// between being shown an entry and losing it is the whole point.
type OfferedLedger struct {
	Version int                     `json:"version"`
	Offered map[string]OfferedEntry `json:"offered"`
}

// OfferedKey is the ledger key for one resource: "<channel>:<id>".
func OfferedKey(channel, id string) string { return channel + ":" + id }

// OfferedPath is the repo-scope offered ledger location. It sits in .ainfra/
// beside the applied ledger, which is git-ignored, so it never lands in git.
func OfferedPath(root string) string {
	return filepath.Join(root, ".ainfra", "prune-offered.json")
}

// OfferedPathUser is the user-scope offered ledger location, alongside the
// user-scope applied ledger under $XDG_CONFIG_HOME/ainfra.
func OfferedPathUser() (string, error) {
	dir, err := xdg.ConfigHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "prune-offered.json"), nil
}

// ReadOffered loads the offered ledger. A missing file is a first run, not an
// error: it returns an empty ledger.
//
// An unparseable file also returns an empty ledger, with corrupt=true so the
// caller can warn. This fails open on purpose: an empty ledger means every
// untracked resource is offered afresh and nothing is deleted this run, and the
// cost is one extra offer round. Failing closed — treating an unreadable ledger
// as "everything was already offered" — would delete without the user ever
// having been shown the list, which is the one outcome this design exists to
// prevent.
func ReadOffered(fs Filesystem, path string) (*OfferedLedger, bool, error) {
	empty := &OfferedLedger{Version: offeredLedgerVersion, Offered: map[string]OfferedEntry{}}

	raw, err := fs.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return empty, false, nil
		}
		return nil, false, err
	}

	var l OfferedLedger
	if err := json.Unmarshal(raw, &l); err != nil {
		return empty, true, nil
	}
	if l.Offered == nil {
		l.Offered = map[string]OfferedEntry{}
	}
	l.Version = offeredLedgerVersion
	return &l, false, nil
}

// WriteOffered persists the offered ledger, creating its directory if needed.
func WriteOffered(fs Filesystem, path string, l *OfferedLedger) error {
	if err := fs.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	l.Version = offeredLedgerVersion
	if l.Offered == nil {
		l.Offered = map[string]OfferedEntry{}
	}
	raw, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	return fs.WriteFile(path, append(raw, '\n'), 0o644)
}
