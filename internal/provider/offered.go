package provider

import (
	"crypto/sha256"
	"encoding/hex"
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

// OfferedLedger is the per-machine record of untracked resources this user has
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

// agentSuffix keys a ledger file to one target agent, mirroring
// appliedPathForAgent. Claude Code keeps the unsuffixed name for continuity.
//
// Without this, a side-by-side install (`ainfra install --prune --agent codex`)
// would rewrite the ledger from its own provider set — which contains no
// Pruner — and truncate it to empty, so the next Claude Code run would re-offer
// everything and prune could never converge on a machine that installs both.
func agentSuffix(agentID string) string {
	if agentID == "" || agentID == "claude-code" {
		return ""
	}
	return "." + agentID
}

// repoScopeKey identifies a repo-scope ledger by its root path. Hashed rather
// than embedded so the filename stays short and filesystem-safe. A repo that
// moves simply gets a fresh ledger and re-offers, which is the safe direction.
func repoScopeKey(root string) string {
	sum := sha256.Sum256([]byte(root))
	return "repo-" + hex.EncodeToString(sum[:8])
}

// offeredDir is the directory holding every offered ledger on this machine.
//
// It lives under $XDG_CONFIG_HOME/ainfra, NOT inside the repo, and that is a
// correctness requirement rather than a preference. The ledger's whole meaning
// is "this user, on this machine, was shown this entry". A ledger inside the
// repo would be committed and shared: ainfra never git-ignores .ainfra/ (init
// writes only the `ainfra.personal.*` pattern), so a teammate cloning the repo
// would inherit a ledger listing entries they have never seen, and their very
// first `install --prune` would delete their own local config without ever
// reporting it. That is precisely the outcome this design exists to prevent.
func offeredDir() (string, error) {
	dir, err := xdg.ConfigHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "prune-offered"), nil
}

// OfferedPathUser is the user-scope offered ledger for one target agent.
func OfferedPathUser(agentID string) (string, error) {
	dir, err := offeredDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "user"+agentSuffix(agentID)+".json"), nil
}

// OfferedPathRepo is the offered ledger for one repo root and target agent.
// Keyed by the root's hash and stored outside the repo — see offeredDir.
func OfferedPathRepo(root, agentID string) (string, error) {
	dir, err := offeredDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, repoScopeKey(root)+agentSuffix(agentID)+".json"), nil
}

// PruneBackupRoot is the directory prune copies removed resources into, before
// the per-run timestamp. Outside the repo for the same reason as the ledger:
// a backup of an untracked .mcp.json entry carries that server's full config,
// including any env values, and must not be committed by accident.
func PruneBackupRoot() (string, error) {
	dir, err := xdg.ConfigHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "pruned"), nil
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
