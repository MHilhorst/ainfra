package claudecode

import (
	"errors"
	iofs "io/fs"
	"path/filepath"
	"strings"

	"github.com/MHilhorst/ainfra/internal/lockfile"
	"github.com/MHilhorst/ainfra/internal/provider"
	"github.com/MHilhorst/ainfra/internal/provider/fsmerge"
)

// UserCommandsTarget is the one non-default value a command's target may take.
// It installs the command under the user's home instead of the repo, which is
// what Claude Code loads in every repo rather than only this one.
//
// The value is matched literally, not resolved and compared: the desired hash
// is computed in resolve/pipeline.go, which has a repo dir but no home dir, so
// the manifest string is the only token both sides can agree on. Widening this
// to arbitrary directories therefore means teaching the pipeline about env.Home
// first — see the Observe doc comment.
const UserCommandsTarget = "~/.claude/commands"

// Commands reconciles standalone markdown files under .claude/commands/, either
// in the repo (the default) or in the user's home when the manifest sets
// `target: ~/.claude/commands`. Each file is fully owned by ainfra; the
// filename without the .md extension is the resource ID.
type Commands struct{}

// Channel returns the channel name this provider manages.
func (Commands) Channel() string { return "commands" }

// commandsDirFor returns the directory a command with this target is written
// to. Any value other than UserCommandsTarget — including the empty default —
// is the repo's own .claude/commands/.
func commandsDirFor(env provider.Env, target string) string {
	if target == UserCommandsTarget && env.Home != "" {
		return filepath.Join(env.Home, ".claude", "commands")
	}
	return filepath.Join(env.Root, ".claude", "commands")
}

func commandsDir(env provider.Env) string {
	return commandsDirFor(env, "")
}

// commandPathFor returns the file a command with this target is written to.
func commandPathFor(env provider.Env, target, id string) string {
	return filepath.Join(commandsDirFor(env, target), id+".md")
}

func commandPath(env provider.Env, id string) string {
	return commandPathFor(env, "", id)
}

// commandTargets lists every target a command file may live under, in the order
// Observe scans them. The repo comes first so a repo-local command wins the
// de-duplication against a same-named user-wide one — the narrower declaration
// is the more specific intent.
func commandTargets(env provider.Env) []string {
	targets := []string{""}
	if env.Home != "" && env.Home != env.Root {
		targets = append(targets, UserCommandsTarget)
	}
	return targets
}

// Observe lists *.md files in every directory a command can live in and returns
// a Resource per file. A missing directory is treated as no resources.
//
// ContentHash covers the file's bytes AND the target it was found under, and
// must stay byte-identical to the desired-hash construction in
// resolve/pipeline.go — the diff compares the two directly. Hashing the bytes
// is what makes a hand-edit to a materialized command surface as drift;
// hashing the target alongside them is what makes moving a command between the
// repo and the user's home surface as drift too. Without the target in the
// hash, a command whose declaration moved would match the copy still sitting in
// its old directory and never relocate.
//
// Observing the user's home in every repo is safe because DiffResources only
// deletes what the repo's own applied ledger recorded (or what a tombstone
// names). A user-wide command another repo installed is observed here, matches
// nothing desired or prior, and is left alone — unless `--prune` is on, which
// is guarded by the offered ledger.
func (Commands) Observe(env provider.Env) ([]provider.Resource, error) {
	seen := map[string]bool{}
	var resources []provider.Resource

	for _, target := range commandTargets(env) {
		entries, err := env.FS.ReadDir(commandsDirFor(env, target))
		if errors.Is(err, iofs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}

		for _, name := range entries {
			if !strings.HasSuffix(name, ".md") {
				continue
			}
			id := strings.TrimSuffix(name, ".md")
			if seen[id] {
				continue
			}
			raw, rerr := env.FS.ReadFile(commandPathFor(env, target, id))
			if rerr != nil {
				continue
			}
			seen[id] = true
			resources = append(resources, provider.Resource{
				ID:          id,
				Channel:     "commands",
				ContentHash: CommandContentHash(string(raw), target),
			})
		}
	}
	return resources, nil
}

// CommandContentHash builds a command's reconcile hash from its materialized
// content and its target. It MUST stay byte-identical to the desired-hash
// construction in resolve/pipeline.go — the diff compares the two directly, so
// any divergence silently breaks reconciliation rather than failing loudly.
func CommandContentHash(content, target string) string {
	return lockfile.ContentHash(map[string]any{
		"content": content, "target": target,
	})
}

// Apply executes the channel plan, writing or removing command files.
// When env.DryRun is true, the result is computed but no files are modified.
//
// A write also removes the same command from every other target directory, so
// a declaration that moved between the repo and the user's home leaves no
// second copy behind. Claude Code would otherwise load both, and the stale one
// keeps winning Observe's de-duplication — a drift that re-applies forever.
//
// Delete removes the command from every target directory rather than the one
// the change names: the resource behind a delete comes from the applied ledger,
// which records hashes and no payload, so the target it was installed under is
// not knowable here.
func (Commands) Apply(env provider.Env, plan provider.ChannelPlan) (provider.ApplyResult, error) {
	var applied []provider.Change

	for _, c := range plan.Changes {
		if c.Kind == provider.ChangeNoop {
			continue
		}

		if !env.DryRun {
			var err error
			switch c.Kind {
			case provider.ChangeCreate, provider.ChangeUpdate:
				target, _ := c.Resource.Payload["target"].(string)
				content, _ := c.Resource.Payload["content"].(string)
				err = fsmerge.WriteOwnedFile(env.FS, commandPathFor(env, target, c.ID), []byte(content))
				if err == nil {
					err = removeCommandExcept(env, target, c.ID)
				}
			case provider.ChangeDelete:
				err = removeCommandEverywhere(env, c.ID)
			}
			if err != nil {
				return provider.ApplyResult{}, err
			}
		}

		applied = append(applied, c)
	}

	return provider.ApplyResult{
		Channel: "commands",
		Applied: applied,
	}, nil
}

// removeCommandExcept deletes the command's file from every target directory
// other than keep.
func removeCommandExcept(env provider.Env, keep, id string) error {
	for _, target := range commandTargets(env) {
		if target == keep {
			continue
		}
		if err := removeCommandAt(env, target, id); err != nil {
			return err
		}
	}
	return nil
}

// removeCommandEverywhere deletes the command's file from every target
// directory. Used for deletes, where the target the command was installed
// under is not recoverable from the applied ledger.
func removeCommandEverywhere(env provider.Env, id string) error {
	for _, target := range commandTargets(env) {
		if err := removeCommandAt(env, target, id); err != nil {
			return err
		}
	}
	return nil
}

// removeCommandAt deletes one command file. An absent file is the desired end
// state, not an error — letting it fail would abort the whole channel and
// strand the resource in the ledger, so the next run plans the same doomed
// change forever.
func removeCommandAt(env provider.Env, target, id string) error {
	err := env.FS.Remove(commandPathFor(env, target, id))
	if errors.Is(err, iofs.ErrNotExist) {
		return nil
	}
	return err
}

// Backup copies the command's markdown file into dir before Apply removes it.
// It backs up the first target directory the file actually exists in, so a
// user-wide command is captured as reliably as a repo-local one.
func (Commands) Backup(env provider.Env, r provider.Resource, dir string) error {
	dest := filepath.Join(dir, "commands", r.ID+".md")
	for _, target := range commandTargets(env) {
		src := commandPathFor(env, target, r.ID)
		if _, err := env.FS.ReadFile(src); err != nil {
			continue
		}
		return copyFile(env, src, dest)
	}
	return copyFile(env, commandPath(env, r.ID), dest)
}
